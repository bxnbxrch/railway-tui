package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"railway-tui/internal/model"
	"railway-tui/internal/ui/theme"
)

const logBufferCap = 5000

// logsPane is the docker-compose-style merged, colored log view.
type logsPane struct {
	styles *theme.Styles
	vp     viewport.Model
	filter textinput.Model

	buf      []model.LogLine     // time-ordered ring buffer
	rendered []string            // cached rendered lines parallel to filtered view
	seen     map[string]struct{} // recent line fingerprints for de-duplication
	seenRing []string            // insertion order for evicting old fingerprints

	// available sources come from the topology; toggled ones stream.
	sources   []model.Source
	activeKey map[string]bool // key -> streaming

	filtering   bool
	showSidebar bool
	split       bool // future: merged vs per-source columns
	autoscroll  bool
	cursor      int // sidebar cursor

	width, height int
	tagWidth      int
}

func newLogsPane(styles *theme.Styles) *logsPane {
	fi := textinput.New()
	fi.Placeholder = "filter (text, or @level:error / [service])"
	fi.Prompt = "/"
	return &logsPane{
		styles:      styles,
		filter:      fi,
		activeKey:   map[string]bool{},
		seen:        map[string]struct{}{},
		showSidebar: true,
		autoscroll:  true,
		tagWidth:    12,
	}
}

const seenCap = 2000

// dupe reports whether an identical line was recently seen (same source, time
// and message), recording it if not. This absorbs the overlap between the
// historical tail, the live stream, and reconnect replays.
func (p *logsPane) dupe(ll model.LogLine) bool {
	fp := ll.Source.Key() + "|" + ll.Timestamp.Format("15:04:05.000000") + "|" + ll.Message
	if _, ok := p.seen[fp]; ok {
		return true
	}
	p.seen[fp] = struct{}{}
	p.seenRing = append(p.seenRing, fp)
	if len(p.seenRing) > seenCap {
		old := p.seenRing[0]
		p.seenRing = p.seenRing[1:]
		delete(p.seen, old)
	}
	return false
}

func (p *logsPane) setSize(w, h int) {
	p.width, p.height = w, h
	reserve := 0
	if p.showSidebar {
		// Sidebar block = inner content + 2 border cols, then 1 separator col.
		reserve = p.sidebarWidth() + 2 + 1
	}
	vpW := w - reserve
	if vpW < 10 {
		vpW = 10
	}
	vpH := h - 1 // reserve filter line
	if vpH < 3 {
		vpH = 3
	}
	p.vp.Width = vpW
	p.vp.Height = vpH
	p.filter.Width = vpW - 4
	p.reflow()
}

// sidebarWidth is the inner (content) width of the source sidebar, excluding
// its border.
func (p *logsPane) sidebarWidth() int {
	w := p.width / 4
	if w < 20 {
		w = 20
	}
	if w > 32 {
		w = 32
	}
	return w
}

// setSources updates the available source list (called when topology loads).
// It preserves toggle state for keys that still exist.
func (p *logsPane) setSources(srcs []model.Source) {
	p.sources = srcs
	if p.cursor >= len(srcs) {
		p.cursor = max(0, len(srcs)-1)
	}
}

// append inserts a line, keeping the buffer roughly time-ordered and capped.
// Returns false if the line was dropped — either as a replayed duplicate
// (tail/stream/reconnect overlap) or because its source has since been
// disabled (a line already in flight when the stream was cancelled) —
// callers should skip error/notification handling for dropped lines so a
// replay flood, or a stale line from a disabled source, can't re-trigger them.
func (p *logsPane) append(ll model.LogLine) bool {
	if !p.activeKey[ll.Source.Key()] {
		return false
	}
	if !ll.Timestamp.IsZero() && p.dupe(ll) {
		return false
	}
	if ll.Timestamp.IsZero() {
		ll.Timestamp = time.Now()
	}
	// Insertion-correct against recent reordering: scan back a bounded window.
	i := len(p.buf)
	limit := 0
	if i > 300 {
		limit = i - 300
	}
	for i > limit && p.buf[i-1].Timestamp.After(ll.Timestamp) {
		i--
	}
	p.buf = append(p.buf, model.LogLine{})
	copy(p.buf[i+1:], p.buf[i:])
	p.buf[i] = ll

	if len(p.buf) > logBufferCap {
		p.buf = p.buf[len(p.buf)-logBufferCap:]
	}
	p.reflow()
	return true
}

