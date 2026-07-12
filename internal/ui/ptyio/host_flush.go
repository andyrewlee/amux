package ptyio

import "time"

// FlushGate applies the shared flush quiet-period policy at the top of a flush
// tick. When output is still arriving it marks a flush scheduled and returns
// (delay, true) so the caller re-arms after delay; otherwise it clears the
// scheduled flags and returns (0, false) so the caller proceeds to flush the
// buffer. FlushScheduled/FlushPendingSince follow the same single-writer
// (Update goroutine) convention the two panes used before this was shared.
func (st *State) FlushGate(now time.Time, quiet, maxInterval time.Duration) (deferDelay time.Duration, deferred bool) {
	if delay, d := st.FlushDelay(now, quiet, maxInterval); d {
		st.FlushScheduled = true
		return delay, true
	}
	st.FlushScheduled = false
	st.FlushPendingSince = time.Time{}
	return 0, false
}

// RearmFlush finishes a flush pass after a chunk was written: if PendingOutput
// drained it truncates the buffer, runs onDrained for pane-specific bookkeeping,
// and returns false; otherwise it marks a follow-up flush scheduled at now and
// returns true so the caller re-arms its own tick (the re-arm delay and message
// are pane-specific and stay with the caller). onDrained may be nil and, when
// set, is expected to take the pane mutex for the bookkeeping it clears — it is
// called with mu released, matching the prior per-pane code.
func (st *State) RearmFlush(now time.Time, onDrained func()) (rearm bool) {
	if len(st.PendingOutput) == 0 {
		st.PendingOutput = st.PendingOutput[:0]
		if onDrained != nil {
			onDrained()
		}
		return false
	}
	st.FlushScheduled = true
	st.FlushPendingSince = now
	return true
}
