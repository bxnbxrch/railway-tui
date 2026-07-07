# railway-tui

A terminal dashboard that wraps the [Railway](https://railway.com) CLI. See
logs from multiple services at once (docker-compose style), get a dedicated
red feed of detected errors, track deploy status, browse project topology, and
get in-TUI toast notifications when things deploy, crash, or log errors — with
fast project/environment switching.

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea). It shells
out to the `railway` CLI (using `--json` everywhere it can) and streams
`railway logs` subprocesses, so it stays in sync with your CLI auth and never
needs a separate API token.

## Requirements

- Go 1.23+ (to build)
- The `railway` CLI installed and logged in (`railway login`)

## Build & run

```sh
go build -o railway-tui ./cmd/railway-tui
./railway-tui                      # opens linked/configured project
./railway-tui --project unity --env dev
./railway-tui --version
```

## Panes

| Pane | What it shows |
|------|---------------|
| **Logs** | Merged, per-service-colored log stream (compose style). Toggle sources (deploy/build/http per service) from the sidebar; live filter with text, `@level:error`, or `[service]`. Error lines are marked with a red ✖ and tinted red inline. |
| **Errors** | A dedicated red-accented feed of every error detected in the log stream (error/fatal/panic levels or configured patterns), each entry showing service, time, level, and the wrapped message. |
| **Deploys** | Per-service status, replicas (with crash count), age, latest commit. Redeploy/restart with confirmation. |
| **Topology** | Project → environment → service tree with status, replicas, source, and domain. Enter focuses the logs stream on a service. |
| **Notifications** | History of deploy/crash/log-error events. Toasts pop over any pane. |
| **Settings** | Toggle notification rules, poll intervals, and toast duration in-app (persisted to YAML). |

## Keys

| Key | Action |
|-----|--------|
| `1`–`6` | Jump to a pane (Logs/Errors/Deploys/Topology/Notifs/Settings) |
| `L` | Cycle saved layouts (single & split-pane arrangements) |
| `tab` | Move focus between panes in a split layout |
| `p` / `e` | Switch **project** / **environment** (modal picker) |
| `/` | Filter logs (Logs pane) |
| `s` | Toggle source sidebar (Logs pane) |
| `a` | Toggle autoscroll/tail (Logs pane) |
| `R` / `x` | Redeploy / restart selected service (Deploys pane) |
| `enter` | Focus a service (Topology) / drill in |
| `q` | Quit |

## Configuration

Config lives at `~/.config/railway-tui/config.yaml` (respects
`$XDG_CONFIG_HOME`). It's created with sane defaults on first run and covers:

- Default `project` / `environment`
- `polling` intervals (deploy status)
- `notifications` rules (deploy success/fail, crash, log-error patterns, muted
  services, toast duration)
- `layouts` — named pane arrangements (primary + optional split, ratio,
  orientation, active log sources)
- `theme`

The Settings pane edits the common toggles; layouts and error patterns are
edited directly in the YAML (reloaded on next launch).

## Notes & limitations

- **Uptime / dependency graph:** Railway's CLI exposes project structure and
  per-service deploy status, but *not* inter-service call edges, so Topology
  shows the accurate structure without inferred dependency arrows. "Uptime" is
  approximated from deploy-status history and HTTP error rates rather than a
  true SLA figure.
- **Desktop notifications** are intentionally out of scope for v1 — alerts are
  in-TUI (toasts + Notifications tab) only.
- Log merging keeps a bounded, roughly time-ordered buffer (corrects minor
  cross-source reordering within a small window), matching how `docker compose
  logs` interleaves sources.

## Layout of the code

```
cmd/railway-tui/        entrypoint, flags, auth preflight
internal/model/         domain types (Service, Deployment, LogLine, Metrics…)
internal/railwaycli/    exec wrapper (one-shot --json) + log stream supervisor
internal/config/        YAML config load/save + defaults
internal/ui/            Bubble Tea app: panes, layouts, picker, watcher, toasts
internal/ui/theme/      palette + component styles
```

Run the tests (no network required — they parse captured JSON fixtures and
exercise the pure logic):

```sh
go test ./...
```