// purgeSource removes all buffered lines for a source (and its dedup
// fingerprints), used when a source is disabled so its past output
// disappears immediately rather than lingering in the pane.
func (p *logsPane) purgeSource(key string) {
	kept := p.buf[:0]
	for _, ll := range p.buf {
		if ll.Source.Key() != key {
			kept = append(kept, ll)
		}
	}
	p.buf = kept
	// Drop this source's fingerprints too, so re-enabling it later shows its
	// (now-fresh) tail instead of treating identical history as duplicates.
	filtered := p.seenRing[:0]
	for _, fp := range p.seenRing {
		if strings.HasPrefix(fp, key+"|") {
			delete(p.seen, fp)
		} else {
			filtered = append(filtered, fp)
		}
	}
	p.seenRing = filtered
	p.reflow()
}

// matches applies the current filter string to a line.
func (p *logsPane) matches(ll model.LogLine) bool {
	q := strings.TrimSpace(p.filter.Value())
	if q == "" {
		return true
	}
	lq := strings.ToLower(q)
	// [service] tag filter
	if strings.HasPrefix(q, "[") && strings.HasSuffix(q, "]") {
		want := strings.ToLower(strings.Trim(q, "[]"))
		return strings.Contains(strings.ToLower(ll.Source.Label()), want)
	}
	// @level:x filter
	if strings.HasPrefix(lq, "@level:") {
		want := strings.TrimPrefix(lq, "@level:")
		return strings.EqualFold(ll.Level, want)
	}
	return strings.Contains(strings.ToLower(ll.Message), lq) ||
		strings.Contains(strings.ToLower(ll.Level), lq) ||
		strings.Contains(strings.ToLower(ll.Source.Label()), lq)
}

func (p *logsPane) reflow() {
	lines := make([]string, 0, len(p.buf))
	for _, ll := range p.buf {
		if !p.matches(ll) {
			continue
		}
		lines = append(lines, p.renderLine(ll))
	}
	p.rendered = lines
	content := strings.Join(lines, "\n")
	if len(lines) == 0 {
		content = p.emptyState()
	}
	p.vp.SetContent(content)
	if p.autoscroll {
		p.vp.GotoBottom()
	}
}

// emptyState explains why no log lines are showing.
func (p *logsPane) emptyState() string {
	active := 0
	for _, on := range p.activeKey {
		if on {
			active++
		}
	}
	switch {
	case len(p.buf) > 0:
		return p.styles.Dim.Render("No lines match the current filter (" + p.filter.Value() + ").")
	case active == 0:
		return p.styles.Dim.Render("No log sources selected.\n\n" +
			"Toggle a service in the sidebar with [enter], or focus one from the Topology pane.")
	default:
		return p.styles.Dim.Render("Waiting for logs from " + itoaLocal(active) + " source(s)…\n\n" +
			"(streaming; nothing has been emitted yet)")
	}
}

