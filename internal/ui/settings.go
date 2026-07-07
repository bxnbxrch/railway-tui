package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"railway-tui/internal/config"
	"railway-tui/internal/ui/theme"
)

// settingsPane is an in-TUI editor for the common config toggles. The YAML
// file remains the source of truth and manual-edit escape hatch.
type settingsPane struct {
	styles        *theme.Styles
	cfg           *config.Config
	cursor        int
	width, height int
	saved         bool
	logPath       string
}

func newSettingsPane(styles *theme.Styles, cfg *config.Config) *settingsPane {
	return &settingsPane{styles: styles, cfg: cfg}
}

func (p *settingsPane) setSize(w, h int) { p.width, p.height = w, h }

// settingItem describes one editable row.
type settingItem struct {
	label string
	get   func(*config.Config) string
	// toggle/adjust mutate the config; ok reports if handled.
	left  func(*config.Config)
	right func(*config.Config)
}

func (p *settingsPane) items() []settingItem {
	boolStr := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	return []settingItem{
		{
			label: "Notify: deploy success",
			get:   func(c *config.Config) string { return boolStr(c.Notifications.OnDeploySuccess) },
			left:  func(c *config.Config) { c.Notifications.OnDeploySuccess = !c.Notifications.OnDeploySuccess },
			right: func(c *config.Config) { c.Notifications.OnDeploySuccess = !c.Notifications.OnDeploySuccess },
		},
		{
			label: "Notify: deploy failed",
			get:   func(c *config.Config) string { return boolStr(c.Notifications.OnDeployFail) },
			left:  func(c *config.Config) { c.Notifications.OnDeployFail = !c.Notifications.OnDeployFail },
			right: func(c *config.Config) { c.Notifications.OnDeployFail = !c.Notifications.OnDeployFail },
		},
		{
			label: "Notify: crash",
			get:   func(c *config.Config) string { return boolStr(c.Notifications.OnCrash) },
			left:  func(c *config.Config) { c.Notifications.OnCrash = !c.Notifications.OnCrash },
			right: func(c *config.Config) { c.Notifications.OnCrash = !c.Notifications.OnCrash },
		},
		{
			label: "Notify: log errors",
			get:   func(c *config.Config) string { return boolStr(c.Notifications.OnLogError) },
			left:  func(c *config.Config) { c.Notifications.OnLogError = !c.Notifications.OnLogError },
			right: func(c *config.Config) { c.Notifications.OnLogError = !c.Notifications.OnLogError },
		},
		{
			label: "Toast duration (s)",
			get:   func(c *config.Config) string { return fmt.Sprintf("%d", c.Notifications.ToastSeconds) },
			left:  func(c *config.Config) { c.Notifications.ToastSeconds = clampi(c.Notifications.ToastSeconds-1, 1, 60) },
			right: func(c *config.Config) { c.Notifications.ToastSeconds = clampi(c.Notifications.ToastSeconds+1, 1, 60) },
		},
		{
			label: "Deploy poll (s)",
			get:   func(c *config.Config) string { return fmt.Sprintf("%d", c.Polling.DeploySeconds) },
			left:  func(c *config.Config) { c.Polling.DeploySeconds = clampi(c.Polling.DeploySeconds-5, 5, 300) },
			right: func(c *config.Config) { c.Polling.DeploySeconds = clampi(c.Polling.DeploySeconds+5, 5, 300) },
		},
	}
}

// settingsChangedMsg tells the app to re-apply config (poll intervals, etc.).
type settingsChangedMsg struct{}

func (p *settingsPane) Update(msg tea.Msg) tea.Cmd {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	items := p.items()
	switch km.String() {
	case "up", "k":
		p.cursor = max(0, p.cursor-1)
	case "down", "j":
		p.cursor = min(len(items)-1, p.cursor+1)
	case "left", "h":
		if p.cursor < len(items) {
			items[p.cursor].left(p.cfg)
			return p.changed()
		}
	case "right", "l", " ", "enter":
		if p.cursor < len(items) {
			items[p.cursor].right(p.cfg)
			return p.changed()
		}
	case "S":
		if err := p.cfg.Save(); err == nil {
			p.saved = true
		}
	}
	return nil
}

func (p *settingsPane) changed() tea.Cmd {
	p.saved = false
	return func() tea.Msg { return settingsChangedMsg{} }
}

func (p *settingsPane) View() string {
	var b strings.Builder
	b.WriteString(p.styles.Title.Render("Settings"))
	b.WriteString(p.styles.Dim.Render("   " + config.Path()))
	b.WriteString("\n\n")

	for i, it := range p.items() {
		cursor := "  "
		labelStyle := p.styles.Dim
		if i == p.cursor {
			cursor = p.styles.Title.Render("▸ ")
			labelStyle = lipgloss.NewStyle().Foreground(p.styles.T.Fg).Bold(true)
		}
		val := lipgloss.NewStyle().Foreground(p.styles.T.Accent).Render(it.get(p.cfg))
		b.WriteString(fmt.Sprintf("%s%s  %s\n", cursor, labelStyle.Render(fmt.Sprintf("%-24s", it.label)), val))
	}

	b.WriteString("\n")
	b.WriteString(p.styles.Help.Render("←→/space adjust · ↑↓ move · [S]ave to disk"))
	if p.saved {
		b.WriteString("   " + lipgloss.NewStyle().Foreground(p.styles.T.Good).Render("✔ saved"))
	}
	b.WriteString("\n\n")
	b.WriteString(p.styles.Dim.Render("Layouts & error patterns: edit the YAML directly (auto-loaded on next start)."))
	if p.logPath != "" {
		b.WriteString("\n")
		b.WriteString(p.styles.Dim.Render("Debug log: " + p.logPath))
	}
	return b.String()
}

func clampi(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
