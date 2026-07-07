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

	buf         []model.LogLine     // time-ordered ring buffer
	renderedAll []string            // cached rendered rows, parallel to buf
	dirty       bool                // viewport content needs a rebuild
	seen        map[string]struct{} // recent line fingerprints for de-duplication
	seenRing    []string            // insertion order for evicting old fingerprints

	// available sources come from the topology; toggled ones stream.
	sources   []model.Source
	activeKey map[string]bool // key -> user wants it streaming

	// health/liveness, so "is it working?" is answerable at a glance.
	health map[string]streamEvent // key -> latest stream state (shared with App)
	counts map[string]int         // key -> lines received this session

	logPath string // debug log location, shown when streams fail

	filtering   bool
	showSidebar bool
	autoscroll  bool
	cursor      int // sidebar cursor

	width, height int
	tagWidth      int
}

func newLogsPane(styles *theme.Styles) *logsPane {
	fi := textinput.New()
	fi.Placeholder = "filter (text, @level:error, @kind:build, [service])"
	fi.Prompt = "/"
	return &logsPane{
		styles:      styles,
		filter:      fi,
		activeKey:   map[string]bool{},
		health:      map[string]streamEvent{},
		counts:      map[string]int{},
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
	fp := ll.Source.Key() + "|" + ll.Timestamp.Format("15:04:05.000000000") + "|" + ll.Message
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

// resetContext clears everything tied to the current project/environment
// (used when switching context).
func (p *logsPane) resetContext() {
	p.buf = nil
	p.renderedAll = nil
	p.seen = map[string]struct{}{}
	p.seenRing = nil
	p.counts = map[string]int{}
	p.activeKey = map[string]bool{}
	p.dirty = true
}

// append inserts a line, keeping the buffer roughly time-ordered and capped.
// The rendered-row cache is updated in lockstep, so appending is O(window)
// instead of re-rendering the whole buffer (which made the UI lag under any
// real log volume). Returns false if the line was dropped as a replayed
// duplicate — callers should skip error/notification handling for dropped
// lines so a replay flood can't repeatedly re-trigger them.
func (p *logsPane) append(ll model.LogLine) bool {
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
	p.renderedAll = append(p.renderedAll, "")
	copy(p.renderedAll[i+1:], p.renderedAll[i:])
	p.renderedAll[i] = p.renderLine(ll)

	if len(p.buf) > logBufferCap {
		p.buf = p.buf[len(p.buf)-logBufferCap:]
		p.renderedAll = p.renderedAll[len(p.renderedAll)-logBufferCap:]
	}

	p.counts[ll.Source.Key()]++
	p.dirty = true
	return true
}

// lastMessageFor returns the most recent buffered log message for a service
// (any source kind), used by the deploy-progress overlay to show live output.
func (p *logsPane) lastMessageFor(name string) string {
	for i := len(p.buf) - 1; i >= 0; i-- {
		if p.buf[i].Source.ServiceName == name && strings.TrimSpace(p.buf[i].Message) != "" {
			return p.buf[i].Message
		}
	}
	return ""
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
	// @kind:x filter (deploy/build/http/network) — the quick way to see just
	// build output after toggling a build source on.
	if strings.HasPrefix(lq, "@kind:") {
		want := strings.TrimPrefix(lq, "@kind:")
		return strings.EqualFold(string(ll.Source.Kind), want)
	}
	return strings.Contains(strings.ToLower(ll.Message), lq) ||
		strings.Contains(strings.ToLower(ll.Level), lq) ||
		strings.Contains(strings.ToLower(ll.Source.Label()), lq)
}

// sync rebuilds the viewport content from the cached rendered rows (cheap: a
// filter pass + join, no re-styling). Called lazily from View when dirty.
func (p *logsPane) sync() {
	p.dirty = false
	lines := make([]string, 0, len(p.buf))
	for i, ll := range p.buf {
		if !p.matches(ll) {
			continue
		}
		lines = append(lines, p.renderedAll[i])
	}
	content := strings.Join(lines, "\n")
	if len(lines) == 0 {
		content = p.emptyState()
	}
	p.vp.SetContent(content)
	if p.autoscroll {
		p.vp.GotoBottom()
	}
}

// reflow fully re-renders every cached row (needed when the pane width
// changes) and then syncs the viewport.
func (p *logsPane) reflow() {
	p.renderedAll = make([]string, len(p.buf))
	for i, ll := range p.buf {
		p.renderedAll[i] = p.renderLine(ll)
	}
	p.sync()
}

// emptyState explains why no log lines are showing — using real stream health
// so a failing stream can never masquerade as "waiting for logs".
func (p *logsPane) emptyState() string {
	active, live, conn, failed := 0, 0, 0, 0
	firstErr := ""
	for key, on := range p.activeKey {
		if !on {
			continue
		}
		active++
		ev, ok := p.health[key]
		if !ok {
			conn++
			continue
		}
		switch ev.state {
		case streamLive:
			live++
		case streamConnecting, streamReconnecting:
			conn++
		case streamFailed:
			failed++
			if firstErr == "" && ev.info != "" {
				firstErr = ev.info
			}
		}
	}
	switch {
	case len(p.buf) > 0:
		return p.styles.Dim.Render("No lines match the current filter (" + p.filter.Value() + ").\n\nPress / then esc to clear it.")
	case active == 0:
		return p.styles.Dim.Render("No log sources selected.\n\n" +
			"Toggle a service in the sidebar with [enter], or focus one from the Topology pane.")
	case failed == active:
		msg := "All " + itoaLocal(active) + " log stream(s) are failing."
		if firstErr != "" {
			msg += "\n\n" + firstErr
		}
		if p.logPath != "" {
			msg += "\n\nDetails: " + p.logPath
		}
		return lipgloss.NewStyle().Foreground(p.styles.T.Bad).Render(msg)
	case live == 0:
		return p.styles.Dim.Render("Connecting to " + itoaLocal(conn) + " log stream(s)…")
	default:
		return p.styles.Dim.Render(itoaLocal(live) + " stream(s) connected — waiting for output.\n\n" +
			"(the services are quiet; lines appear the moment they log)")
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

// compactCount formats a line count for the narrow sidebar (1234 -> "1.2k").
func compactCount(n int) string {
	switch {
	case n >= 100000:
		return itoaLocal(n/1000) + "k"
	case n >= 1000:
		return itoaLocal(n/1000) + "." + itoaLocal((n%1000)/100) + "k"
	default:
		return itoaLocal(n)
	}
}

// iconFor maps a stream state to its sidebar glyph.
func iconFor(st streamState) string {
	switch st {
	case streamLive:
		return "●"
	case streamConnecting:
		return "◌"
	case streamReconnecting:
		return "↻"
	case streamFailed:
		return "✖"
	case streamEnded:
		return "✓"
	}
	return "○"
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
			case "enter":
				p.filtering = false
				p.filter.Blur()
				p.sync()
			case "esc":
				// esc cancels: clear the filter and leave filter mode.
				p.filter.SetValue("")
				p.filtering = false
				p.filter.Blur()
				p.sync()
			default:
				p.filter, cmd = p.filter.Update(msg)
				p.sync()
			}
			return cmd, nil
		}
		switch m.String() {
		case "/":
			p.filtering = true
			p.filter.Focus()
			return textinput.Blink, nil
		case "esc":
			if p.filter.Value() != "" {
				p.filter.SetValue("")
				p.sync()
			}
			return nil, nil
		case "s":
			p.showSidebar = !p.showSidebar
			p.setSize(p.width, p.height)
			return nil, nil
		case "a":
			p.autoscroll = !p.autoscroll
			if p.autoscroll {
				p.vp.GotoBottom()
			}
			return nil, nil
		case "c":
			p.buf = nil
			p.renderedAll = nil
			p.seen = map[string]struct{}{}
			p.seenRing = nil
			p.sync()
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
	if p.dirty {
		p.sync()
	}
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
		hint := "[/]filter [s]ources [a]tail [c]lear"
		if q != "" {
			hint = "filter: " + q + " (esc clears)  " + hint
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

// sourceIcon picks the health glyph and its color for one source row.
func (p *logsPane) sourceIcon(key string) (string, lipgloss.Style) {
	good := lipgloss.NewStyle().Foreground(p.styles.T.Good)
	warn := lipgloss.NewStyle().Foreground(p.styles.T.Warn)
	bad := lipgloss.NewStyle().Foreground(p.styles.T.Bad)

	ev, hasEv := p.health[key]
	if !p.activeKey[key] {
		// A finished one-shot (build) keeps its ✓ so "it ran" stays visible.
		if hasEv && ev.state == streamEnded {
			return iconFor(streamEnded), good
		}
		return iconFor(streamOff), p.styles.Dim
	}
	if !hasEv {
		return iconFor(streamConnecting), warn
	}
	switch ev.state {
	case streamLive:
		return iconFor(streamLive), good
	case streamReconnecting:
		return iconFor(streamReconnecting), warn
	case streamFailed:
		return iconFor(streamFailed), bad
	case streamEnded:
		return iconFor(streamEnded), good
	default:
		return iconFor(streamConnecting), warn
	}
}

func (p *logsPane) sidebar() string {
	w := p.sidebarWidth()
	var b strings.Builder
	b.WriteString(p.styles.Title.Render("Sources"))
	b.WriteString("\n")

	for i, src := range p.sources {
		key := src.Key()
		icon, iconStyle := p.sourceIcon(key)
		label := src.Label()
		if src.Kind != model.LogDeploy {
			label += " " + string(src.Kind)
		}
		count := ""
		if c := p.counts[key]; c > 0 {
			count = compactCount(c)
		}
		// Row layout: icon + space + label … right-aligned count.
		avail := w - 2 - lipgloss.Width(count)
		if count != "" {
			avail-- // gap before count
		}
		if avail < 3 {
			avail = 3
		}
		label = ansi.Truncate(label, avail, "…")
		pad := w - 2 - lipgloss.Width(label) - lipgloss.Width(count)
		if pad < 0 {
			pad = 0
		}
		if i == p.cursor {
			line := ansi.Truncate(icon+" "+label+strings.Repeat(" ", pad)+count, w, "…")
			style := lipgloss.NewStyle().Foreground(p.styles.T.SourceColor(src.Label())).
				Background(p.styles.T.BorderCol).Bold(true)
			b.WriteString(style.Render(line))
		} else {
			lbl := lipgloss.NewStyle().Foreground(p.styles.T.SourceColor(src.Label())).Render(label + strings.Repeat(" ", pad))
			b.WriteString(iconStyle.Render(icon) + " " + lbl + p.styles.Dim.Render(count))
		}
		b.WriteString("\n")
	}
	// One-line legend so the icons are self-explanatory.
	legend := "● live ◌ conn ↻ retry"
	legend2 := "✖ fail ✓ done ○ off"
	b.WriteString("\n")
	b.WriteString(p.styles.Dim.Render(ansi.Truncate(legend, w, "")))
	b.WriteString("\n")
	b.WriteString(p.styles.Dim.Render(ansi.Truncate(legend2, w, "")))

	content := b.String()
	// Border adds 2 rows; keep the whole block the same height as the pane.
	innerH := p.height - 2
	if innerH < 1 {
		innerH = 1
	}
	return p.styles.Border.Width(w).Height(innerH).Render(content)
}
