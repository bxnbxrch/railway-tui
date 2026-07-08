package ui

import (
	"testing"
	"time"

	"railway-tui/internal/model"
	"railway-tui/internal/ui/theme"
)

// TestLogsPanePurgeSource verifies that disabling a source removes its
// buffered lines immediately and that new lines from a disabled source are
// rejected (guards against a line already in flight when the stream is
// cancelled from still showing up).
func TestLogsPanePurgeSource(t *testing.T) {
	p := newLogsPane(theme.NewStyles(theme.Default()))
	a := model.Source{ServiceName: "api", Environment: "dev", Kind: model.LogDeploy}
	b := model.Source{ServiceName: "worker", Environment: "dev", Kind: model.LogDeploy}
	p.activeKey[a.Key()] = true
	p.activeKey[b.Key()] = true

	base := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	p.append(model.LogLine{Source: a, Timestamp: base, Message: "a1"})
	p.append(model.LogLine{Source: b, Timestamp: base, Message: "b1"})
	p.append(model.LogLine{Source: a, Timestamp: base.Add(time.Second), Message: "a2"})
	if len(p.buf) != 3 {
		t.Fatalf("expected 3 buffered lines, got %d", len(p.buf))
	}

	// Disable source a: its lines should vanish immediately, b's should remain.
	p.activeKey[a.Key()] = false
	p.purgeSource(a.Key())
	if len(p.buf) != 1 || p.buf[0].Source.ServiceName != "worker" {
		t.Fatalf("expected only worker's line to remain, got %+v", p.buf)
	}

	// A line arriving late for the now-disabled source must be rejected.
	if ok := p.append(model.LogLine{Source: a, Timestamp: base.Add(2 * time.Second), Message: "late"}); ok {
		t.Fatal("append for disabled source should be rejected")
	}
	if len(p.buf) != 1 {
		t.Fatalf("buffer should still have 1 line, got %d", len(p.buf))
	}
}

// TestErrorsPanePurgeSource verifies disabling a source clears its errors.
func TestErrorsPanePurgeSource(t *testing.T) {
	p := newErrorsPane(theme.NewStyles(theme.Default()))
	p.setSize(80, 20)
	a := model.Source{ServiceName: "api", Kind: model.LogDeploy}
	b := model.Source{ServiceName: "worker", Kind: model.LogDeploy}
	p.append(model.LogLine{Source: a, Level: "error", Message: "boom-a"})
	p.append(model.LogLine{Source: b, Level: "error", Message: "boom-b"})
	if len(p.buf) != 2 {
		t.Fatalf("expected 2 errors, got %d", len(p.buf))
	}
	p.purgeSource(a.Key())
	if len(p.buf) != 1 || p.buf[0].Source.ServiceName != "worker" {
		t.Fatalf("expected only worker's error to remain, got %+v", p.buf)
	}
}
