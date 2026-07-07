package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"railway-tui/internal/model"
	"railway-tui/internal/ui/theme"
)

// TestNewPanesRenderNoOverflow renders the Metrics, Vars, and Service panes
// with representative data and asserts no row exceeds the pane width, guarding
// against layout regressions like those the logs pane historically had.
func TestNewPanesRenderNoOverflow(t *testing.T) {
	st := theme.NewStyles(theme.Default())
	svc := model.Service{
		ID:       "s1",
		Name:     "a-service-with-a-longish-name",
		Repo:     "org/a-service-with-a-longish-name",
		URL:      "https://a-service-with-a-longish-name.up.railway.app",
		Status:   model.StatusSuccess,
		Replicas: model.Replicas{Running: 2, Configured: 2},
		Regions:  []model.Region{{Name: "us-west", Configured: 1}, {Name: "eu-west", Configured: 1}},
		Volumes:  []model.Volume{{Name: "data", MountPath: "/data", CurrentSizeMB: 935, SizeMB: 50000}},
	}

	metrics := newMetricsPane(st)
	metrics.setService("production", svc)
	metrics.setMetrics("s1", &model.Metrics{Series: map[string]model.MetricSeries{
		"CPU_USAGE":       {Name: "CPU_USAGE", Points: pts(0.1, 0.3, 0.9)},
		"CPU_LIMIT":       {Name: "CPU_LIMIT", Points: pts(2, 2, 2)},
		"MEMORY_USAGE_GB": {Name: "MEMORY_USAGE_GB", Points: pts(0.02, 0.03)},
		"MEMORY_LIMIT_GB": {Name: "MEMORY_LIMIT_GB", Points: pts(1, 1)},
		"DISK_USAGE_GB":   {Name: "DISK_USAGE_GB", Points: pts(0)},
		"NETWORK_RX_GB":   {Name: "NETWORK_RX_GB", Points: pts(0.001)},
		"NETWORK_TX_GB":   {Name: "NETWORK_TX_GB", Points: pts(0.002)},
	}})

	vars := newVarsPane(st)
	vars.setService("production", svc)
	vars.setVars("s1", []model.Variable{
		{Name: "DATABASE_URL", Value: "postgres://user:pass@host:5432/db?sslmode=require"},
		{Name: "SHORT", Value: "x"},
	})

	service := newServicePane(st)
	service.setService("production", svc)
	service.setDomains("s1", []model.Domain{
		{Domain: "www.example.com", Type: "custom", TargetPort: 8080, SyncStatus: "ACTIVE"},
	})

	type pane interface {
		setSize(w, h int)
		View() string
	}
	panes := map[string]pane{"metrics": metrics, "vars": vars, "service": service}

	for _, size := range [][2]int{{100, 30}, {80, 24}, {60, 20}} {
		w, h := size[0], size[1]
		for name, p := range panes {
			p.setSize(w, h)
			for i, line := range strings.Split(p.View(), "\n") {
				if lw := lipgloss.Width(line); lw > w {
					t.Fatalf("%s @ %dx%d: line %d width %d exceeds %d:\n%q", name, w, h, i, lw, w, line)
				}
			}
		}
	}
}

func TestConfirmFootersStayInsidePane(t *testing.T) {
	st := theme.NewStyles(theme.Default())
	svc := model.Service{ID: "s1", Name: "a-service-with-a-very-long-name"}

	deploys := newDeploysPane(st)
	deploys.setServices("production-with-a-long-name", []model.Service{svc})
	deploys.confirming = true
	deploys.action = "from-source"

	vars := newVarsPane(st)
	vars.setService("production", svc)
	vars.setVars("s1", []model.Variable{{Name: "A_VERY_LONG_VARIABLE_NAME_THAT_MUST_CLIP", Value: "secret"}})
	vars.confirming = true

	service := newServicePane(st)
	service.setService("production", svc)
	service.setDomains("s1", []model.Domain{{Domain: "a-very-long-domain-name-that-must-clip.example.com"}})
	service.confirming = true

	type pane interface {
		setSize(w, h int)
		View() string
	}
	panes := map[string]pane{"deploys": deploys, "vars": vars, "service": service}

	for _, size := range [][2]int{{60, 20}, {40, 12}, {28, 8}} {
		w, h := size[0], size[1]
		for name, p := range panes {
			p.setSize(w, h)
			view := p.View()
			if got := lipgloss.Height(view); got > h {
				t.Fatalf("%s @ %dx%d: height %d exceeds %d:\n%s", name, w, h, got, h, view)
			}
			for i, line := range strings.Split(view, "\n") {
				if lw := lipgloss.Width(line); lw > w {
					t.Fatalf("%s @ %dx%d: line %d width %d exceeds %d:\n%q", name, w, h, i, lw, w, line)
				}
			}
		}
	}
}

func pts(vals ...float64) []model.MetricPoint {
	out := make([]model.MetricPoint, len(vals))
	for i, v := range vals {
		out[i] = model.MetricPoint{Value: v}
	}
	return out
}
