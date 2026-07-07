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

func TestIndeterminateBar(t *testing.T) {
	for _, frame := range []int{0, 1, 5, 9, 12} {
		bar := indeterminateBar(frame, 10)
		if n := len([]rune(bar)); n != 10 {
			t.Fatalf("frame %d: want width 10, got %d (%q)", frame, n, bar)
		}
		if !strings.ContainsRune(bar, '█') || !strings.ContainsRune(bar, '░') {
			t.Fatalf("frame %d: bar should have lit and unlit cells: %q", frame, bar)
		}
	}
}

func TestHumanElapsed(t *testing.T) {
	now := time.Now()
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{5 * time.Second, "5s"},
		{80 * time.Second, "1m20s"},
		{2 * time.Hour, "2h00m"},
	}
	for _, c := range cases {
		if got := humanElapsed(now.Add(-c.ago)); got != c.want {
			t.Errorf("humanElapsed(-%v) = %q, want %q", c.ago, got, c.want)
		}
	}
}

func TestCollapseWS(t *testing.T) {
	if got := collapseWS("build   step\n\tnext  line"); got != "build step next line" {
		t.Errorf("collapseWS wrong: %q", got)
	}
}

// TestDeployProgressOverlay renders the overlay for an in-flight deploy and
// asserts it appears, is empty when idle, and never overflows the frame width.
func TestDeployProgressOverlay(t *testing.T) {
	app := New(config.Default(), railwaycli.New())

	// Idle: no active deploys → no overlay.
	app.services = []model.Service{{ID: "1", Name: "web", Status: model.StatusSuccess}}
	app.width, app.height, app.ready = 100, 30, true
	if got := app.deployProgressOverlay(); got != "" {
		t.Fatalf("idle should render no overlay, got:\n%s", got)
	}

	// Building: overlay appears.
	app.services = []model.Service{
		{ID: "1", Name: "web", Status: model.StatusBuilding, LatestDeploy: &model.Deployment{CreatedAt: time.Now().Add(-90 * time.Second)}},
		{ID: "2", Name: "worker", Status: model.StatusDeploying, LatestDeploy: &model.Deployment{CreatedAt: time.Now().Add(-5 * time.Second)}},
	}
	if !app.hasActiveDeploys() {
		t.Fatal("expected active deploys")
	}
	if got := app.deployProgressOverlay(); got == "" || !strings.Contains(got, "Deploying") {
		t.Fatalf("expected a Deploying overlay, got:\n%q", got)
	}

	// Composited full frame must not overflow the terminal width at any size.
	for _, size := range [][2]int{{120, 40}, {100, 30}, {80, 24}} {
		w, h := size[0], size[1]
		app.width, app.height, app.ready = w, h, true
		app.resize()
		for i, line := range strings.Split(app.View(), "\n") {
			if lw := lipgloss.Width(line); lw > w {
				t.Fatalf("size %dx%d: line %d width %d exceeds %d:\n%q", w, h, i, lw, w, line)
			}
		}
	}
}
