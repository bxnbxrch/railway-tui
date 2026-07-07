// Command railway-tui is a terminal dashboard wrapping the Railway CLI:
// docker-compose-style merged logs, metrics graphs, deploy status, topology,
// and background notifications — with easy project/environment switching.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"railway-tui/internal/config"
	"railway-tui/internal/dbg"
	"railway-tui/internal/railwaycli"
	"railway-tui/internal/ui"
)

const version = "0.1.0"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		flProject = flag.String("project", "", "project id/name to open (overrides config)")
		flEnv     = flag.String("env", "", "environment to open (overrides config)")
		flVersion = flag.Bool("version", false, "print version and exit")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "railway-tui %s — a terminal dashboard for the Railway CLI\n\n", version)
		fmt.Fprintf(os.Stderr, "Usage: railway-tui [flags]\n\nFlags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nKeys: [p]roject [e]nv [L]ayout · 1-6 panes · [tab] focus · / filter · [q]uit\n")
	}
	flag.Parse()
	if *flVersion {
		fmt.Println("railway-tui", version)
		return nil
	}

	// Ensure the railway CLI exists before we bring up the TUI.
	if _, err := exec.LookPath("railway"); err != nil {
		return fmt.Errorf("`railway` CLI not found on PATH — install it first (https://docs.railway.com/guides/cli)")
	}

	logPath, logErr := dbg.Init()
	if logErr != nil {
		fmt.Fprintln(os.Stderr, "warning: could not open debug log:", logErr)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not read config:", err)
		cfg = config.Default()
	}
	if *flProject != "" {
		cfg.Project = *flProject
	}
	if *flEnv != "" {
		cfg.Environment = *flEnv
	}
	cwd, _ := os.Getwd()
	dbg.Logf("startup: cwd=%q cfg.project=%q cfg.env=%q", cwd, cfg.Project, cfg.Environment)

	client := railwaycli.New()

	// Preflight auth check so we fail fast with a clear message.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := client.WhoAmI(ctx); err != nil {
		return fmt.Errorf("not authenticated with Railway — run `railway login` first\n(%v)", err)
	}

	app := ui.New(cfg, client)
	app.SetLogPath(logPath)
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}
