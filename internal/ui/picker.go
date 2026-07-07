package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"railway-tui/internal/model"
	"railway-tui/internal/ui/theme"
)

// pickerMode is which column of the switcher is focused.
type pickerMode int

const (
	pickProject pickerMode = iota
	pickEnv
)

// picker is a modal overlay for switching project and environment. It shows
// projects on the left and the selected project's environments on the right.
type picker struct {
	styles *theme.Styles
	active bool
	mode   pickerMode

	projects []model.ProjectRef
	projCur  int
	envCur   int

	width, height int
	loading       bool
	err           string
}

func newPicker(styles *theme.Styles) *picker {
	return &picker{styles: styles}
}

func (p *picker) open(projects []model.ProjectRef, curProject, curEnv string) {
	p.active = true
	p.mode = pickProject
	p.projects = projects
	p.err = ""
	p.loading = projects == nil
	// Position cursors on current selection.
	p.projCur, p.envCur = 0, 0
	for i, pr := range projects {
		if pr.ID == curProject || pr.Name == curProject {
			p.projCur = i
			for j, e := range pr.Envs {
				if e.ID == curEnv || e.Name == curEnv {
					p.envCur = j
				}
			}
		}
	}
}

func (p *picker) setProjects(projects []model.ProjectRef) {
	p.projects = projects
	p.loading = false
}

func (p *picker) setError(e string) {
	p.err = e
	p.loading = false
}

func (p *picker) close() { p.active = false }

func (p *picker) curEnvs() []model.EnvRef {
	if p.projCur < len(p.projects) {
		return p.projects[p.projCur].Envs
	}
	return nil
}

// pickerChoiceMsg is emitted when the user confirms a project+env.
type pickerChoiceMsg struct {
	project model.ProjectRef
	env     model.EnvRef
}

func (p *picker) Update(msg tea.Msg) tea.Cmd {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch km.String() {
	case "esc", "q":
		p.close()
	case "left", "h":
		p.mode = pickProject
	case "right", "l", "tab":
		p.mode = pickEnv
	case "up", "k":
		if p.mode == pickProject {
			p.projCur = max(0, p.projCur-1)
			p.envCur = 0
		} else {
			p.envCur = max(0, p.envCur-1)
		}
	case "down", "j":
		if p.mode == pickProject {
			p.projCur = min(len(p.projects)-1, p.projCur+1)
			p.envCur = 0
		} else {
			p.envCur = min(len(p.curEnvs())-1, p.envCur+1)
		}
	case "enter":
		if p.mode == pickProject {
			// Move to env selection for this project.
			p.mode = pickEnv
			return nil
		}
		envs := p.curEnvs()
		if p.projCur < len(p.projects) && p.envCur < len(envs) {
			proj := p.projects[p.projCur]
			env := envs[p.envCur]
			p.close()
			return func() tea.Msg { return pickerChoiceMsg{project: proj, env: env} }
		}
	}
	return nil
}

func (p *picker) View() string {
	if !p.active {
		return ""
	}
	w := p.width - 8
	if w > 80 {
		w = 80
	}
	colW := w/2 - 2

	var title string = p.styles.Title.Render("Switch Project / Environment")

	var left, right strings.Builder
	left.WriteString(p.styles.Help.Render("PROJECTS") + "\n")
	if p.loading {
		left.WriteString(p.styles.Dim.Render("loading…"))
	}
	for i, pr := range p.projects {
		label := pr.Name
		if pr.Workspace != "" {
			label += " " + p.styles.Dim.Render("· "+pr.Workspace)
		}
		left.WriteString(p.selRow(label, i == p.projCur, p.mode == pickProject, colW))
		left.WriteString("\n")
	}

	right.WriteString(p.styles.Help.Render("ENVIRONMENTS") + "\n")
	for i, e := range p.curEnvs() {
		right.WriteString(p.selRow(e.Name, i == p.envCur, p.mode == pickEnv, colW))
		right.WriteString("\n")
	}

	leftBox := lipgloss.NewStyle().Width(colW).Render(left.String())
	rightBox := lipgloss.NewStyle().Width(colW).Render(right.String())
	cols := lipgloss.JoinHorizontal(lipgloss.Top, leftBox, "  ", rightBox)

	help := p.styles.Help.Render("←→ switch column · ↑↓ move · enter select · esc cancel")
	if p.err != "" {
		help = lipgloss.NewStyle().Foreground(p.styles.T.Bad).Render(p.err)
	}

	content := lipgloss.JoinVertical(lipgloss.Left, title, "", cols, "", help)
	return p.styles.BorderActive.Width(w).Padding(1, 2).Render(content)
}

func (p *picker) selRow(label string, selected, focused bool, w int) string {
	prefix := "  "
	style := lipgloss.NewStyle()
	if selected {
		prefix = "▸ "
		if focused {
			style = style.Foreground(p.styles.T.Accent).Bold(true)
		} else {
			style = style.Foreground(p.styles.T.Fg)
		}
	} else {
		style = p.styles.Dim
	}
	return style.Render(prefix + label)
}
