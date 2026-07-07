// Package theme centralizes colors and styles so panes stay visually
// consistent and the palette can be swapped from config.
package theme

import (
	"hash/fnv"

	"github.com/charmbracelet/lipgloss"

	"railway-tui/internal/model"
)

// Theme holds the active palette.
type Theme struct {
	Bg        lipgloss.Color
	Fg        lipgloss.Color
	Dim       lipgloss.Color
	Accent    lipgloss.Color
	Good      lipgloss.Color
	Warn      lipgloss.Color
	Bad       lipgloss.Color
	Active    lipgloss.Color
	BorderCol lipgloss.Color

	// sourcePalette is cycled to color per-service log tags.
	sourcePalette []lipgloss.Color
}

// Default returns the built-in dark theme.
func Default() *Theme {
	return &Theme{
		Bg:        lipgloss.Color("#0b0e14"),
		Fg:        lipgloss.Color("#c9d1d9"),
		Dim:       lipgloss.Color("#6e7681"),
		Accent:    lipgloss.Color("#a277ff"),
		Good:      lipgloss.Color("#61ffca"),
		Warn:      lipgloss.Color("#ffca85"),
		Bad:       lipgloss.Color("#ff6767"),
		Active:    lipgloss.Color("#82aaff"),
		BorderCol: lipgloss.Color("#30363d"),
		sourcePalette: []lipgloss.Color{
			"#82aaff", "#61ffca", "#ffca85", "#c3a6ff",
			"#ff6ac1", "#5fd7ff", "#f7c948", "#8ce99a",
			"#ff9e64", "#7dcfff", "#bb9af7", "#e0af68",
		},
	}
}

// SourceColor returns a stable color for a service name.
func (t *Theme) SourceColor(name string) lipgloss.Color {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return t.sourcePalette[int(h.Sum32())%len(t.sourcePalette)]
}

// StatusColor maps a deploy status to a color.
func (t *Theme) StatusColor(s model.DeployStatus) lipgloss.Color {
	switch {
	case s.Bad():
		return t.Bad
	case s.Active():
		return t.Warn
	case s == model.StatusSuccess:
		return t.Good
	default:
		return t.Dim
	}
}

// StatusDot returns a colored status glyph.
func (t *Theme) StatusDot(s model.DeployStatus) string {
	return lipgloss.NewStyle().Foreground(t.StatusColor(s)).Render("●")
}

// LevelColor maps a log level string to a color.
func (t *Theme) LevelColor(level string) lipgloss.Color {
	switch level {
	case "error", "fatal", "err", "critical":
		return t.Bad
	case "warn", "warning":
		return t.Warn
	case "debug", "trace":
		return t.Dim
	default:
		return t.Fg
	}
}

// Styles bundles reusable component styles derived from the theme.
type Styles struct {
	T *Theme

	Title        lipgloss.Style
	TabActive    lipgloss.Style
	TabInactive  lipgloss.Style
	StatusBar    lipgloss.Style
	Border       lipgloss.Style
	BorderActive lipgloss.Style
	Dim          lipgloss.Style
	Help         lipgloss.Style
	Toast        lipgloss.Style
	ToastBad     lipgloss.Style
	ToastWarn    lipgloss.Style
	ToastGood    lipgloss.Style
}

// NewStyles builds styles from a theme.
func NewStyles(t *Theme) *Styles {
	base := lipgloss.NewStyle()
	return &Styles{
		T:            t,
		Title:        base.Foreground(t.Accent).Bold(true),
		TabActive:    base.Foreground(t.Bg).Background(t.Accent).Bold(true).Padding(0, 1),
		TabInactive:  base.Foreground(t.Dim).Padding(0, 1),
		StatusBar:    base.Foreground(t.Dim).Background(lipgloss.Color("#161b22")),
		Border:       base.Border(lipgloss.RoundedBorder()).BorderForeground(t.BorderCol),
		BorderActive: base.Border(lipgloss.RoundedBorder()).BorderForeground(t.Accent),
		Dim:          base.Foreground(t.Dim),
		Help:         base.Foreground(t.Dim),
		Toast: base.Border(lipgloss.RoundedBorder()).Padding(0, 1).
			BorderForeground(t.Accent).Foreground(t.Fg),
		ToastBad: base.Border(lipgloss.RoundedBorder()).Padding(0, 1).
			BorderForeground(t.Bad).Foreground(t.Fg),
		ToastWarn: base.Border(lipgloss.RoundedBorder()).Padding(0, 1).
			BorderForeground(t.Warn).Foreground(t.Fg),
		ToastGood: base.Border(lipgloss.RoundedBorder()).Padding(0, 1).
			BorderForeground(t.Good).Foreground(t.Fg),
	}
}
