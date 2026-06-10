package ptyio

import (
	"time"

	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/vterm"
)

// OverflowLogThrottle bounds how often a sustained PTY overflow logs.
const OverflowLogThrottle = 2 * time.Second

// NoteOverflowDropLocked accumulates dropped overflow bytes and reports
// whether a throttled overflow warning should be emitted now (the caller logs
// outside the lock). It returns the aggregated dropped-byte total to report
// when logNow is true. The caller must hold the state lock.
func (st *State) NoteOverflowDropLocked(droppedBytes int) (logNow bool, total int) {
	st.OverflowDroppedSinceLog += droppedBytes
	now := time.Now()
	if st.LastOverflowLogAt.IsZero() || now.Sub(st.LastOverflowLogAt) >= OverflowLogThrottle {
		total = st.OverflowDroppedSinceLog
		st.OverflowDroppedSinceLog = 0
		st.LastOverflowLogAt = now
		return true, total
	}
	return false, 0
}

// ConsumeOverflowCarryLocked resumes a previous overflow cut: when carry
// state is pending from the last trim, it advances data past the unsafe
// prefix and updates the carry. It returns the (possibly trimmed) data and
// whether a carry was consumed. The caller must hold the state lock.
func (st *State) ConsumeOverflowCarryLocked(data []byte) ([]byte, bool) {
	if st.OverflowTrimCarry == (vterm.ParserCarryState{}) {
		return data, false
	}
	data, st.OverflowTrimCarry = TrimPTYOverflowPrefix(data, 0, st.OverflowTrimCarry)
	return data, true
}

// TrimOverflow applies the overflow policy to a pending buffer that exceeded
// maxBuffered: it drops the overflow prefix at a parser-safe boundary
// (seeded with the terminal's current parser carry state) and returns a fresh
// copy of the retained bytes, the carry for the next chunk, and how many
// bytes were dropped from the front.
func TrimOverflow(pending []byte, maxBuffered int, seed vterm.ParserCarryState) (retained []byte, carry vterm.ParserCarryState, droppedFromFront int) {
	overflow := len(pending) - maxBuffered
	retainedSlice, carry := TrimPTYOverflowPrefix(pending, overflow, seed)
	droppedFromFront = len(pending) - len(retainedSlice)
	return append([]byte(nil), retainedSlice...), carry, droppedFromFront
}

// FlushDelay implements the flush quiet-period policy: a flush is deferred
// while output is still arriving (quietFor < quiet) unless it has already
// been pending longer than maxInterval. It returns the delay before the next
// flush attempt and whether the flush should be deferred.
func (st *State) FlushDelay(now time.Time, quiet, maxInterval time.Duration) (time.Duration, bool) {
	quietFor := now.Sub(st.LastOutputAt)
	pendingFor := time.Duration(0)
	if !st.FlushPendingSince.IsZero() {
		pendingFor = now.Sub(st.FlushPendingSince)
	}
	if quietFor < quiet && pendingFor < maxInterval {
		delay := quiet - quietFor
		if delay < time.Millisecond {
			delay = time.Millisecond
		}
		return delay, true
	}
	return 0, false
}

// TakeFlushChunkLocked removes up to maxChunk bytes from the front of
// PendingOutput and returns a copy (nil when nothing is buffered). A
// non-positive maxChunk takes the whole buffer. The caller must hold the
// state lock.
func (st *State) TakeFlushChunkLocked(maxChunk int) []byte {
	chunkSize := len(st.PendingOutput)
	if maxChunk > 0 && chunkSize > maxChunk {
		chunkSize = maxChunk
	}
	if chunkSize == 0 {
		return nil
	}
	chunk := append([]byte(nil), st.PendingOutput[:chunkSize]...)
	copy(st.PendingOutput, st.PendingOutput[chunkSize:])
	st.PendingOutput = st.PendingOutput[:len(st.PendingOutput)-chunkSize]
	return chunk
}

// WriteFilteredChunkLocked filters chunk for known PTY noise (carrying
// incomplete fragments in NoiseTrailing), writes the visible remainder via
// write, and emits the per-flush perf counters. It returns the filtered
// bytes. The caller must hold the state lock; write goes to the terminal
// guarded by that same lock.
func (st *State) WriteFilteredChunkLocked(write func([]byte), chunk []byte) []byte {
	filtered := FilterKnownPTYNoiseStream(chunk, &st.NoiseTrailing)
	perf.Count("pty_flush_bytes_processed", int64(len(chunk)))
	if d := len(chunk) - len(filtered); d > 0 {
		perf.Count("pty_flush_bytes_filtered", int64(d))
	}
	if len(filtered) > 0 {
		flushDone := perf.Time("pty_flush")
		write(filtered)
		flushDone()
		perf.Count("pty_flush_bytes", int64(len(filtered)))
	}
	return filtered
}
