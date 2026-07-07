package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"railway-tui/internal/config"
	"railway-tui/internal/model"
	"railway-tui/internal/ui/theme"
)

func TestErrorDetection(t *testing.T) {
	cases := []struct {
		level, msg string
		want       bool
	}{
		{"error", "boom", true},
		{"ERROR", "boom", true},
		{"fatal", "die", true},
		{"info", "all good", false},
		{"info", "hit a panic in handler", true}, // pattern match
		{"warn", "slow", false},
	}
	watch := newWatcher(config.Default().Notifications)
	for _, c := range cases {
		got := watch.isError(model.LogLine{Level: c.level, Message: c.msg})
		if got != c.want {
			t.Errorf("isError(%q,%q)=%v want %v", c.level, c.msg, got, c.want)
		}
	}
}

func TestErrorsPaneRendersRedAndClips(t *testing.T) {
	p := newErrorsPane(theme.NewStyles(theme.Default()))
	p.setSize(80, 20)
	p.append(model.LogLine{
		Source:  model.Source{ServiceName: "unity-backend", Kind: model.LogDeploy},
		Level:   "error",
		Message: strings.Repeat("connection refused to postgres ", 8),
	})
	view := p.View()
	// Every rendered row must fit the pane width (long messages are wrapped,
	// not overflowed). Color output is TTY-dependent so isn't asserted here.
	for i, line := range strings.Split(view, "\n") {
		if wdt := lipgloss.Width(line); wdt > 80 {
			t.Fatalf("errors pane line %d width %d exceeds 80", i, wdt)
		}
	}
	if !strings.Contains(view, "Errors") {
		t.Fatal("errors view should show the title")
	}
}
