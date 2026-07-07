package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"railway-tui/internal/config"
	"railway-tui/internal/model"
	"railway-tui/internal/railwaycli"
)

// TestSplitLayoutNoOverflow renders the default split layout (logs+deploys)
// and asserts no row exceeds the terminal width — guarding the bug where panes
// were sized to the full column width but drawn inside a border, wrapping the
// logs sidebar. It populates panes directly so no real `railway` process runs.
func TestSplitLayoutNoOverflow(t *testing.T) {
	app := New(config.Default(), railwaycli.New())

	svcs := []model.Service{
		{ID: "1", Name: "unity-backend", Status: model.StatusSuccess, Replicas: model.Replicas{Running: 1, Configured: 1}},
		{ID: "2", Name: "Database", Status: model.StatusSuccess, Replicas: model.Replicas{Running: 1, Configured: 1}},
	}
	app.services = svcs
	app.deploys.setServices("dev", svcs)
	app.logs.setSources([]model.Source{
		{ServiceName: "unity-backend", Environment: "dev", Kind: model.LogDeploy},
		{ServiceName: "Database", Environment: "dev", Kind: model.LogDeploy},
	})
	base := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 30; i++ {
		app.logs.append(model.LogLine{
			Source:    model.Source{ServiceName: "unity-backend", Environment: "dev", Kind: model.LogDeploy},
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Level:     "info",
			Message:   strings.Repeat("a long log line that must be clipped inside the split column ", 4),
		})
	}

	for _, size := range [][2]int{{120, 40}, {100, 30}, {80, 24}} {
		w, h := size[0], size[1]
		app.width, app.height, app.ready = w, h, true
		app.resize()
		view := app.View()
		for i, line := range strings.Split(view, "\n") {
			if lw := lipgloss.Width(line); lw > w {
				t.Fatalf("size %dx%d: line %d width %d exceeds %d:\n%q", w, h, i, lw, w, line)
			}
		}
	}
}
