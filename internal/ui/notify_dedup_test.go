package ui

import (
	"testing"
	"time"

	"railway-tui/internal/model"
	"railway-tui/internal/ui/theme"
)

func TestIsContinuous(t *testing.T) {
	cases := []struct {
		kind model.LogKind
		want bool
	}{
		{model.LogDeploy, true},
		{model.LogHTTP, true},
		{model.LogNetwork, true},
		{model.LogBuild, false},
	}
	for _, c := range cases {
		if got := isContinuous(c.kind); got != c.want {
			t.Errorf("isContinuous(%v) = %v, want %v", c.kind, got, c.want)
		}
	}
}

// TestAppendReturnsFalseOnDuplicate locks in the contract the app relies on to
// avoid re-triggering error entries/toasts for replayed lines: append reports
// whether a line was newly added.
func TestAppendReturnsFalseOnDuplicate(t *testing.T) {
	p := newLogsPane(theme.NewStyles(theme.Default()))
	ts := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	ll := model.LogLine{
		Source:    model.Source{ServiceName: "web", Kind: model.LogBuild},
		Timestamp: ts,
		Level:     "error",
		Message:   "No package manager could be inferred",
	}
	if ok := p.append(ll); !ok {
		t.Fatal("first append should report true (new line)")
	}
	if ok := p.append(ll); ok {
		t.Fatal("replayed duplicate append should report false")
	}
	if ok := p.append(ll); ok {
		t.Fatal("second replay should also report false")
	}
}
