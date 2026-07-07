package ui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"railway-tui/internal/dbg"
	"railway-tui/internal/model"
	"railway-tui/internal/railwaycli"
)

// streamState is the lifecycle of one log-stream source, surfaced in the UI
// (sidebar icons + status bar) so streaming health is never invisible.
type streamState int

const (
	streamOff streamState = iota
	streamConnecting
	streamLive
	streamReconnecting
	streamFailed
	streamEnded // one-shot source (build logs) finished
)

// streamEvent reports a state transition for one source.
type streamEvent struct {
	key     string
	kind    model.LogKind
	service string
	state   streamState
	info    string // short human-readable reason (stderr / error)
	lines   int    // lines delivered so far (tail + stream), for "ended" feedback
}

// streamEventMsg carries a streamEvent into the Update loop.
type streamEventMsg streamEvent

// logBatchMsg carries one or more aggregated log lines into the Update loop.
// Batching (instead of one message per line) keeps the UI responsive under
// log floods: the pane re-renders once per batch, not once per line.
type logBatchMsg []model.LogLine

// logManager supervises N streaming log subprocesses and funnels all their
// lines into a single aggregator channel, and their health transitions into an
// events channel. Re-arming tea.Cmds (waitForLines / waitForEvents) drain
// both, so the panes and the watcher see everything via the root Update loop.
type logManager struct {
	client  *railwaycli.Client
	project string
	agg     chan model.LogLine
	events  chan streamEvent
	streams map[string]*streamHandle
}

type streamHandle struct {
	src    model.Source
	cancel context.CancelFunc
}

func newLogManager(client *railwaycli.Client, project string) *logManager {
	return &logManager{
		client:  client,
		project: project,
		agg:     make(chan model.LogLine, 1024),
		events:  make(chan streamEvent, 64),
		streams: map[string]*streamHandle{},
	}
}

func (m *logManager) isActive(key string) bool {
	_, ok := m.streams[key]
	return ok
}

// add starts a stream for src if not already running.
func (m *logManager) add(src model.Source) {
	key := src.Key()
	if _, ok := m.streams[key]; ok {
		return
	}
	dbg.Logf("logmgr ADD source [%s] (project=%q)", key, m.project)
	ctx, cancel := context.WithCancel(context.Background())
	m.streams[key] = &streamHandle{src: src, cancel: cancel}
	go m.pump(ctx, src, m.project)
}

// remove stops a stream.
func (m *logManager) remove(key string) {
	if h, ok := m.streams[key]; ok {
		dbg.Logf("logmgr REMOVE source [%s]", key)
		h.cancel()
		delete(m.streams, key)
	}
}

// toggle flips a source on/off.
func (m *logManager) toggle(src model.Source) {
	if m.isActive(src.Key()) {
		m.remove(src.Key())
	} else {
		m.add(src)
	}
}

// stopAll cancels every stream (used on context switch / quit).
func (m *logManager) stopAll() {
	for k, h := range m.streams {
		h.cancel()
		delete(m.streams, k)
	}
}

// emit publishes a state transition (dropped only if the app is shutting down).
func (m *logManager) emit(ctx context.Context, src model.Source, st streamState, info string, lines int) {
	ev := streamEvent{
		key: src.Key(), kind: src.Kind, service: src.ServiceName,
		state: st, info: shortInfo(info), lines: lines,
	}
	select {
	case m.events <- ev:
	case <-ctx.Done():
	}
}

// shortInfo compresses a stderr/error blob into one short line for the UI.
func shortInfo(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if len(s) > 70 {
		s = s[:70] + "…"
	}
	return s
}

// tailLines is the history seeded before a live stream attaches. Build logs
// get a much deeper seed: they're a finite historical record (the interesting
// part *is* the history), whereas continuous streams only need enough context
// to not start on an empty pane.
const (
	tailLinesLive  = 20
	tailLinesBuild = 200
)

