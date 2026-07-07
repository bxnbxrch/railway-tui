package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"railway-tui/internal/model"
	"railway-tui/internal/ui/theme"
)

// topologyPane renders project → environment → service as a tree. Selecting a
// service emits a focusServiceMsg so logs/metrics can follow.
type topologyPane struct {
	styles  *theme.Styles
	project *model.Project

	// flattened rows for cursor navigation
	rows          []topoRow
	cursor        int
	width, height int
}

type topoRow struct {
	env     string
	service *model.Service // nil for env header
}

func newTopologyPane(styles *theme.Styles) *topologyPane {
	return &topologyPane{styles: styles}
}

func (p *topologyPane) setSize(w, h int) { p.width, p.height = w, h }

func (p *topologyPane) setProject(proj *model.Project) {
	p.project = proj
	p.rebuild()
}

func (p *topologyPane) rebuild() {
	p.rows = nil
	if p.project == nil {
		return
	}
	for _, env := range p.project.Environments {
		p.rows = append(p.rows, topoRow{env: env.Name})
		for i := range env.Services {
			p.rows = append(p.rows, topoRow{env: env.Name, service: &env.Services[i]})
		}
	}
	if p.cursor >= len(p.rows) {
		p.cursor = max(0, len(p.rows)-1)
	}
}

// focusServiceMsg tells the app to point logs/metrics at a service+env.
type focusServiceMsg struct {
	env     string
	service model.Service
}

func (p *topologyPane) Update(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case tea.KeyMsg:
		switch m.String() {
		case "up", "k":
			p.moveCursor(-1)
		case "down", "j":
			p.moveCursor(1)
		case "enter":
			if p.cursor < len(p.rows) && p.rows[p.cursor].service != nil {
				r := p.rows[p.cursor]
				svc := *r.service
				return func() tea.Msg { return focusServiceMsg{env: r.env, service: svc} }
			}
		}
	}
	return nil
}

// moveCursor skips env-header rows so navigation lands on services.
func (p *topologyPane) moveCursor(delta int) {
	n := len(p.rows)
	if n == 0 {
		return
	}
	c := p.cursor
	for i := 0; i < n; i++ {
		c += delta
		if c < 0 {
			c = 0
			break
		}
		if c >= n {
			c = n - 1
			break
		}
		if p.rows[c].service != nil {
			break
		}
	}
	p.cursor = c
}

func (p *topologyPane) View() string {
	if p.project == nil {
		return p.styles.Dim.Render("Loading topology…")
	}
	var b strings.Builder
	title := p.project.Name
	if p.project.Workspace != "" {
		title += "  " + p.styles.Dim.Render("("+p.project.Workspace+")")
	}
	b.WriteString(p.styles.Title.Render("⬡ " + title))
	b.WriteString("\n\n")

	for i, r := range p.rows {
		if r.service == nil {
			b.WriteString(lipgloss.NewStyle().Foreground(p.styles.T.Accent).Bold(true).
				Render("┌ " + r.env))
			b.WriteString("\n")
			continue
		}
		s := r.service
		cursor := "│   "
		if i == p.cursor {
			cursor = p.styles.Title.Render("│ ▸ ")
		}
		dot := p.styles.T.StatusDot(s.Status)
		name := s.Name
		reps := ""
		if s.Replicas.Configured > 0 {
			reps = p.styles.Dim.Render(fmt.Sprintf(" ×%d", s.Replicas.Configured))
		}
		src := ""
		switch {
		case s.Repo != "":
			src = p.styles.Dim.Render("  " + s.Repo)
		case s.Image != "":
			src = p.styles.Dim.Render("  " + shortImage(s.Image))
		}
		url := ""
		if s.URL != "" {
			url = p.styles.Dim.Render("  " + strings.TrimPrefix(s.URL, "https://"))
		}
		line := fmt.Sprintf("%s%s %s%s%s%s", cursor, dot, name, reps, src, url)
		if i == p.cursor {
			line = lipgloss.NewStyle().Background(p.styles.T.BorderCol).Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(p.styles.Help.Render("[enter] focus logs+metrics on service  ↑↓ move"))
	b.WriteString("\n")
	b.WriteString(p.styles.Dim.Render("note: railway exposes structure, not inter-service call edges"))
	return b.String()
}

func shortImage(img string) string {
	if i := strings.LastIndex(img, "/"); i >= 0 {
		return img[i+1:]
	}
	return img
}
