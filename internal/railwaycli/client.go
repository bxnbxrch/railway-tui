// Package railwaycli is a thin wrapper around the `railway` CLI. One-shot
// commands are run with --json and parsed into model.* types; long-lived
// streaming commands (logs) live in stream.go.
package railwaycli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"railway-tui/internal/dbg"
	"railway-tui/internal/model"
)

// Client runs railway CLI commands. Bin defaults to "railway" on PATH.
type Client struct {
	Bin     string
	Timeout time.Duration
}

// New returns a Client with sensible defaults.
func New() *Client {
	return &Client{Bin: "railway", Timeout: 25 * time.Second}
}

func (c *Client) bin() string {
	if c.Bin == "" {
		return "railway"
	}
	return c.Bin
}

// run executes a one-shot railway command and returns stdout. stderr is folded
// into the error so callers get actionable messages (not logged in, not
// linked, etc.).
func (c *Client) run(ctx context.Context, args ...string) ([]byte, error) {
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	start := time.Now()
	cmd := exec.CommandContext(ctx, c.bin(), args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	dur := time.Since(start).Round(time.Millisecond)
	if err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		dbg.Logf("cli FAIL (%s) railway %s -> %s", dur, strings.Join(args, " "), msg)
		return nil, &CLIError{Args: args, Msg: msg, Err: err}
	}
	dbg.Logf("cli ok   (%s) railway %s -> %d bytes", dur, strings.Join(args, " "), out.Len())
	return out.Bytes(), nil
}

// CLIError carries the failing command and railway's stderr message.
type CLIError struct {
	Args []string
	Msg  string
	Err  error
}

func (e *CLIError) Error() string {
	return fmt.Sprintf("railway %s: %s", strings.Join(e.Args, " "), e.Msg)
}

func (e *CLIError) Unwrap() error { return e.Err }

// WhoAmI returns the logged-in user, or an error if not authenticated.
func (c *Client) WhoAmI(ctx context.Context) (string, error) {
	out, err := c.run(ctx, "whoami")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Services lists services in the linked (or given) environment.
func (c *Client) Services(ctx context.Context, project, env string) ([]model.Service, error) {
	args := []string{"service", "list", "--json"}
	args = appendScope(args, project, env, "")
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var raw []rawService
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse service list: %w", err)
	}
	svcs := make([]model.Service, 0, len(raw))
	for _, r := range raw {
		svcs = append(svcs, r.toModel())
	}
	return svcs, nil
}

// Deployments lists deployments for a service.
func (c *Client) Deployments(ctx context.Context, project, env, service string) ([]model.Deployment, error) {
	args := []string{"deployment", "list", "--json"}
	args = appendScope(args, project, env, service)
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var raw []rawDeployment
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse deployment list: %w", err)
	}
	ds := make([]model.Deployment, 0, len(raw))
	for _, r := range raw {
		ds = append(ds, r.toModel())
	}
	return ds, nil
}

// Projects lists all projects in the account with their environments, for the
// project/environment switcher.
func (c *Client) Projects(ctx context.Context) ([]model.ProjectRef, error) {
	out, err := c.run(ctx, "list", "--json")
	if err != nil {
		return nil, err
	}
	var raw []rawProjectRef
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse project list: %w", err)
	}
	refs := make([]model.ProjectRef, 0, len(raw))
	for _, r := range raw {
		ref := model.ProjectRef{ID: r.ID, Name: r.Name, Workspace: r.Workspace.Name}
		for _, e := range r.Environments.Edges {
			ref.Envs = append(ref.Envs, model.EnvRef{ID: e.Node.ID, Name: e.Node.Name})
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

// Project returns the full topology of the linked (or given) project.
func (c *Client) Project(ctx context.Context, project, env string) (*model.Project, error) {
	args := []string{"status", "--json"}
	// status requires --environment when --project is explicit.
	if project != "" && env == "" {
		env = "production"
	}
	args = appendScope(args, project, env, "")
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var raw rawStatus
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse status: %w", err)
	}
	return raw.toModel(), nil
}

// Metrics fetches raw time-series for a service. kinds is any of
// cpu/memory/network/volume; empty means all.
func (c *Client) Metrics(ctx context.Context, project, env, service string, kinds ...string) (*model.Metrics, error) {
	args := []string{"metrics", "--raw", "--json"}
	args = appendScope(args, project, env, service)
	for _, k := range kinds {
		args = append(args, "--"+k)
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var raw rawMetrics
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse metrics: %w", err)
	}
	return raw.toModel(service), nil
}

// LogTail fetches the last n lines for a source without streaming (the
// --lines flag disables streaming), used to seed the pane with recent history
// before the live stream attaches.
func (c *Client) LogTail(ctx context.Context, src model.Source, project string, n int) ([]model.LogLine, error) {
	args := []string{"logs", "--json", "--lines", itoaSimple(n)}
	switch src.Kind {
	case model.LogBuild:
		args = append(args, "--build")
	case model.LogDeploy:
		args = append(args, "--deployment")
	case model.LogHTTP:
		args = append(args, "--http")
	case model.LogNetwork:
		args = append(args, "--network")
	}
	args = appendScope(args, project, src.Environment, src.ServiceName)
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var lines []model.LogLine
	for _, raw := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		lines = append(lines, decodeLogLine(raw, src))
	}
	return lines, nil
}

func itoaSimple(n int) string { return fmt.Sprintf("%d", n) }

// Redeploy redeploys the latest deployment of a service.
func (c *Client) Redeploy(ctx context.Context, project, env, service string) error {
	args := []string{"redeploy", "--yes"}
	args = appendScope(args, project, env, service)
	_, err := c.run(ctx, args...)
	return err
}

// Restart restarts the latest deployment (no rebuild).
func (c *Client) Restart(ctx context.Context, project, env, service string) error {
	args := []string{"restart", "--yes"}
	args = appendScope(args, project, env, service)
	_, err := c.run(ctx, args...)
	return err
}

// appendScope adds --project/--environment/--service flags when set.
func appendScope(args []string, project, env, service string) []string {
	if project != "" {
		args = append(args, "--project", project)
	}
	if env != "" {
		args = append(args, "--environment", env)
	}
	if service != "" {
		args = append(args, "--service", service)
	}
	return args
}
