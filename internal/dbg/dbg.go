// Package dbg provides a lightweight file logger for diagnosing the TUI at
// runtime (the alt-screen makes stdout logging useless). It is a no-op until
// Init is called, so importing it is cheap.
package dbg

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

var (
	l    = log.New(io.Discard, "", 0)
	path string
)

// Init opens (or truncates) the log file and starts logging. The path honors
// $XDG_STATE_HOME, falling back to ~/.local/state. Safe to call once at start.
func Init() (string, error) {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	dir = filepath.Join(dir, "railway-tui")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, "debug.log")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", err
	}
	l = log.New(f, "", log.Ltime|log.Lmicroseconds)
	path = p
	l.Printf("=== railway-tui debug log started ===")
	return p, nil
}

// Path returns the active log file path ("" if not initialized).
func Path() string { return path }

// Logf writes a formatted line to the debug log.
func Logf(format string, args ...any) {
	l.Printf(format, args...)
}

// Log writes a line to the debug log.
func Log(args ...any) {
	l.Print(fmt.Sprint(args...))
}
