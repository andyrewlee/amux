package ptyio

import (
	"sync"

	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/vterm"
)

// OutputHooks carries the pane-specific pieces of AppendOutput. Every hook is
// optional; a nil hook is skipped, and where noted its lock section is not
// entered. Hooks documented "under mu" run while AppendOutput holds the pane
// mutex; the rest run with mu released.
type OutputHooks struct {
	// OnCarryConsumed runs under mu when a pending overflow carry was consumed
	// from the front of the incoming data (center resets its activity ANSI
	// state here).
	OnCarryConsumed func()
	// AfterAppendLocked runs under mu right after data is appended to
	// PendingOutput, receiving the appended byte count. When nil, AppendOutput
	// does not take the lock for it (the sidebar has no per-append accounting).
	AfterAppendLocked func(appendedLen int)
	// SeedForTrim returns the parser carry seed for the overflow trim. It runs
	// with mu released and does its own locking; panes reset their terminal
	// parser state inside it. A nil hook seeds with the zero carry state.
	SeedForTrim func() vterm.ParserCarryState
	// OnOverflowLocked runs under mu during the overflow bookkeeping, after
	// OverflowTrimCarry is stored and before NoteOverflowDropLocked, for
	// pane-specific settle/noise-reset accounting. It receives the dropped
	// overflow byte count, the absolute offset of the first retained byte, and
	// the buffer length before this chunk was appended.
	OnOverflowLocked func(overflow, retainedStart, prevPendingLen int)
	// LogOverflow emits the throttled overflow warning (pane-specific wording)
	// with the aggregated dropped-byte total. It runs with mu released.
	LogOverflow func(droppedTotal int)
	// DropBytesCounter and DropCounter name the per-pane overflow perf counters
	// (e.g. "pty_output_drop_bytes"/"pty_output_drop"). Empty names are skipped.
	DropBytesCounter string
	DropCounter      string
}

// AppendResult reports what AppendOutput did so a pane can run its own
// post-append accounting (center's chat-activity slice tracking).
type AppendResult struct {
	// Data is the incoming data after any overflow carry was consumed — the
	// bytes actually appended to PendingOutput.
	Data []byte
	// PrevPendingLen is len(PendingOutput) before this chunk was appended.
	PrevPendingLen int
	// RetainedStart is the absolute offset (relative to the pre-trim buffer) of
	// the first retained byte after an overflow trim; 0 when Overflowed is false.
	RetainedStart int
	// Overflowed reports whether the buffer exceeded maxBuffered and was trimmed.
	Overflowed bool
}

// AppendOutput buffers a chunk of PTY output on st, applying the shared
// overflow-trim policy, and returns what it did for pane-specific follow-up.
// It consumes any pending overflow carry, appends to PendingOutput, and — when
// the buffer exceeds maxBuffered — drops a parser-safe prefix, stores the new
// carry, and emits the pane's drop counters and throttled warning.
//
// mu is the embedding struct's mutex. AppendOutput acquires and releases it on
// exactly the boundaries the two panes used before this became shared: a lock
// around the carry consume, an optional lock for AfterAppendLocked, the
// SeedForTrim hook doing its own locking, and a lock around the overflow
// bookkeeping. PendingOutput is written with mu released, so call AppendOutput
// only from the single-writer Update goroutine, as both panes do.
func (st *State) AppendOutput(mu sync.Locker, data []byte, maxBuffered int, h OutputHooks) AppendResult {
	mu.Lock()
	var carryConsumed bool
	data, carryConsumed = st.ConsumeOverflowCarryLocked(data)
	if carryConsumed && h.OnCarryConsumed != nil {
		h.OnCarryConsumed()
	}
	mu.Unlock()

	prevPendingLen := len(st.PendingOutput)
	st.PendingOutput = append(st.PendingOutput, data...)
	if h.AfterAppendLocked != nil {
		mu.Lock()
		h.AfterAppendLocked(len(data))
		mu.Unlock()
	}

	res := AppendResult{Data: data, PrevPendingLen: prevPendingLen}
	if len(st.PendingOutput) <= maxBuffered {
		return res
	}
	overflow := len(st.PendingOutput) - maxBuffered
	if h.DropBytesCounter != "" {
		perf.Count(h.DropBytesCounter, int64(overflow))
	}
	if h.DropCounter != "" {
		perf.Count(h.DropCounter, 1)
	}

	seed := vterm.ParserCarryState{}
	if h.SeedForTrim != nil {
		seed = h.SeedForTrim()
	}
	retained, overflowCarry, retainedStart := TrimOverflow(st.PendingOutput, maxBuffered, seed)
	st.PendingOutput = retained

	mu.Lock()
	st.OverflowTrimCarry = overflowCarry
	if h.OnOverflowLocked != nil {
		h.OnOverflowLocked(overflow, retainedStart, prevPendingLen)
	}
	overflowLogNow, overflowDroppedTotal := st.NoteOverflowDropLocked(retainedStart)
	mu.Unlock()
	if overflowLogNow && h.LogOverflow != nil {
		h.LogOverflow(overflowDroppedTotal)
	}

	res.RetainedStart = retainedStart
	res.Overflowed = true
	return res
}