// pump seeds recent history (a tail) then keeps a live stream running,
// reconnecting when `railway logs` ends (its stream has a finite server-side
// lifetime — the cause of logs "randomly stopping"). Every transition is
// emitted as a streamEvent so the UI can show per-source health. Duplicate
// lines from tail/reconnect replays are de-duplicated downstream by the pane.
func (m *logManager) pump(ctx context.Context, src model.Source, project string) {
	key := src.Key()
	m.emit(ctx, src, streamConnecting, "", 0)

	// Seed with recent history so the pane isn't empty before live logs arrive.
	n := tailLinesLive
	if src.Kind == model.LogBuild {
		n = tailLinesBuild
	}
	seeded := 0
	tailCtx, cancelTail := context.WithTimeout(ctx, 20*time.Second)
	if tail, err := m.client.LogTail(tailCtx, src, project, n); err != nil {
		dbg.Logf("logmgr TAIL ERR [%s]: %v", key, err)
	} else {
		dbg.Logf("logmgr TAIL [%s]: %d lines", key, len(tail))
		for _, ll := range tail {
			select {
			case m.agg <- ll:
				seeded++
			case <-ctx.Done():
				cancelTail()
				return
			}
		}
	}
	cancelTail()

	// Build logs are a finite, historical record of a single build that has
	// already run — not a live stream. Reconnecting after it ends would just
	// replay the entire build output on a loop. Run it once and report "ended"
	// so the sidebar can show it as done (instead of a checkbox that lies).
	if !isContinuous(src.Kind) {
		ls, err := m.client.StartLogStream(ctx, src, project)
		if err != nil {
			dbg.Logf("logmgr STREAM START ERR [%s]: %v (one-shot, not retrying)", key, err)
			m.emit(ctx, src, streamFailed, err.Error(), seeded)
			return
		}
		got := m.drain(ctx, src, ls, seeded) // emits live on first line
		dbg.Logf("logmgr STREAM DONE [%s] (one-shot; not reconnecting)", key)
		m.emit(ctx, src, streamEnded, ls.Stderr(), seeded+got)
		return
	}

	// Continuous streams: `railway logs` ends when the server rotates the
	// stream, so we reconnect in a loop. The health we surface is deliberately
	// smoothed so a *healthy* rotation never strobes the sidebar:
	//   - "live" is emitted only when a line actually arrives (see drain), not
	//     when the process starts — a stream that dies instantly won't flash
	//     green first.
	//   - A reconnect that previously delivered data stays "live" through the
	//     brief gap (no "reconnecting" flicker); we only downgrade to
	//     "reconnecting" once a reconnect comes back empty, and escalate to a
	//     steady "failed" after several consecutive empty attempts rather than
	//     flashing forever.
	backoff := time.Second
	const maxBackoff = 20 * time.Second
	const failAfterEmpty = 3
	total := seeded
	emptyStreak := 0
	for ctx.Err() == nil {
		ls, err := m.client.StartLogStream(ctx, src, project)
		if err != nil {
			dbg.Logf("logmgr STREAM START ERR [%s]: %v (retrying in %s)", key, err, backoff)
			emptyStreak++
			m.escalate(ctx, src, emptyStreak, failAfterEmpty, err.Error(), total)
		} else {
			got := m.drain(ctx, src, ls, total) // emits live on first line
			total += got
			if ctx.Err() != nil {
				return
			}
			if got > 0 {
				// Healthy rotation: reconnect promptly and stay "live" through
				// the gap — do not emit any transition, so the icon holds green.
				emptyStreak = 0
				backoff = time.Second
			} else {
				emptyStreak++
				reason := ls.Stderr()
				if reason == "" {
					if e := ls.Err(); e != nil {
						reason = e.Error()
					}
				}
				m.escalate(ctx, src, emptyStreak, failAfterEmpty, reason, total)
			}
		}
		if ctx.Err() != nil {
			return
		}
		dbg.Logf("logmgr RECONNECT [%s] in %s (emptyStreak=%d)", key, backoff, emptyStreak)
		if !sleepCtx(ctx, backoff) {
			return
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// escalate emits the health transition for a reconnect that produced no data:
// "reconnecting" for the first few empty attempts (a transient blip), then a
// steady "failed" once the stream is clearly not coming back — instead of
// flapping between states indefinitely.
func (m *logManager) escalate(ctx context.Context, src model.Source, streak, limit int, reason string, total int) {
	if streak >= limit {
		m.emit(ctx, src, streamFailed, reason, total)
	} else {
		m.emit(ctx, src, streamReconnecting, reason, total)
	}
}

// isContinuous reports whether a log kind represents an ongoing live stream
// (worth reconnecting) as opposed to a finite historical record (build logs).
func isContinuous(k model.LogKind) bool {
	return k == model.LogDeploy || k == model.LogHTTP || k == model.LogNetwork
}

// drain forwards a stream's lines to the aggregator until it ends or ctx is
// cancelled. It emits "live" the moment the first line arrives (so a stream
// that starts but never delivers never shows green). total is the running
// line count before this stream, used to report an accurate count with the
// live transition. Returns the number of lines received from this stream.
func (m *logManager) drain(ctx context.Context, src model.Source, ls *railwaycli.LogStream, total int) int {
	got := 0
	for {
		select {
		case ll, ok := <-ls.Lines:
			if !ok {
				return got
			}
			if got == 0 {
				m.emit(ctx, src, streamLive, "", total)
			}
			got++
			select {
			case m.agg <- ll:
			case <-ctx.Done():
				return got
			}
		case <-ctx.Done():
			return got
		}
	}
}

// sleepCtx sleeps for d or until ctx is cancelled; returns false if cancelled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// maxLogBatch caps how many lines a single logBatchMsg carries, so one huge
// flood can't starve the Update loop.
const maxLogBatch = 256

// waitForLines returns a tea.Cmd that blocks for the next log line, then
// greedily drains whatever else is already queued (up to maxLogBatch) into a
// single batch message. It re-arms after each batch.
func (m *logManager) waitForLines() tea.Cmd {
	agg := m.agg
	return func() tea.Msg {
		ll, ok := <-agg
		if !ok {
			return nil
		}
		batch := logBatchMsg{ll}
		for len(batch) < maxLogBatch {
			select {
			case l2, ok2 := <-agg:
				if !ok2 {
					return batch
				}
				batch = append(batch, l2)
			default:
				return batch
			}
		}
		return batch
	}
}

// waitForEvents returns a tea.Cmd that blocks for the next stream health
// transition and re-arms after each one.
func (m *logManager) waitForEvents() tea.Cmd {
	events := m.events
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return nil
		}
		return streamEventMsg(ev)
	}
}
