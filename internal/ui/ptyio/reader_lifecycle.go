package ptyio

import (
	"io"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/safego"
)

// StartReaderOptions bundles the per-consumer pieces of starting a PTY read
// loop on a shared State: how to get the terminal, how messages are built,
// and how they are forwarded into the UI.
type StartReaderOptions struct {
	// AcquireTerm is evaluated under the state lock. It returns the terminal
	// to read from, or nil when the tab/terminal is not in a readable state.
	// It must return an untyped nil for "not readable" (not a nil pointer
	// wrapped in the interface).
	AcquireTerm func() io.Reader
	Config      PTYReaderConfig
	Factory     PTYMsgFactory
	// ReaderLabel and ForwardLabel name the spawned goroutines.
	ReaderLabel  string
	ForwardLabel string
	// Forward drains the reader's message channel into the UI sink.
	Forward func(<-chan tea.Msg)
}

// StartReader starts the PTY read loop for st unless one is already running.
// mu is the embedding struct's mutex (the one documented to guard st). The
// reader marks st stopped (under mu) when it exits.
func (st *State) StartReader(mu sync.Locker, opts StartReaderOptions) {
	mu.Lock()
	if st.ReaderActive {
		if st.MsgCh == nil || st.ReaderCancel == nil {
			// Inconsistent leftover state: treat as not running and rearm.
			st.ReaderActive = false
		} else {
			mu.Unlock()
			return
		}
	}
	term := opts.AcquireTerm()
	if term == nil {
		st.ReaderActive = false
		mu.Unlock()
		return
	}
	st.ReaderActive = true
	st.RestartBackoff = 0
	atomic.StoreInt64(&st.Heartbeat, time.Now().UnixNano())

	if st.ReaderCancel != nil {
		close(st.ReaderCancel)
	}
	st.ReaderCancel = make(chan struct{})
	st.MsgCh = make(chan tea.Msg, opts.Config.ReadQueueSize)
	st.ReaderGen++
	gen := st.ReaderGen

	cancel := st.ReaderCancel
	msgCh := st.MsgCh
	mu.Unlock()

	safego.Go(opts.ReaderLabel, func() {
		defer st.MarkReaderStopped(mu, gen)
		RunPTYReader(term, msgCh, cancel, &st.Heartbeat, opts.Config, opts.Factory)
	})
	safego.Go(opts.ForwardLabel, func() {
		opts.Forward(msgCh)
	})
}

// StopReader signals the read loop to stop and clears reader bookkeeping.
// mu must not be held by the caller.
func (st *State) StopReader(mu sync.Locker) {
	mu.Lock()
	if st.ReaderCancel != nil {
		close(st.ReaderCancel)
		st.ReaderCancel = nil
	}
	st.ReaderActive = false
	st.MsgCh = nil
	mu.Unlock()
	atomic.StoreInt64(&st.Heartbeat, 0)
}

// MarkReaderStopped clears reader bookkeeping after the read loop has exited
// on its own (RunPTYReader returned), but only when gen is still the current
// reader generation. A stale reader that exits after a restart must not
// clobber the replacement reader's state. The cancel channel is intentionally
// left in place for the next StartReader to close.
func (st *State) MarkReaderStopped(mu sync.Locker, gen uint64) {
	mu.Lock()
	if st.ReaderGen != gen {
		mu.Unlock()
		return
	}
	st.ReaderActive = false
	st.MsgCh = nil
	mu.Unlock()
	atomic.StoreInt64(&st.Heartbeat, 0)
}

// ReaderStalled reports whether an active reader's heartbeat is older than
// stallTimeout. mu must not be held by the caller.
func (st *State) ReaderStalled(mu sync.Locker, stallTimeout time.Duration) bool {
	mu.Lock()
	active := st.ReaderActive
	mu.Unlock()
	if !active {
		return false
	}
	lastBeat := atomic.LoadInt64(&st.Heartbeat)
	return lastBeat > 0 && time.Since(time.Unix(0, lastBeat)) > stallTimeout
}

// Reader-restart backoff policy: restarts are counted within a rolling
// window; each retry doubles the delay from RestartBackoffInitial up to
// RestartBackoffCap, and once the per-window restart budget is exhausted the
// caller should stop restarting and mark the terminal detached.
const (
	// RestartBackoffInitial is the first reader-restart delay.
	RestartBackoffInitial = 200 * time.Millisecond
	// RestartBackoffCap bounds the exponential reader-restart delay.
	RestartBackoffCap = 5 * time.Second
)

// NextRestartBackoffLocked advances the restart-backoff state and returns the
// delay before the next restart attempt. ok is false once more than
// maxRestarts attempts happened inside the rolling window — the caller should
// give up instead of restarting. The caller must hold the state lock.
func (st *State) NextRestartBackoffLocked(window time.Duration, maxRestarts int) (backoff time.Duration, ok bool) {
	now := time.Now()
	if st.RestartSince.IsZero() || now.Sub(st.RestartSince) > window {
		st.RestartSince = now
		st.RestartCount = 0
	}
	st.RestartCount++
	if st.RestartCount > maxRestarts {
		st.RestartBackoff = 0
		return 0, false
	}
	backoff = st.RestartBackoff
	if backoff <= 0 {
		backoff = RestartBackoffInitial
	} else {
		backoff *= 2
		if backoff > RestartBackoffCap {
			backoff = RestartBackoffCap
		}
	}
	st.RestartBackoff = backoff
	return backoff, true
}

// ResetRestartBackoffLocked clears restart-backoff state, e.g. when the
// terminal is detached and restarts no longer apply. The caller must hold the
// state lock.
func (st *State) ResetRestartBackoffLocked() {
	st.RestartBackoff = 0
	st.RestartCount = 0
	st.RestartSince = time.Time{}
}
