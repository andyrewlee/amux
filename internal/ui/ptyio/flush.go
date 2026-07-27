package ptyio

import (
	"bytes"
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

// DEC 2026 synchronized output brackets one complete frame: ESC[?2026h tells
// the terminal to hold the display steady, ESC[?2026l releases it. tmux wraps
// every client redraw this way (amux advertises the sync terminal feature —
// see internal/tmux/command.go), but it writes the client socket in ≤1KB
// chunks, so any redraw bigger than that spans several reads: measured on a
// streaming pane, 53% of reads end inside an open region.
//
// A flush that ends there hands the vterm half a frame. The vterm's freeze then
// holds the *previous* frame, so the pane shows stale content and reports every
// line dirty — a full repaint — until the next flush closes the region.
//
// So flushes end on frame boundaries: a flush stops at the last completed
// region and leaves a partial frame buffered (TakeFlushChunkLocked), and when
// nothing has completed there is nothing worth writing, so the flush waits
// instead (FlushDelay). On the same streaming trace that takes flushes ending
// mid-frame from 43% to ~1%, at an unchanged flush rate. Both waits are bounded
// — the flush ceilings cap them, and a partial frame that reaches the terminal
// anyway still has vterm.SyncStallTimeout behind it — so a writer that dies
// mid-frame cannot stall the pane.

// syncMarkerPrefix is the shared prefix of the DEC 2026 begin/end markers; the
// byte after it selects which one — 'h' opens a region, 'l' closes it, and
// anything else (notably '$' for a DECRQM query) is neither.
//
// Only this standalone form is recognized. The vterm's mode dispatch walks
// every parameter, so it would also honor 2026 bundled with other modes
// (ESC[?25;2026h); parsing parameter lists here would cost more than it is
// worth while no known agent emits that form. The consequence of missing one
// is bounded: that frame flushes half-drawn, exactly as it did before any of
// this existed.
var syncMarkerPrefix = []byte("\x1b[?2026")

const (
	// syncTailScanLimit bounds the marker scan so a large backlog does not pay
	// for the whole buffer. It has to comfortably exceed one frame — a marker
	// further back than this is invisible here, which for a sync-begin means the
	// frame is flushed half-drawn after all — so it is sized for a full-screen
	// truecolor repaint on a large pane rather than for the per-keystroke frames
	// that motivated this. The scan stays cheap at that size: see
	// BenchmarkScanSyncFrames, which is orders of magnitude under the cost of
	// writing the same bytes to the vterm.
	syncTailScanLimit = 64 * 1024
	// syncHoldRetry is how soon a flush held for a partial frame retries. The
	// frame body that closes the region also restarts the quiet period, so this
	// only needs to be short enough not to add visible latency itself.
	syncHoldRetry = 4 * time.Millisecond
)

// scanSyncFrames walks pending's DEC 2026 markers and reports how much of it
// may be written without leaving the terminal inside an unterminated region —
// all of it when the buffer does not end mid-frame, otherwise the prefix
// through the last completed region, and 0 when nothing has completed — plus
// whether a complete frame is in there at all.
//
// The scan runs forward rather than backward from the tail because bytes.Index
// is vectorized and bytes.LastIndex is not; on a busy tab this runs on the UI
// goroutine twice per flush, and the backward form costs ~100x more per byte.
//
// It reads pending in isolation, which costs it two cases the vterm's parser
// sees and it does not: a terminal already inside a region (only reachable once
// a chunk cap has split a frame too large to fall back from), and a marker
// split across the cut. Both report a buffer as ending outside a frame when it
// does not, and both cost at most the frozen frame that would have happened
// anyway — never a dropped or reordered byte. Carrying that state would mean
// threading the terminal through every flush call for cases that need a single
// frame to exceed the chunk cap.
func scanSyncFrames(pending []byte) (safeLen int, hasCompleteFrame bool) {
	window, offset := pending, 0
	if len(window) > syncTailScanLimit {
		offset = len(window) - syncTailScanLimit
		window = window[offset:]
	}
	lastEnd, open := -1, false
	for i := 0; i < len(window); {
		j := bytes.Index(window[i:], syncMarkerPrefix)
		if j < 0 {
			break
		}
		i += j + len(syncMarkerPrefix)
		if i >= len(window) {
			// A marker split by the buffer tail: the vterm parser carries the
			// fragment, so it decides nothing here.
			break
		}
		switch window[i] {
		case 'h':
			open = true
		case 'l':
			open = false
			lastEnd = i + 1
		}
		i++
	}
	hasCompleteFrame = lastEnd >= 0 && !open
	switch {
	case !open:
		return len(pending), hasCompleteFrame // the buffer does not end mid-frame
	case lastEnd < 0:
		return 0, false // nothing but a partial frame
	default:
		return offset + lastEnd, true
	}
}

// FlushDelay implements the flush quiet-period policy: a flush is deferred
// while output is still arriving (quietFor < quiet), or while the buffer holds
// only the opening half of a synchronized-output frame, unless it has already
// been pending longer than maxInterval. It returns the delay before the next
// flush attempt and whether the flush should be deferred.
func (st *State) FlushDelay(now time.Time, quiet, maxInterval time.Duration) (time.Duration, bool) {
	quietFor := now.Sub(st.LastOutputAt)
	pendingFor := time.Duration(0)
	if !st.FlushPendingSince.IsZero() {
		pendingFor = now.Sub(st.FlushPendingSince)
	}
	safeLen, hasCompleteFrame := scanSyncFrames(st.PendingOutput)
	// The quiet period is a guess at where a frame ends, for writers that give
	// no better signal. A writer that brackets its frames has already said, so
	// a finished frame goes out now instead of waiting out the debounce. This
	// cannot flush more often than output arrives: the reader's frame-interval
	// coalescing upstream is what actually caps the rate.
	//
	// Only at the base cadence, though. Every caller that wants a slower one —
	// alt-screen timing, backpressure, the inactive-tab multipliers — asks by
	// raising quiet above PtyFlushQuiet, and those exist to shed render work
	// (an inactive tab flushes up to 12x less often). Skipping the wait for a
	// finished frame would undo precisely the throttling they asked for, and
	// tmux brackets every redraw, so it would fire on nearly every flush.
	earlyFrameFlush := hasCompleteFrame && quiet <= PtyFlushQuiet
	if quietFor < quiet && pendingFor < maxInterval && !earlyFrameFlush {
		delay := quiet - quietFor
		if delay < time.Millisecond {
			delay = time.Millisecond
		}
		return delay, true
	}
	// Both ceilings bound the partial-frame hold independently: pendingFor caps
	// it while output keeps arriving, quietFor caps it when the writer stopped
	// mid-frame. Either one alone can stall — pendingFor is zero until a caller
	// sets FlushPendingSince, and quietFor stays small under continuous output.
	if pendingFor < maxInterval && quietFor < maxInterval && len(st.PendingOutput) > 0 && safeLen == 0 {
		return syncHoldRetry, true
	}
	return 0, false
}

// TakeFlushChunkLocked removes up to maxChunk bytes from the front of
// PendingOutput and returns a copy (nil when nothing is buffered). A
// non-positive maxChunk takes the whole buffer. The chunk also stops at the
// last completed synchronized-output frame, so complete frames are displayed
// now and a partial frame at the tail stays buffered instead of freezing the
// terminal mid-redraw. A zero safe length is deliberately not clamped: that is
// the case FlushDelay already held for as long as its ceilings allow, and
// taking nothing would spin the flush tick forever. The caller must hold the
// state lock.
func (st *State) TakeFlushChunkLocked(maxChunk int) []byte {
	chunkSize := len(st.PendingOutput)
	if maxChunk > 0 && chunkSize > maxChunk {
		chunkSize = maxChunk
	}
	// Scan the prefix actually being taken, not the whole buffer: what matters
	// is where this chunk ends. When maxChunk cuts a large backlog the buffer's
	// own tail is irrelevant, and looking at it would miss the frame boundary
	// sitting just before the cut.
	if safe, _ := scanSyncFrames(st.PendingOutput[:chunkSize]); safe > 0 && safe < chunkSize {
		chunkSize = safe
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
