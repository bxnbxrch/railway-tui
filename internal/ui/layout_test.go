package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"railway-tui/internal/model"
	"railway-tui/internal/ui/theme"
)

// TestLogsPaneNoOverflow verifies that with the sidebar shown and long log
// lines, no rendered row exceeds the pane width (which previously caused the
// checkboxes to be overwritten by wrapped log text).
func TestLogsPaneNoOverflow(t *testing.T) {
	p := newLogsPane(theme.NewStyles(theme.Default()))
	p.setSources([]model.Source{
		{ServiceName: "unity-backend", Environment: "dev", Kind: model.LogDeploy},
		{ServiceName: "unity-backend", Environment: "dev", Kind: model.LogBuild},
		{ServiceName: "unity-backend", Environment: "dev", Kind: model.LogHTTP},
		{ServiceName: "Database", Environment: "dev", Kind: model.LogDeploy},
	})
	base := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 40; i++ {
		p.append(model.LogLine{
			Source:    model.Source{ServiceName: "unity-backend", Environment: "dev", Kind: model.LogDeploy},
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Level:     "info",
			Message:   strings.Repeat("this is a very long log line that should be clipped ", 6),
		})
	}

	const width = 100
	p.setSize(width, 30)
	view := p.View()

	for i, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("line %d width %d exceeds pane width %d:\n%q", i, w, width, line)
		}
	}
}

// TestLogsPaneNarrow ensures the pane still lays out without overflow at a
// small terminal size.
func TestLogsPaneNarrow(t *testing.T) {
	p := newLogsPane(theme.NewStyles(theme.Default()))
	p.setSources([]model.Source{
		{ServiceName: "svc-with-a-fairly-long-name", Environment: "prod", Kind: model.LogDeploy},
	})
	p.append(model.LogLine{
		Source:  model.Source{ServiceName: "svc-with-a-fairly-long-name", Environment: "prod", Kind: model.LogHTTP},
		Level:   "error",
		Message: strings.Repeat("boom ", 30),
	})
	const width = 60
	p.setSize(width, 20)
	for i, line := range strings.Split(p.View(), "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("narrow: line %d width %d > %d", i, w, width)
		}
	}
}
