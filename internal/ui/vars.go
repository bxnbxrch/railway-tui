package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"railway-tui/internal/model"
	"railway-tui/internal/ui/theme"
)

// varsPane is an environment-variable editor for the focused service, mirroring
// the Railway web dashboard's Variables tab: list, reveal/hide values, add a new
// KEY=VALUE pair, and delete a variable. Values are masked by default.
type varsPane struct {
	styles *theme.Styles
	vp     viewport.Model
	input  textinput.Model

	env     string
	service model.Service
	vars    []model.Variable
	loading bool
	reveal  bool
	cursor  int
	errMsg  string

	adding     bool
	confirming bool // delete confirmation

	width, height int
	ready         bool
}

func newVarsPane(styles *theme.Styles) *varsPane {
	in := textinput.New()
	in.Placeholder = "KEY=value"
	in.Prompt = "+ "
	return &varsPane{styles: styles, input: in}
}

func (p *varsPane) setSize(w, h int) {
	p.width, p.height = w, h
	vpH := h - 3 // title + help/input
	if vpH < 3 {
		vpH = 3
	}
	p.vp.Width = w
	p.vp.Height = vpH
	p.input.Width = w - 4
	p.ready = true
	p.reflow()
}

// setService points the pane at a service. Returns true if focus changed.
func (p *varsPane) setService(env string, svc model.Service) bool {
	changed := svc.ID != p.service.ID || env != p.env
	p.env = env
	p.service = svc
	if changed {
		p.vars = nil
		p.errMsg = ""
		p.cursor = 0
		p.adding, p.confirming = false, false
		p.input.Blur()
		p.input.SetValue("")
		p.loading = svc.ID != ""
		p.reflow()
	}
	return changed
}

func (p *varsPane) setVars(serviceID string, vars []model.Variable) {
	if serviceID != p.service.ID {
		return
	}
	p.vars = vars
	p.loading = false
	p.errMsg = ""
	if p.cursor >= len(vars) {
		p.cursor = max(0, len(vars)-1)
	}
	p.reflow()
}

func (p *varsPane) setError(serviceID, msg string) {
	if serviceID != p.service.ID {
		return
	}
	p.loading = false
	p.errMsg = msg
	p.reflow()
}

func (p *varsPane) selected() (model.Variable, bool) {
	if p.cursor >= 0 && p.cursor < len(p.vars) {
		return p.vars[p.cursor], true
	}
	return model.Variable{}, false
}

// loadVarsMsg asks the root to (re)fetch variables for a service.
type loadVarsMsg struct {
	serviceID   string
	serviceName string
}

// varsActionMsg asks the root to set or delete a variable.
type varsActionMsg struct {
	action  string // "set" | "delete"
	service model.Service
	env     string
	key     string
	value   string
}

func (p *varsPane) Update(msg tea.Msg) tea.Cmd {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		p.vp, cmd = p.vp.Update(msg)
		return cmd
	}

	// Text-entry sub-mode: adding a new variable.
	if p.adding {
		switch km.String() {
		case "enter":
			raw := strings.TrimSpace(p.input.Value())
			p.adding = false
			p.input.Blur()
			p.input.SetValue("")
			key, val, ok := splitKV(raw)
			if !ok || p.service.ID == "" {
				p.errMsg = "expected KEY=value"
				p.reflow()
				return nil
			}
			svc, env := p.service, p.env
			return func() tea.Msg {
				return varsActionMsg{action: "set", service: svc, env: env, key: key, value: val}
			}
		case "esc":
			p.adding = false
			p.input.Blur()
			p.input.SetValue("")
			p.reflow()
			return nil
		default:
			var cmd tea.Cmd
			p.input, cmd = p.input.Update(msg)
			return cmd
		}
	}

	// Delete confirmation sub-mode.
	if p.confirming {
		switch km.String() {
		case "y", "Y", "enter":
			p.confirming = false
			v, ok := p.selected()
			if !ok || p.service.ID == "" {
				return nil
			}
			svc, env := p.service, p.env
			key := v.Name
			return func() tea.Msg {
				return varsActionMsg{action: "delete", service: svc, env: env, key: key}
			}
		case "n", "N", "esc":
			p.confirming = false
			p.reflow()
		}
		return nil
	}

	switch km.String() {
	case "up", "k":
		p.cursor = max(0, p.cursor-1)
		p.reflow()
	case "down", "j":
		p.cursor = min(len(p.vars)-1, p.cursor+1)
		p.reflow()
	case "v":
		p.reveal = !p.reveal
		p.reflow()
	case "r":
		if p.service.ID != "" {
			p.loading = true
			p.reflow()
			id, name := p.service.ID, p.service.Name
			return func() tea.Msg { return loadVarsMsg{serviceID: id, serviceName: name} }
		}
	case "n":
		if p.service.ID != "" {
			p.adding = true
			p.errMsg = ""
			p.input.Focus()
			return textinput.Blink
		}
	case "d":
		if _, ok := p.selected(); ok {
			p.confirming = true
			p.reflow()
		}
	case "pgup", "pgdown", "ctrl+u", "ctrl+d":
		var cmd tea.Cmd
		p.vp, cmd = p.vp.Update(msg)
		return cmd
	}
	return nil
}

