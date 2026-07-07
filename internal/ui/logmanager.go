package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"railway-tui/internal/dbg"
	"railway-tui/internal/model"
	"railway-tui/internal/railwaycli"
)

// logManager supervises N streaming log subprocesses and funnels all their
// lines into a single aggregator channel. A single re-arming tea.Cmd
// (waitForLine) drains the aggregator, so both the logs pane and the watcher
// see every line via the root Update loop.
type logManager struct {
	client  *railwaycli.Client
	project string
	agg     chan model.LogLine
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
		streams: make(map[string]*streamHandle),
	}
}

// active returns the keys of currently-streaming sources.
func (m *logManager) active() map[string]model.Source {
	out := make(map[string]model.Source, len(m.streams))
	for k, h := range m.streams {
		out[k] = h.src
	}
	return out
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
	go m.pump(ctx, src)
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

// pump seeds recent history (a tail) then keeps a live stream running,
// silently reconnecting when `railway logs` ends (its stream has a finite
// server-side lifetime — the cause of logs "randomly stopping"). Reconnects
// are recorded only to the debug log, never as lines in the pane. Duplicate
// lines from tail/reconnect replays are de-duplicated downstream by the pane.
func (m *logManager) pump(ctx context.Context, src model.Source) {
	// Seed with recent history so the pane isn't empty before live logs arrive.
	tailCtx, cancelTail := context.WithTimeout(ctx, 20*time.Second)
	if tail, err := m.client.LogTail(tailCtx, src, m.project, 20); err != nil {
		dbg.Logf("logmgr TAIL ERR [%s]: %v", src.Key(), err)
	} else {
		dbg.Logf("logmgr TAIL [%s]: %d lines", src.Key(), len(tail))
		for _, ll := range tail {
			select {
			case m.agg <- ll:
			case <-ctx.Done():
				cancelTail()
				return
			}
		}
	}
	cancelTail()

	backoff := time.Second
	const maxBackoff = 20 * time.Second
	for ctx.Err() == nil {
		ls, err := m.client.StartLogStream(ctx, src, m.project)
		if err != nil {
			dbg.Logf("logmgr STREAM START ERR [%s]: %v (retrying in %s)", src.Key(), err, backoff)
		} else if m.drain(ctx, ls) {
			// A stream that delivered data reset the backoff, so a normal
			// server-side rotation reconnects promptly.
			backoff = time.Second
		}
		if ctx.Err() != nil {
			return
		}
		dbg.Logf("logmgr RECONNECT [%s] in %s", src.Key(), backoff)
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

// drain forwards a stream's lines to the aggregator until it ends or ctx is
// cancelled. Returns true if any line was received (a healthy stream).
func (m *logManager) drain(ctx context.Context, ls *railwaycli.LogStream) bool {
	got := false
	for {
		select {
		case ll, ok := <-ls.Lines:
			if !ok {
				return got
			}
			got = true
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

// logLineMsg carries one aggregated line into the Update loop.
type logLineMsg model.LogLine

// waitForLine returns a tea.Cmd that blocks on the aggregator and re-arms
// itself after each line (standard Bubble Tea subprocess-streaming pattern).
func (m *logManager) waitForLine() tea.Cmd {
	return func() tea.Msg {
		ll, ok := <-m.agg
		if !ok {
			return nil
		}
		return logLineMsg(ll)
	}
}
