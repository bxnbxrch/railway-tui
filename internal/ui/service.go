package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"railway-tui/internal/model"
	"railway-tui/internal/ui/theme"
)

// servicePane is a focused detail view for one service — source, status,
// replicas by region, volumes, and domains — plus domain management (generate /
// delete) and open-in-browser, mirroring the Railway web dashboard's service
// panel. Deploy actions live in the Deploys pane.
type servicePane struct {
	styles *theme.Styles
	vp     viewport.Model

	env     string
	service model.Service

	domains        []model.Domain
	loadingDomains bool
	domErr         string
	cursor         int // index into domains
	confirming     bool

	width, height int
	ready         bool
}

func newServicePane(styles *theme.Styles) *servicePane {
	return &servicePane{styles: styles}
}

func (p *servicePane) setSize(w, h int) {
	p.width, p.height = w, h
	vpH := h - 2
	if vpH < 3 {
		vpH = 3
	}
	p.vp.Width = w
	p.vp.Height = vpH
	p.ready = true
	p.reflow()
}

// setService points the pane at a service. Returns true if focus changed.
func (p *servicePane) setService(env string, svc model.Service) bool {
	changed := svc.ID != p.service.ID || env != p.env
	p.env = env
	p.service = svc
	if changed {
		p.domains = nil
		p.domErr = ""
		p.cursor = 0
		p.confirming = false
		p.loadingDomains = svc.ID != ""
		p.reflow()
	}
	return changed
}

// refreshService updates the service data in place (from a poll) without
// resetting domains/cursor.
func (p *servicePane) refreshService(svc model.Service) {
	if svc.ID == p.service.ID {
		p.service = svc
		p.reflow()
	}
}

func (p *servicePane) setDomains(serviceID string, ds []model.Domain) {
	if serviceID != p.service.ID {
		return
	}
	p.domains = ds
	p.loadingDomains = false
	p.domErr = ""
	if p.cursor >= len(ds) {
		p.cursor = max(0, len(ds)-1)
	}
	p.reflow()
}

func (p *servicePane) setDomainError(serviceID, msg string) {
	if serviceID != p.service.ID {
		return
	}
	p.loadingDomains = false
	p.domErr = msg
	p.reflow()
}

// domainActionMsg asks the root to generate or delete a domain.
type domainActionMsg struct {
	action  string // "generate" | "delete"
	service model.Service
	env     string
	domain  string
}

// openURLMsg asks the root to open a URL in the browser.
type openURLMsg struct {
	url   string
	label string
}

