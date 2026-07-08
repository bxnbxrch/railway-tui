package ui

import (
	"testing"
	"time"

	"railway-tui/internal/model"
	"railway-tui/internal/ui/theme"
)

// TestLogDedup verifies replayed lines (same source/time/message) are dropped,
// so tail + stream + reconnect overlap doesn't produce visible duplicates.
func TestLogDedup(t *testing.T) {
	p := newLogsPane(theme.NewStyles(theme.Default()))
	ts := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	ll := model.LogLine{
		Source:    model.Source{ServiceName: "api", Environment: "dev", Kind: model.LogDeploy},
		Timestamp: ts,
		Message:   "started",
	}
	p.activeKey[ll.Source.Key()] = true
	p.append(ll)
	p.append(ll) // replayed identical line
	p.append(ll)
	if len(p.buf) != 1 {
		t.Fatalf("expected 1 line after dedup, got %d", len(p.buf))
	}

	// A different timestamp is a distinct line.
	ll2 := ll
	ll2.Timestamp = ts.Add(time.Second)
	p.append(ll2)
	if len(p.buf) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(p.buf))
	}

	// A different message at the same timestamp is distinct.
	ll3 := ll
	ll3.Message = "stopped"
	p.append(ll3)
	if len(p.buf) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(p.buf))
	}
}
