package ui

import "testing"

func TestSplitKV(t *testing.T) {
	cases := []struct {
		in       string
		key, val string
		ok       bool
	}{
		{"API_URL=https://x.com", "API_URL", "https://x.com", true},
		{"TOKEN=a=b=c", "TOKEN", "a=b=c", true}, // only first '=' splits
		{"KEY=", "KEY", "", true},               // empty value allowed
		{"=value", "", "", false},               // empty key rejected
		{"noequals", "", "", false},
	}
	for _, c := range cases {
		k, v, ok := splitKV(c.in)
		if ok != c.ok || (ok && (k != c.key || v != c.val)) {
			t.Errorf("splitKV(%q) = (%q,%q,%v), want (%q,%q,%v)", c.in, k, v, ok, c.key, c.val, c.ok)
		}
	}
}

func TestMaskValue(t *testing.T) {
	if got := maskValue(""); got != "" {
		t.Errorf("empty value should mask to empty, got %q", got)
	}
	if got := maskValue("abc"); len([]rune(got)) != 3 {
		t.Errorf("short value: want 3 bullets, got %q", got)
	}
	// Long values are capped so the exact length doesn't leak.
	if got := maskValue("a-very-long-secret-token-value"); len([]rune(got)) != 8 {
		t.Errorf("long value should cap at 8 bullets, got %d", len([]rune(got)))
	}
}

func TestHumanGB(t *testing.T) {
	cases := []struct {
		gb   float64
		want string
	}{
		{0, "0 MB"},
		{0.0246, "25.2 MB"},
		{2.5, "2.50 GB"},
	}
	for _, c := range cases {
		if got := humanGB(c.gb); got != c.want {
			t.Errorf("humanGB(%v) = %q, want %q", c.gb, got, c.want)
		}
	}
}

func TestActionLabel(t *testing.T) {
	if actionLabel("down") != "REMOVE deployment" {
		t.Errorf("down label wrong: %q", actionLabel("down"))
	}
	if actionLabel("from-source") != "REDEPLOY (from source)" {
		t.Errorf("from-source label wrong: %q", actionLabel("from-source"))
	}
	if actionLabel("restart") != "RESTART" {
		t.Errorf("default label wrong: %q", actionLabel("restart"))
	}
}