func itoaLocal(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func (p *logsPane) renderLine(ll model.LogLine) string {
	tag := ll.Source.Label()
	if len(tag) > p.tagWidth {
		tag = tag[:p.tagWidth]
	}
	tag = tag + strings.Repeat(" ", p.tagWidth-len(tag))
	tagStyled := lipgloss.NewStyle().Foreground(p.styles.T.SourceColor(ll.Source.Label())).Bold(true).Render(tag)

	kind := ""
	if ll.Source.Kind != model.LogDeploy {
		kind = p.styles.Dim.Render(" " + string(ll.Source.Kind))
	}

	ts := ll.Timestamp.Local().Format("15:04:05")
	tsStyled := p.styles.Dim.Render(ts)

	lvlColor := p.styles.T.LevelColor(ll.Level)
	isErr := isErrorLevel(ll.Level)

	// Level abbreviation (kept separate from the message so we never nest ANSI).
	levelTag := ""
	if ll.Level != "" {
		abbr := strings.ToUpper(ll.Level)[:min(4, len(ll.Level))]
		levelTag = lipgloss.NewStyle().Foreground(lvlColor).Bold(isErr).Render(abbr) + " "
	}

	// Message body color: red-tinted for errors, level color otherwise.
	bodyColor := lvlColor
	if isErr {
		bodyColor = redSoft
	}
	body := lipgloss.NewStyle().Foreground(bodyColor).Render(ll.Message)

	// Errors get a red ✖ marker in the separator so they pop in the stream.
	sep := p.styles.Dim.Render(" │ ")
	if isErr {
		marker := lipgloss.NewStyle().Foreground(p.styles.T.Bad).Bold(true).Render("✖")
		sep = p.styles.Dim.Render(" │ ") + marker + " "
	}
	line := tsStyled + " " + tagStyled + kind + sep + levelTag + body
	// Hard-clip to the viewport width so long lines can't overflow into the
	// sidebar or wrap and corrupt the layout.
	if p.vp.Width > 0 {
		line = ansi.Truncate(line, p.vp.Width, "…")
	}
	return line
}

func (p *logsPane) Update(msg tea.Msg) (tea.Cmd, *sourceToggle) {
	var cmd tea.Cmd
	var toggle *sourceToggle
	switch m := msg.(type) {
	case tea.KeyMsg:
		if p.filtering {
			switch m.String() {
			case "enter", "esc":
				p.filtering = false
				p.filter.Blur()
				p.reflow()
			default:
				p.filter, cmd = p.filter.Update(msg)
				p.reflow()
			}
			return cmd, nil
		}
		switch m.String() {
		case "/":
			p.filtering = true
			p.filter.Focus()
			return textinput.Blink, nil
		case "s":
			p.showSidebar = !p.showSidebar
			p.setSize(p.width, p.height)
			return nil, nil
		case "a":
			p.autoscroll = !p.autoscroll
			return nil, nil
		case "c":
			p.buf = nil
			p.seen = map[string]struct{}{}
			p.seenRing = nil
			p.reflow()
			return nil, nil
		case "up", "k":
			if p.showSidebar {
				p.cursor = max(0, p.cursor-1)
			}
			return nil, nil
		case "down", "j":
			if p.showSidebar {
				p.cursor = min(len(p.sources)-1, p.cursor+1)
			}
			return nil, nil
		case "enter", " ":
			if p.showSidebar && p.cursor < len(p.sources) {
				src := p.sources[p.cursor]
				p.activeKey[src.Key()] = !p.activeKey[src.Key()]
				toggle = &sourceToggle{src: src, on: p.activeKey[src.Key()]}
			}
			return nil, toggle
		case "pgup", "pgdown", "ctrl+u", "ctrl+d", "home", "end":
			p.autoscroll = false
			p.vp, cmd = p.vp.Update(msg)
			return cmd, nil
		}
	}
	p.vp, cmd = p.vp.Update(msg)
	return cmd, toggle
}

// sourceToggle signals the root to start/stop a stream.
type sourceToggle struct {
	src model.Source
	on  bool
}

func (p *logsPane) View() string {
	logView := p.vp.View()
	filterLine := ""
	if p.filtering {
		filterLine = p.filter.View()
	} else {
		q := p.filter.Value()
		scroll := "TAIL"
		if !p.autoscroll {
			scroll = "SCROLL"
		}
		hint := "[/]filter [s]ources [a]utoscroll [c]lear"
		if q != "" {
			hint = "filter: " + q + "  " + hint
		}
		filterLine = p.styles.Help.Render(hint + "  " + scroll)
	}
	// Clamp the filter/help line so it never widens the body past the viewport
	// (JoinVertical pads to the widest line, which would overflow the pane).
	if p.vp.Width > 0 {
		filterLine = ansi.Truncate(filterLine, p.vp.Width, "")
	}
	body := lipgloss.JoinVertical(lipgloss.Left, logView, filterLine)

	if !p.showSidebar {
		return body
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, p.sidebar(), " ", body)
}

func (p *logsPane) sidebar() string {
	w := p.sidebarWidth()
	var b strings.Builder
	b.WriteString(p.styles.Title.Render("Sources"))
	b.WriteString("\n")

	for i, src := range p.sources {
		box := "☐"
		if p.activeKey[src.Key()] {
			box = "☑"
		}
		label := src.Label()
		if src.Kind != model.LogDeploy {
			label += " " + string(src.Kind)
		}
		line := box + " " + label
		// Display-width aware clamp (label has no ANSI yet).
		line = ansi.Truncate(line, w, "…")
		style := lipgloss.NewStyle().Foreground(p.styles.T.SourceColor(src.Label()))
		if i == p.cursor {
			style = style.Background(p.styles.T.BorderCol).Bold(true)
		}
		b.WriteString(style.Render(line))
		b.WriteString("\n")
	}
	content := b.String()
	// Border adds 2 rows; keep the whole block the same height as the pane.
	innerH := p.height - 2
	if innerH < 1 {
		innerH = 1
	}
	return p.styles.Border.Width(w).Height(innerH).Render(content)
}
