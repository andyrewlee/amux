package ptyio

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/ui/compositor"
	"github.com/andyrewlee/amux/internal/vterm"
)

// State holds the PTY I/O bookkeeping shared by the center tab and sidebar
// terminal stacks: pending-output buffering, flush scheduling, overflow-trim
// carry, reader lifecycle, restart backoff, and the rendered-snapshot cache.
//
// It is embedded by both consumers. Locking stays with the embedding struct:
// unless a field is documented as atomic, it must be accessed under the
// embedder's mutex or from the single-writer Update goroutine, exactly as the
// fields were before they moved here.
type State struct {
	// PendingOutput buffers raw PTY output between flush ticks so partial
	// screen updates are not rendered.
	PendingOutput []byte
	// NoiseTrailing carries an incomplete known-noise line fragment (e.g. a
	// macOS malloc diagnostic) across chunk boundaries.
	NoiseTrailing []byte
	// OverflowTrimCarry is the parser carry state at the last overflow cut.
	OverflowTrimCarry vterm.ParserCarryState
	// FlushScheduled marks that a flush tick is already pending.
	FlushScheduled bool
	// LastOutputAt is when PTY data last arrived.
	LastOutputAt time.Time
	// FlushPendingSince is when the currently-scheduled flush was requested.
	FlushPendingSince time.Time
	// LastOverflowLogAt and OverflowDroppedSinceLog throttle the overflow-drop
	// warning so a sustained overflow logs at most once per throttle window
	// with the aggregated byte count.
	LastOverflowLogAt       time.Time
	OverflowDroppedSinceLog int

	// MsgCh is the reader goroutine's output channel; ReaderCancel signals it
	// to stop. ReaderActive guards against starting two readers.
	MsgCh        chan tea.Msg
	ReaderCancel chan struct{}
	ReaderActive bool
	// ReaderGen identifies the current reader goroutine. StartReader increments
	// it; a reader's exit cleanup only clears state when its generation is
	// still current, so a slow-exiting stale reader cannot clobber its
	// replacement's bookkeeping. Guarded by the embedder's mutex.
	ReaderGen uint64
	// Heartbeat is the last reader read time in nanoseconds. Atomic.
	Heartbeat int64

	// RestartBackoff/RestartCount/RestartSince implement exponential backoff
	// for reader restarts within a rolling window.
	RestartBackoff time.Duration
	RestartCount   int
	RestartSince   time.Time

	// CachedSnap/CachedVersion/CachedShowCursor cache the rendered terminal
	// snapshot so an unchanged terminal does not rebuild it every frame.
	CachedSnap       *compositor.VTermSnapshot
	CachedVersion    uint64
	CachedShowCursor bool
	SnapshotBuffer   compositor.SnapshotDoubleBuffer
}

// ResetSnapshotCache clears cached render snapshots and the reusable snapshot buffers.
func (s *State) ResetSnapshotCache() {
	if s == nil {
		return
	}
	s.CachedSnap = nil
	s.CachedVersion = 0
	s.CachedShowCursor = false
	s.SnapshotBuffer.Reset()
}
