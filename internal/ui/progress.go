package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"railway-tui/internal/model"
)

// spinnerFrames is a braille spinner cycled by the progress animation.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const (
	progressPanelW = 42
	progressMaxRow = 4
)

// deployProgressOverlay renders a compact panel for services currently building
// or deploying: a spinner, phase, elapsed time, an indeterminate progress bar,
// and the latest log line — so you can watch a redeploy/build as it happens.
// Returns "" when nothing is in flight.
func (a *App) deployProgressOverlay() string {
	active := a.activeDeploys()
	if len(active) == 0 {
		return ""
	}
	// Adapt the panel to the terminal so it never overflows a narrow frame; if
	// there's not even room for a minimal panel, skip the overlay entirely.
	pw := progressPanelW
	if a.width-4 < pw {
		pw = a.width - 4
	}
	if pw < 20 {
		return ""
	}
	dim := a.styles.Dim
	spin := spinnerFrames[a.progressFrame%len(spinnerFrames)]

	var b strings.Builder
	header := lipgloss.NewStyle().Foreground(a.theme.Warn).Bold(true).Render(spin + " Deploying")
	b.WriteString(header)
	b.WriteString("  " + dim.Render(fmt.Sprintf("%d in progress", len(active))))
	b.WriteString("\n")

	shown := active
	extra := 0
	if len(shown) > progressMaxRow {
		extra = len(shown) - progressMaxRow
		shown = shown[:progressMaxRow]
	}

	for _, s := range shown {
		col := a.theme.Warn
		if s.Status == model.StatusDeploying {
			col = a.theme.Active
		}
		name := lipgloss.NewStyle().Foreground(a.theme.SourceColor(s.Name)).Bold(true).
			Render(truncate(s.Name, 16))
		phase := lipgloss.NewStyle().Foreground(col).Render(statusLabel(s.Status))
		elapsed := ""
		if s.LatestDeploy != nil && !s.LatestDeploy.CreatedAt.IsZero() {
			elapsed = dim.Render(humanElapsed(s.LatestDeploy.CreatedAt))
		}
		b.WriteString(fmt.Sprintf("%s  %s  %s\n", name, phase, elapsed))

		bar := lipgloss.NewStyle().Foreground(col).Render(indeterminateBar(a.progressFrame, 10))
		last := a.logs.lastMessageFor(s.Name)
		line := dim.Render("waiting for output…")
		if last != "" {
			line = dim.Render(truncate(collapseWS(last), max(6, pw-13)))
		}
		b.WriteString("  " + bar + " " + line + "\n")
	}
	if extra > 0 {
		b.WriteString(dim.Render(fmt.Sprintf("  … +%d more", extra)))
	}

	content := clampBlock(strings.TrimRight(b.String(), "\n"), pw)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(a.theme.Warn).
		Padding(0, 1).
		Render(content)
	return box
}

// indeterminateBar renders a fixed-width bar with a lit segment that sweeps
// (and wraps) across it, driven by frame — an honest "in progress" indicator
// since Railway does not expose a build completion percentage.
func indeterminateBar(frame, width int) string {
	if width < 4 {
		width = 4
	}
	const seg = 3
	pos := frame % width
	var b strings.Builder
	for i := 0; i < width; i++ {
		if (i-pos+width)%width < seg {
			b.WriteRune('█')
		} else {
			b.WriteRune('░')
		}
	}
	return b.String()
}

// humanElapsed formats a running duration compactly (e.g. "42s", "1m20s").
func humanElapsed(t time.Time) string {
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	secs := int(d.Seconds())
	switch {
	case secs < 60:
		return fmt.Sprintf("%ds", secs)
	case secs < 3600:
		return fmt.Sprintf("%dm%02ds", secs/60, secs%60)
	default:
		return fmt.Sprintf("%dh%02dm", secs/3600, (secs%3600)/60)
	}
}

// collapseWS flattens newlines/tabs/runs of spaces so a multi-line log message
// renders as a single tidy line in the overlay.
func collapseWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
