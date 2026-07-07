package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"railway-tui/internal/config"
	"railway-tui/internal/model"
	"railway-tui/internal/ui/theme"
)

// severity ranks a notification for coloring and filtering.
type severity int

const (
	sevInfo severity = iota
	sevGood
	sevWarn
	sevBad
)

// notification is a single event surfaced to the user.
type notification struct {
	ts      time.Time
	sev     severity
	title   string
	body    string
	service string
}

// watcher applies rules to deploy-status changes and log lines, producing
// notifications. It is stateless w.r.t. UI; the app owns the toast queue and
// history. Deploy transitions are detected by diffing successive snapshots.
type watcher struct {
	cfg      config.Notifications
	lastSeen map[string]model.DeployStatus // service name -> last status
}

func newWatcher(cfg config.Notifications) *watcher {
	return &watcher{cfg: cfg, lastSeen: map[string]model.DeployStatus{}}
}

func (w *watcher) setConfig(cfg config.Notifications) { w.cfg = cfg }

// onServices diffs the latest service statuses against the last snapshot and
// returns notifications for meaningful transitions.
func (w *watcher) onServices(svcs []model.Service) []notification {
	var out []notification
	for _, s := range svcs {
		prev, had := w.lastSeen[s.Name]
		w.lastSeen[s.Name] = s.Status
		if !had || prev == s.Status || s.Status == "" {
			continue
		}
		if w.cfg.Muted(s.Name) {
			continue
		}
		switch {
		case s.Status == model.StatusCrashed && w.cfg.OnCrash:
			out = append(out, notification{
				ts: time.Now(), sev: sevBad, service: s.Name,
				title: "CRASHED", body: s.Name + " crashed",
			})
		case s.Status == model.StatusFailed && w.cfg.OnDeployFail:
			out = append(out, notification{
				ts: time.Now(), sev: sevBad, service: s.Name,
				title: "DEPLOY FAILED", body: s.Name + " deployment failed",
			})
		case s.Status == model.StatusSuccess && prev.Active() && w.cfg.OnDeploySuccess:
			out = append(out, notification{
				ts: time.Now(), sev: sevGood, service: s.Name,
				title: "DEPLOYED", body: s.Name + " is live",
			})
		case s.Status.Active() && !prev.Active():
			out = append(out, notification{
				ts: time.Now(), sev: sevInfo, service: s.Name,
				title: "BUILDING", body: s.Name + " " + strings.ToLower(string(s.Status)),
			})
		}
	}
	return out
}

// isErrorLevel reports whether a log level string denotes an error.
func isErrorLevel(level string) bool {
	switch strings.ToLower(level) {
	case "error", "fatal", "critical", "err", "panic":
		return true
	}
	return false
}

// isError reports whether a log line should be treated as an error, based on
// its level or the configured patterns. This is independent of the
// notification toggles/mutes, so the Errors pane can collect everything.
func (w *watcher) isError(ll model.LogLine) bool {
	if isErrorLevel(ll.Level) {
		return true
	}
	lm := strings.ToLower(ll.Message)
	for _, pat := range w.cfg.ErrorPatterns {
		if pat != "" && strings.Contains(lm, strings.ToLower(pat)) {
			return true
		}
	}
	return false
}

// onLogLine applies error rules to a log line for notification purposes.
func (w *watcher) onLogLine(ll model.LogLine) *notification {
	if !w.cfg.OnLogError {
		return nil
	}
	if w.cfg.Muted(ll.Source.ServiceName) {
		return nil
	}
	if !w.isError(ll) {
		return nil
	}
	body := ll.Message
	if len(body) > 80 {
		body = body[:80] + "…"
	}
	return &notification{
		ts: time.Now(), sev: sevWarn, service: ll.Source.ServiceName,
		title: "LOG ERROR · " + ll.Source.ServiceName, body: body,
	}
}

// --- toast overlay + history ---

// toast is a notification currently displayed, with an expiry.
type toast struct {
	note    notification
	expires time.Time
}

// toastExpiredMsg triggers a re-render to drop expired toasts.
type toastExpiredMsg struct{}

