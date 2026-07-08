package ui

import (
	"testing"
	"time"

	"railway-tui/internal/config"
	"railway-tui/internal/model"
	"railway-tui/internal/ui/theme"
)

func TestSparklineWidth(t *testing.T) {
	got := sparkline([]float64{1, 2, 3, 4, 5, 6, 7, 8}, 8)
	if len([]rune(got)) != 8 {
		t.Fatalf("want width 8, got %d (%q)", len([]rune(got)), got)
	}
	// Downsample: 100 values into 20 cols.
	got = sparkline(make([]float64, 100), 20)
	if len([]rune(got)) != 20 {
		t.Fatalf("want width 20, got %d", len([]rune(got)))
	}
	// Empty is padded, not panicking.
	if got := sparkline(nil, 5); len([]rune(got)) != 5 {
		t.Fatalf("empty spark want 5 spaces, got %q", got)
	}
}

func TestLogFilter(t *testing.T) {
	p := newLogsPane(theme.NewStyles(theme.Default()))
	line := model.LogLine{
		Source:  model.Source{ServiceName: "api", Kind: model.LogDeploy},
		Level:   "error",
		Message: "connection refused to postgres",
	}
	cases := []struct {
		filter string
		want   bool
	}{
		{"", true},
		{"postgres", true},
		{"MISSING", false},
		{"@level:error", true},
		{"@level:warn", false},
		{"[api]", true},
		{"[web]", false},
	}
	for _, c := range cases {
		p.filter.SetValue(c.filter)
		if got := p.matches(line); got != c.want {
			t.Errorf("filter %q: want %v got %v", c.filter, c.want, got)
		}
	}
}

func TestLogAppendOrdersByTime(t *testing.T) {
	p := newLogsPane(theme.NewStyles(theme.Default()))
	p.activeKey[model.Source{}.Key()] = true
	base := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	// Insert out of order; buffer should end sorted.
	p.append(model.LogLine{Timestamp: base.Add(2 * time.Second), Message: "c"})
	p.append(model.LogLine{Timestamp: base.Add(1 * time.Second), Message: "b"})
	p.append(model.LogLine{Timestamp: base, Message: "a"})
	want := []string{"a", "b", "c"}
	for i, w := range want {
		if p.buf[i].Message != w {
			t.Errorf("pos %d: want %q got %q", i, w, p.buf[i].Message)
		}
	}
}

func TestWatcherDeployTransitions(t *testing.T) {
	cfg := config.Default().Notifications
	w := newWatcher(cfg)

	// First observation: no notifications (no prior state).
	if got := w.onServices([]model.Service{{Name: "api", Status: model.StatusBuilding}}); len(got) != 0 {
		t.Fatalf("first snapshot should be silent, got %d", len(got))
	}
	// Building -> Success => "DEPLOYED".
	notes := w.onServices([]model.Service{{Name: "api", Status: model.StatusSuccess}})
	if len(notes) != 1 || notes[0].sev != sevGood {
		t.Fatalf("want 1 good note, got %+v", notes)
	}
	// Success -> Crashed => bad.
	notes = w.onServices([]model.Service{{Name: "api", Status: model.StatusCrashed}})
	if len(notes) != 1 || notes[0].sev != sevBad {
		t.Fatalf("want 1 bad note, got %+v", notes)
	}
	// No change => silent.
	if notes = w.onServices([]model.Service{{Name: "api", Status: model.StatusCrashed}}); len(notes) != 0 {
		t.Fatalf("unchanged status should be silent, got %d", len(notes))
	}
}

func TestWatcherLogErrorRule(t *testing.T) {
	cfg := config.Default().Notifications
	w := newWatcher(cfg)
	// level-based
	if n := w.onLogLine(model.LogLine{Level: "error", Message: "boom", Source: model.Source{ServiceName: "api"}}); n == nil {
		t.Fatal("error level should notify")
	}
	// pattern-based (panic is a default pattern)
	if n := w.onLogLine(model.LogLine{Level: "info", Message: "got a panic here", Source: model.Source{ServiceName: "api"}}); n == nil {
		t.Fatal("panic pattern should notify")
	}
	// clean info line
	if n := w.onLogLine(model.LogLine{Level: "info", Message: "all good", Source: model.Source{ServiceName: "api"}}); n != nil {
		t.Fatal("clean line should not notify")
	}
	// muted service
	w.setConfig(config.Notifications{OnLogError: true, MutedServices: []string{"api"}})
	if n := w.onLogLine(model.LogLine{Level: "error", Message: "boom", Source: model.Source{ServiceName: "api"}}); n != nil {
		t.Fatal("muted service should not notify")
	}
}

func TestSourceKeyStable(t *testing.T) {
	a := model.Source{ServiceName: "api", Environment: "prod", Kind: model.LogDeploy}
	b := model.Source{ServiceName: "api", Environment: "prod", Kind: model.LogBuild}
	if a.Key() == b.Key() {
		t.Fatal("different kinds must have different keys")
	}
	if a.Key() != (model.Source{ServiceName: "api", Environment: "prod", Kind: model.LogDeploy}).Key() {
		t.Fatal("same source must have same key")
	}
}
