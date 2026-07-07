package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"railway-tui/internal/model"
	"railway-tui/internal/ui/theme"
)

// deploysPane shows per-service deployment status with a collapsible history of
// past deployments, and offers redeploy/restart.
type deploysPane struct {
	styles *theme.Styles
	env    string
	vp     viewport.Model

	services      []model.Service
	cursor        int // index into services
	width, height int
	ready         bool

	// per-service collapsible deployment history
	expanded map[string]bool               // serviceID -> expanded
	history  map[string][]model.Deployment // serviceID -> deployments
	loading  map[string]bool               // serviceID -> fetch in flight

	// confirm dialog
	confirming bool
	action     string // "redeploy" | "restart"
}

func newDeploysPane(styles *theme.Styles) *deploysPane {
	return &deploysPane{
		styles:   styles,
		expanded: map[string]bool{},
		history:  map[string][]model.Deployment{},
		loading:  map[string]bool{},
	}
}

func (p *deploysPane) setSize(w, h int) {
	p.width, p.height = w, h
	vpH := h - 3 // title + trailing help
	if vpH < 3 {
		vpH = 3
	}
	p.vp.Width = w
	p.vp.Height = vpH
	p.ready = true
	p.reflow()
}

func (p *deploysPane) setServices(env string, svcs []model.Service) {
	p.env = env
	p.services = svcs
	if p.cursor >= len(svcs) {
		p.cursor = max(0, len(svcs)-1)
	}
	p.reflow()
}

// setHistory stores fetched deployments for a service.
func (p *deploysPane) setHistory(serviceID string, deps []model.Deployment) {
	p.history[serviceID] = deps
	p.loading[serviceID] = false
	p.reflow()
}

// expandedServiceIDs returns the IDs of services currently expanded (used by
// the app to refresh their history on poll).
func (p *deploysPane) expandedServiceIDs() []string {
	var out []string
	for id, on := range p.expanded {
		if on {
			out = append(out, id)
		}
	}
	return out
}

// deployActionMsg asks the root to run an action against a service.
type deployActionMsg struct {
	action  string
	service model.Service
}

// loadDeploymentsMsg asks the root to fetch a service's deployment history.
type loadDeploymentsMsg struct {
	serviceID   string
	serviceName string
}

func (p *deploysPane) Update(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case tea.KeyMsg:
		if p.confirming {
			switch m.String() {
			case "y", "Y", "enter":
				p.confirming = false
				if svc, ok := p.selected(); ok {
					act := p.action
					return func() tea.Msg { return deployActionMsg{action: act, service: svc} }
				}
			case "n", "N", "esc":
				p.confirming = false
			}
			return nil
		}
		switch m.String() {
		case "up", "k":
			p.cursor = max(0, p.cursor-1)
			p.reflow()
		case "down", "j":
			p.cursor = min(len(p.services)-1, p.cursor+1)
			p.reflow()
		case "enter", " ", "o":
			return p.toggleExpand()
		case "R":
			if len(p.services) > 0 {
				p.confirming = true
				p.action = "redeploy"
			}
		case "x":
			if len(p.services) > 0 {
				p.confirming = true
				p.action = "restart"
			}
		case "F":
			if len(p.services) > 0 {
				p.confirming = true
				p.action = "from-source"
			}
		case "D":
			if len(p.services) > 0 {
				p.confirming = true
				p.action = "down"
			}
		case "pgup", "pgdown", "ctrl+u", "ctrl+d":
			var cmd tea.Cmd
			p.vp, cmd = p.vp.Update(msg)
			return cmd
		}
	}
	return nil
}

// toggleExpand flips the cursor service's expansion, loading history on demand.
func (p *deploysPane) toggleExpand() tea.Cmd {
	svc, ok := p.selected()
	if !ok {
		return nil
	}
	p.expanded[svc.ID] = !p.expanded[svc.ID]
	p.reflow()
	if p.expanded[svc.ID] && p.history[svc.ID] == nil && !p.loading[svc.ID] {
		p.loading[svc.ID] = true
		id, name := svc.ID, svc.Name
		return func() tea.Msg { return loadDeploymentsMsg{serviceID: id, serviceName: name} }
	}
	return nil
}

func (p *deploysPane) selected() (model.Service, bool) {
	if p.cursor >= 0 && p.cursor < len(p.services) {
		return p.services[p.cursor], true
	}
	return model.Service{}, false
}

