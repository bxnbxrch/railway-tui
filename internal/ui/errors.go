package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"railway-tui/internal/model"
	"railway-tui/internal/ui/theme"
)

const errorBufferCap = 1000

// errorsPane collects log lines detected as errors and presents them in a
// clear, red-accented, scrollable feed — each entry showing the service, time,
// level, and (word-wrapped) message.
type errorsPane struct {
	styles *theme.Styles
	vp     viewport.Model
	buf    []model.LogLine

	width, height int
	ready         bool
}

func newErrorsPane(styles *theme.Styles) *errorsPane {
	return &errorsPane{styles: styles}
}

func (p *errorsPane) setSize(w, h int) {
	p.width, p.height = w, h
	vpH := h - 2 // title + spacer
	if vpH < 3 {
		vpH = 3
	}
	p.vp.Width = w
	p.vp.Height = vpH
	p.ready = true
	p.reflow()
}

// append records an error line and keeps the buffer capped.
func (p *errorsPane) append(ll model.LogLine) {
	p.buf = append(p.buf, ll)
	if len(p.buf) > errorBufferCap {
		p.buf = p.buf[len(p.buf)-errorBufferCap:]
	}
	p.reflow()
}

func (p *errorsPane) clear() {
	p.buf = nil
	p.reflow()
}

func (p *errorsPane) Update(msg tea.Msg) tea.Cmd {
	if km, ok := msg.(tea.KeyMsg); ok && km.String() == "c" {
		p.clear()
		return nil
	}
	var cmd tea.Cmd
	p.vp, cmd = p.vp.Update(msg)
	return cmd
}

func (p *errorsPane) reflow() {
	if !p.ready {
		return
	}
	if len(p.buf) == 0 {
		p.vp.SetContent(p.styles.Dim.Render("No errors detected. Error-level or panic/fatal log lines will appear here in red."))
		return
	}
	var b strings.Builder
	for i, ll := range p.buf {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(p.renderEntry(ll))
	}
	p.vp.SetContent(b.String())
	p.vp.GotoBottom()
}

var (
	redBar  = lipgloss.Color("#ff6767")
	redSoft = lipgloss.Color("#ff9b9b")
)

func (p *errorsPane) renderEntry(ll model.LogLine) string {
	bar := lipgloss.NewStyle().Foreground(redBar).Bold(true).Render("▌")

	svc := ll.Source.Label()
	svcStyled := lipgloss.NewStyle().Foreground(p.styles.T.SourceColor(svc)).Bold(true).Render(svc)

	ts := "--:--:--"
	if !ll.Timestamp.IsZero() {
		ts = ll.Timestamp.Local().Format("15:04:05")
	}
	tsStyled := p.styles.Dim.Render(ts)

	level := strings.ToUpper(ll.Level)
	if level == "" {
		level = "ERROR"
	}
	lvlStyled := lipgloss.NewStyle().Foreground(redBar).Bold(true).Render("✖ " + level)

	kind := ""
	if ll.Source.Kind != model.LogDeploy {
		kind = p.styles.Dim.Render(" (" + string(ll.Source.Kind) + ")")
	}

	header := bar + " " + lvlStyled + "  " + svcStyled + kind + "  " + tsStyled

	// Word-wrap the message under an indent, prefixing each line with the bar.
	msgWidth := p.vp.Width - 4
	if msgWidth < 10 {
		msgWidth = 10
	}
	wrapped := lipgloss.NewStyle().Foreground(redSoft).Width(msgWidth).Render(ll.Message)
	var body strings.Builder
	for _, line := range strings.Split(wrapped, "\n") {
		body.WriteString("\n" + bar + "   " + line)
	}
	return header + body.String()
}

func (p *errorsPane) View() string {
	title := p.styles.Title.Render("Errors")
	count := lipgloss.NewStyle().Foreground(redBar).Render(" ● " + itoaLocal(len(p.buf)))
	hint := p.styles.Help.Render("   [c]lear · ↑↓ scroll")
	head := title + count + hint
	return lipgloss.JoinVertical(lipgloss.Left, head, "", p.vp.View())
}