func (p *varsPane) reflow() {
	if !p.ready {
		return
	}
	if p.service.ID == "" {
		p.vp.SetContent(p.styles.Dim.Render(
			"No service focused.\n\nPress [enter] on a service in the Topology pane to edit its variables."))
		return
	}
	if p.loading && len(p.vars) == 0 {
		p.vp.SetContent(p.styles.Dim.Render("loading variables…"))
		return
	}
	if p.errMsg != "" && len(p.vars) == 0 {
		p.vp.SetContent(lipgloss.NewStyle().Foreground(p.styles.T.Bad).Render("variables: " + p.errMsg))
		return
	}
	if len(p.vars) == 0 {
		p.vp.SetContent(p.styles.Dim.Render("No variables. Press [n] to add one."))
		return
	}

	// Reserve room so long values don't overflow into wrapping.
	valW := p.width - p.keyWidth() - 6
	if valW < 6 {
		valW = 6
	}

	var b strings.Builder
	cursorLine := 0
	for i, v := range p.vars {
		if i == p.cursor {
			cursorLine = i
		}
		name := fmt.Sprintf("%-*s", p.keyWidth(), truncate(v.Name, p.keyWidth()))
		nameStyled := lipgloss.NewStyle().Foreground(p.styles.T.Accent).Render(name)
		shown := maskValue(v.Value)
		if p.reveal {
			shown = truncate(v.Value, valW)
		}
		valStyled := lipgloss.NewStyle().Foreground(p.styles.T.Fg).Render(shown)
		prefix := "  "
		row := prefix + nameStyled + " " + p.styles.Dim.Render("=") + " " + valStyled
		if i == p.cursor {
			row = p.styles.Title.Render("▸ ") + nameStyled + " " + p.styles.Dim.Render("=") + " " + valStyled
			row = lipgloss.NewStyle().Background(p.styles.T.BorderCol).Render(row)
		}
		b.WriteString(row)
		b.WriteString("\n")
	}
	p.vp.SetContent(b.String())
	if cursorLine < p.vp.YOffset {
		p.vp.SetYOffset(cursorLine)
	} else if cursorLine >= p.vp.YOffset+p.vp.Height {
		p.vp.SetYOffset(cursorLine - p.vp.Height + 1)
	}
}

// keyWidth is the column width reserved for variable names.
func (p *varsPane) keyWidth() int {
	w := p.width / 3
	if w < 12 {
		w = 12
	}
	if w > 40 {
		w = 40
	}
	return w
}

func (p *varsPane) View() string {
	name := p.service.Name
	if name == "" {
		name = "—"
	}
	reveal := "hidden"
	if p.reveal {
		reveal = "shown"
	}
	head := p.styles.Title.Render("Variables — "+name) +
		p.styles.Dim.Render(fmt.Sprintf("   %d vars · values %s", len(p.vars), reveal))

	foot := p.styles.Help.Render("[n]ew [d]elete [v]alues [r]efresh · ↑↓ move")
	if p.adding {
		foot = p.input.View()
	} else if p.confirming {
		if v, ok := p.selected(); ok {
			foot = p.styles.ToastWarn.Render(fmt.Sprintf(" DELETE %s from %s?  [y/N] ", v.Name, name))
		}
	} else if p.errMsg != "" && len(p.vars) > 0 {
		foot = lipgloss.NewStyle().Foreground(p.styles.T.Bad).Render(p.errMsg)
	}
	return clampBlock(lipgloss.JoinVertical(lipgloss.Left, head, p.vp.View(), foot), p.width)
}

// splitKV parses "KEY=value" into its parts (splitting on the first '=').
func splitKV(s string) (key, val string, ok bool) {
	i := strings.IndexByte(s, '=')
	if i <= 0 {
		return "", "", false
	}
	key = strings.TrimSpace(s[:i])
	val = s[i+1:]
	if key == "" {
		return "", "", false
	}
	return key, val, true
}

// maskValue renders a value as bullets (bounded length so secrets never leak
// their length exactly on long values).
func maskValue(v string) string {
	n := len([]rune(v))
	if n == 0 {
		return ""
	}
	if n > 8 {
		n = 8
	}
	return strings.Repeat("•", n)
}