func (p *servicePane) Update(msg tea.Msg) tea.Cmd {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		p.vp, cmd = p.vp.Update(msg)
		return cmd
	}

	if p.confirming {
		switch km.String() {
		case "y", "Y", "enter":
			p.confirming = false
			if d, ok := p.selectedDomain(); ok {
				svc, env, dom := p.service, p.env, d.Domain
				return func() tea.Msg {
					return domainActionMsg{action: "delete", service: svc, env: env, domain: dom}
				}
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
		p.cursor = min(len(p.domains)-1, p.cursor+1)
		p.reflow()
	case "g":
		if p.service.ID != "" {
			svc, env := p.service, p.env
			p.loadingDomains = true
			p.reflow()
			return func() tea.Msg { return domainActionMsg{action: "generate", service: svc, env: env} }
		}
	case "d":
		if _, ok := p.selectedDomain(); ok {
			p.confirming = true
			p.reflow()
		}
	case "o":
		if url := p.openTarget(); url != "" {
			u := url
			return func() tea.Msg { return openURLMsg{url: u, label: "service"} }
		}
	case "r":
		if p.service.ID != "" {
			p.loadingDomains = true
			p.reflow()
			id, name := p.service.ID, p.service.Name
			return func() tea.Msg { return loadDomainsMsg{serviceID: id, serviceName: name} }
		}
	case "pgup", "pgdown", "ctrl+u", "ctrl+d":
		var cmd tea.Cmd
		p.vp, cmd = p.vp.Update(msg)
		return cmd
	}
	return nil
}

func (p *servicePane) selectedDomain() (model.Domain, bool) {
	if p.cursor >= 0 && p.cursor < len(p.domains) {
		return p.domains[p.cursor], true
	}
	return model.Domain{}, false
}

// openTarget picks a URL to open: the selected domain, else the service URL.
func (p *servicePane) openTarget() string {
	if d, ok := p.selectedDomain(); ok {
		return d.URL()
	}
	return p.service.URL
}

// loadDomainsMsg asks the root to (re)fetch domains for a service.
type loadDomainsMsg struct {
	serviceID   string
	serviceName string
}

func (p *servicePane) reflow() {
	if !p.ready {
		return
	}
	if p.service.ID == "" {
		p.vp.SetContent(p.styles.Dim.Render(
			"No service focused.\n\nPress [enter] on a service in the Topology pane to inspect it."))
		return
	}
	s := p.service
	dim := p.styles.Dim
	label := func(k string) string { return dim.Render(fmt.Sprintf("%-10s", k)) }

	var b strings.Builder

	// Status line.
	dot := p.styles.T.StatusDot(s.Status)
	statusStr := lipgloss.NewStyle().Foreground(p.styles.T.StatusColor(s.Status)).Bold(true).Render(statusLabel(s.Status))
	b.WriteString(label("Status") + dot + " " + statusStr + "\n")

	// Source.
	src := "—"
	switch {
	case s.Repo != "":
		src = s.Repo
	case s.Image != "":
		src = s.Image
	}
	b.WriteString(label("Source") + src + "\n")

	// URL.
	if s.URL != "" {
		b.WriteString(label("URL") + lipgloss.NewStyle().Foreground(p.styles.T.Active).Render(s.URL) + "\n")
	}

	// Replicas + regions.
	reps := fmt.Sprintf("%d/%d running", s.Replicas.Running, s.Replicas.Configured)
	if s.Replicas.Crashed > 0 {
		reps += lipgloss.NewStyle().Foreground(p.styles.T.Bad).Render(fmt.Sprintf("  ✗%d crashed", s.Replicas.Crashed))
	}
	b.WriteString(label("Replicas") + reps + "\n")
	if len(s.Regions) > 0 {
		var rs []string
		for _, r := range s.Regions {
			rs = append(rs, fmt.Sprintf("%s ×%d", r.Name, r.Configured))
		}
		b.WriteString(label("Regions") + strings.Join(rs, ", ") + "\n")
	}

	// Volumes.
	if len(s.Volumes) > 0 {
		for i, v := range s.Volumes {
			key := "Volumes"
			if i > 0 {
				key = ""
			}
			b.WriteString(label(key) + fmt.Sprintf("%s → %s  %s\n", v.Name, v.MountPath, humanMB(v.CurrentSizeMB, v.SizeMB)))
		}
	}

	// Latest deploy age.
	if s.LatestDeploy != nil && !s.LatestDeploy.CreatedAt.IsZero() {
		b.WriteString(label("Deployed") + humanAge(s.LatestDeploy.CreatedAt) + "\n")
	}

	// Domains section.
	b.WriteString("\n" + p.styles.Title.Render("Domains") + "\n")
	switch {
	case p.loadingDomains && len(p.domains) == 0:
		b.WriteString(dim.Render("  loading…") + "\n")
	case p.domErr != "":
		b.WriteString(lipgloss.NewStyle().Foreground(p.styles.T.Bad).Render("  "+p.domErr) + "\n")
	case len(p.domains) == 0:
		b.WriteString(dim.Render("  none — press [g] to generate a domain") + "\n")
	default:
		for i, d := range p.domains {
			cursor := "  "
			if i == p.cursor {
				cursor = p.styles.Title.Render("▸ ")
			}
			port := ""
			if d.TargetPort > 0 {
				port = dim.Render(fmt.Sprintf(":%d", d.TargetPort))
			}
			typ := dim.Render(fmt.Sprintf(" %-6s", orDash(d.Type)))
			sync := ""
			if d.SyncStatus != "" {
				sync = dim.Render(" " + d.SyncStatus)
			}
			name := lipgloss.NewStyle().Foreground(p.styles.T.Active).Render(d.Domain)
			row := cursor + name + port + typ + sync
			if i == p.cursor {
				row = lipgloss.NewStyle().Background(p.styles.T.BorderCol).Render(row)
			}
			b.WriteString(row + "\n")
		}
	}

	p.vp.SetContent(b.String())
}

func (p *servicePane) View() string {
	name := p.service.Name
	if name == "" {
		name = "—"
	}
	head := p.styles.Title.Render("Service — " + name)
	foot := p.styles.Help.Render("[g]enerate domain [d]elete [o]pen [r]efresh · ↑↓ move")
	if p.confirming {
		if d, ok := p.selectedDomain(); ok {
			foot = warningFooter(p.styles, fmt.Sprintf(" DELETE domain %s?  [y/N] ", d.Domain), p.width)
		}
	}
	return clampBlock(lipgloss.JoinVertical(lipgloss.Left, head, p.vp.View(), foot), p.width)
}

// humanMB renders "used / size" for a volume in MB/GB.
func humanMB(usedMB, sizeMB float64) string {
	f := func(mb float64) string {
		if mb >= 1024 {
			return fmt.Sprintf("%.1f GB", mb/1024)
		}
		return fmt.Sprintf("%.0f MB", mb)
	}
	if sizeMB > 0 {
		return f(usedMB) + " / " + f(sizeMB)
	}
	return f(usedMB)
}