// reflow rebuilds the scrollable content and keeps the cursor row visible.
func (p *deploysPane) reflow() {
	if !p.ready {
		return
	}
	if len(p.services) == 0 {
		p.vp.SetContent(p.styles.Dim.Render("No services in this environment. Loading…"))
		return
	}
	var b strings.Builder
	cursorLine := 0
	line := 0
	for i, s := range p.services {
		if i == p.cursor {
			cursorLine = line
		}
		b.WriteString(p.renderServiceRow(i, s))
		b.WriteString("\n")
		line++
		if p.expanded[s.ID] {
			for _, sub := range p.renderHistory(s) {
				b.WriteString(sub)
				b.WriteString("\n")
				line++
			}
		}
	}
	p.vp.SetContent(b.String())
	// Keep the cursor visible.
	if cursorLine < p.vp.YOffset {
		p.vp.SetYOffset(cursorLine)
	} else if cursorLine >= p.vp.YOffset+p.vp.Height {
		p.vp.SetYOffset(cursorLine - p.vp.Height + 1)
	}
}

func (p *deploysPane) renderServiceRow(i int, s model.Service) string {
	arrow := "▸"
	if p.expanded[s.ID] {
		arrow = "▾"
	}
	dot := p.styles.T.StatusDot(s.Status)
	name := s.Name
	statusStr := statusLabel(s.Status)
	status := lipgloss.NewStyle().Foreground(p.styles.T.StatusColor(s.Status)).Bold(s.Status.Active()).Render(statusStr)
	if s.Status.Active() {
		status = status + " " + lipgloss.NewStyle().Foreground(p.styles.T.Warn).Render("⟳")
	}
	reps := fmt.Sprintf("%d/%d", s.Replicas.Running, s.Replicas.Configured)
	if s.Replicas.Crashed > 0 {
		reps = lipgloss.NewStyle().Foreground(p.styles.T.Bad).Render(fmt.Sprintf("%d/%d ✗%d", s.Replicas.Running, s.Replicas.Configured, s.Replicas.Crashed))
	}
	age := "-"
	if s.LatestDeploy != nil && !s.LatestDeploy.CreatedAt.IsZero() {
		age = humanAge(s.LatestDeploy.CreatedAt)
	}

	prefix := "  "
	if i == p.cursor {
		prefix = p.styles.Title.Render("▎ ")
	}
	row := fmt.Sprintf("%s%s %s  %-22s  %-12s  %-9s  %s",
		prefix, p.styles.Dim.Render(arrow), dot, truncate(name, 22), status, reps, p.styles.Dim.Render(age))
	if i == p.cursor {
		row = lipgloss.NewStyle().Background(p.styles.T.BorderCol).Render(row)
	}
	return row
}

func (p *deploysPane) renderHistory(s model.Service) []string {
	if p.loading[s.ID] {
		return []string{p.styles.Dim.Render("      loading deployment history…")}
	}
	deps := p.history[s.ID]
	if len(deps) == 0 {
		return []string{p.styles.Dim.Render("      (no deployment history)")}
	}
	var out []string
	const limit = 12
	for i, d := range deps {
		if i >= limit {
			out = append(out, p.styles.Dim.Render(fmt.Sprintf("      … %d more", len(deps)-limit)))
			break
		}
		dot := p.styles.T.StatusDot(d.Status)
		st := lipgloss.NewStyle().Foreground(p.styles.T.StatusColor(d.Status)).Render(statusLabel(d.Status))
		age := "-"
		if !d.CreatedAt.IsZero() {
			age = humanAge(d.CreatedAt)
		}
		commit := d.CommitMessage
		if commit == "" {
			commit = d.ShortHash()
		}
		branch := ""
		if d.Branch != "" {
			branch = p.styles.Dim.Render(" (" + d.Branch + ")")
		}
		line := fmt.Sprintf("      %s %-12s %-12s  %s%s",
			dot, st, age, truncate(commit, 44), branch)
		out = append(out, line)
	}
	return out
}

func (p *deploysPane) View() string {
	head := p.styles.Title.Render(fmt.Sprintf("Deployments — %s", orDash(p.env)))
	body := p.vp.View()

	// The footer occupies exactly one row so the pane never grows past the
	// height it was sized to. When confirming, the warning banner takes the
	// help line's place (rather than being appended below, which pushed a
	// bordered box off the bottom of the pane). It's a flat highlighted line,
	// not a bordered box, so it can't overflow vertically.
	footer := p.styles.Help.Render("[enter]expand · [R]edeploy [x]restart [F]rom-source [D]own")
	if p.confirming {
		if svc, ok := p.selected(); ok {
			msg := fmt.Sprintf(" %s %q on %s?  [y/N] ", actionLabel(p.action), svc.Name, p.env)
			footer = warningFooter(p.styles, msg, p.width)
		}
	}
	return clampBlock(lipgloss.JoinVertical(lipgloss.Left, head, body, footer), p.width)
}

// actionLabel renders a deploy action as a readable confirmation verb.
func actionLabel(action string) string {
	switch action {
	case "from-source":
		return "REDEPLOY (from source)"
	case "down":
		return "REMOVE deployment"
	default:
		return strings.ToUpper(action)
	}
}

func statusLabel(s model.DeployStatus) string {
	if s == "" {
		return "UNKNOWN"
	}
	return string(s)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func humanAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