// notifyCenter owns the live toast queue and the full history.
type notifyCenter struct {
	styles  *theme.Styles
	dur     time.Duration
	toasts  []toast
	history []notification
	cursor  int
}

func newNotifyCenter(styles *theme.Styles, dur time.Duration) *notifyCenter {
	return &notifyCenter{styles: styles, dur: dur}
}

func (n *notifyCenter) setDur(d time.Duration) { n.dur = d }

const maxLiveToasts = 20

// push adds a notification to history and shows a toast; returns a cmd that
// fires when the toast should expire. This is defense-in-depth against any
// future burst of distinct notifications — only the newest maxLiveToasts stay
// live (overlay() only ever renders the last 4 anyway), so a flood can't grow
// the live queue unboundedly or spam unlimited pending expiry timers.
func (n *notifyCenter) push(note notification) tea.Cmd {
	n.history = append(n.history, note)
	if len(n.history) > 500 {
		n.history = n.history[len(n.history)-500:]
	}
	n.toasts = append(n.toasts, toast{note: note, expires: time.Now().Add(n.dur)})
	if len(n.toasts) > maxLiveToasts {
		n.toasts = n.toasts[len(n.toasts)-maxLiveToasts:]
	}
	d := n.dur
	return tea.Tick(d, func(time.Time) tea.Msg { return toastExpiredMsg{} })
}

func (n *notifyCenter) sweep() {
	now := time.Now()
	kept := n.toasts[:0]
	for _, t := range n.toasts {
		if t.expires.After(now) {
			kept = append(kept, t)
		}
	}
	n.toasts = kept
}

func (n *notifyCenter) unread() int { return len(n.toasts) }

// toastStyle picks a border style by severity.
func (n *notifyCenter) toastStyle(sev severity) lipgloss.Style {
	switch sev {
	case sevBad:
		return n.styles.ToastBad
	case sevWarn:
		return n.styles.ToastWarn
	case sevGood:
		return n.styles.ToastGood
	default:
		return n.styles.Toast
	}
}

// overlay renders the stacked toasts as a right-aligned block (caller composes
// it onto the frame).
func (n *notifyCenter) overlay(width int) string {
	n.sweep()
	if len(n.toasts) == 0 {
		return ""
	}
	var blocks []string
	// newest at bottom; show up to 4
	start := 0
	if len(n.toasts) > 4 {
		start = len(n.toasts) - 4
	}
	for _, t := range n.toasts[start:] {
		icon := sevIcon(t.note.sev)
		title := lipgloss.NewStyle().Bold(true).Render(icon + " " + t.note.title)
		body := t.note.body
		card := n.toastStyle(t.note.sev).Width(38).Render(title + "\n" + body)
		blocks = append(blocks, card)
	}
	return lipgloss.JoinVertical(lipgloss.Right, blocks...)
}

func sevIcon(s severity) string {
	switch s {
	case sevBad:
		return "✖"
	case sevWarn:
		return "▲"
	case sevGood:
		return "✔"
	default:
		return "●"
	}
}

// historyView renders the Notifications tab.
func (n *notifyCenter) historyView(width, height int) string {
	var b strings.Builder
	b.WriteString(n.styles.Title.Render("Notifications"))
	b.WriteString(n.styles.Dim.Render("   (newest last)"))
	b.WriteString("\n\n")
	if len(n.history) == 0 {
		b.WriteString(n.styles.Dim.Render("No events yet. Deploy status changes and log errors show up here."))
		return b.String()
	}
	start := 0
	if len(n.history) > height-4 {
		start = len(n.history) - (height - 4)
	}
	for _, note := range n.history[start:] {
		ts := n.styles.Dim.Render(note.ts.Local().Format("15:04:05"))
		col := n.styles.T.Fg
		switch note.sev {
		case sevBad:
			col = n.styles.T.Bad
		case sevWarn:
			col = n.styles.T.Warn
		case sevGood:
			col = n.styles.T.Good
		}
		icon := lipgloss.NewStyle().Foreground(col).Render(sevIcon(note.sev))
		title := lipgloss.NewStyle().Foreground(col).Bold(true).Render(note.title)
		b.WriteString(ts + " " + icon + " " + title + "  " + note.body + "\n")
	}
	return b.String()
}
