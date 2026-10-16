package vterm

import (
	"sync"
	"time"
)

// SyncStallTimeout bounds how long a DEC 2026 sync-begin may freeze rendering
// without its matching sync-end. A well-behaved writer closes a sync region
// within one frame; if the end marker never arrives (writer died mid-frame,
// output trimmed under overflow), the frozen snapshot must not persist
// forever. Write, RenderBuffers, and Version check the deadline, so the
// terminal recovers on the next output, rendered frame, or snapshot-cache key
// check, whichever comes first.
const SyncStallTimeout = 2 * time.Second

// syncNow returns the current time for sync stall tracking; tests may stub it.
var syncNow = time.Now

// SyncActive reports whether synchronized output is currently active.
func (v *VTerm) SyncActive() bool {
	return v.syncActive
}

// maybeReleaseStaleSync force-ends a sync region whose end marker is overdue.
func (v *VTerm) maybeReleaseStaleSync() {
	if v.syncActive && syncNow().Sub(v.syncStartedAt) > SyncStallTimeout {
		v.setSynchronizedOutput(false)
	}
}

func (v *VTerm) setSynchronizedOutput(active bool) {
	if active {
		if v.syncActive {
			return
		}
		v.syncActive = true
		v.syncStartedAt = syncNow()
		v.syncFrame, _ = syncFramePool.Get().(*syncFrame)
		v.syncScreen = v.syncFrame.freeze(v.Screen)
		v.syncScrollbackLen = len(v.Scrollback)
		v.syncViewOffsetDelta = 0
		v.syncPreserveViewport = v.ViewOffset > 0
		// No cache invalidation: freezing does not change the displayed frame,
		// and renders during sync bypass the live-line cache without clearing
		// dirty state (see liveRenderCacheActive), so per-line dirty tracking
		// stays accurate across the sync window.
		return
	}

	if !v.syncActive {
		return
	}
	v.syncActive = false
	v.syncScreen = nil
	if v.syncFrame != nil {
		// The frame stays reusable even when a mid-window resize rebuilt
		// syncScreen: syncFrame.rows is the pooled backing, and a resize
		// only shuffles which row slices the replacement outer references —
		// never writes through them (the frozen frame is read-only).
		syncFramePool.Put(v.syncFrame)
		v.syncFrame = nil
	}
	v.syncScrollbackLen = 0
	if v.syncPreserveViewport && v.syncViewOffsetDelta != 0 {
		v.ViewOffset += v.syncViewOffsetDelta
	}
	v.syncViewOffsetDelta = 0
	v.syncPreserveViewport = false
	if v.syncDeferTrim {
		v.syncDeferTrim = false
		v.trimScrollback()
	} else {
		v.clampViewOffsetToCurrentMax()
	}
	// Writes made during the sync window marked their lines dirty and bumped
	// the version, and no render cleared that state while the freeze was up,
	// so the next live render repaints exactly the changed lines. A blanket
	// invalidation here would turn every synced frame into a full repaint.
	v.bumpVersion()
}

// syncFrame is the pooled backing for a synchronized-output snapshot. tmux
// brackets every client redraw in DEC 2026, so an unpooled copy allocated a
// fresh [][]Cell plus every row on every rendered frame (~51 allocs and
// ~616KB per begin on a 200x50 screen).
var syncFramePool = sync.Pool{New: func() any { return &syncFrame{} }}

type syncFrame struct {
	rows [][]Cell
}

// freeze copies src into the reusable buffer, growing rows only when the
// pool entry is fresh or a row widened past its retained capacity. Each
// row keeps its own backing because live rows may exceed v.Width (see
// resizeRows' "may stay wider" contract).
func (f *syncFrame) freeze(src [][]Cell) [][]Cell {
	if cap(f.rows) < len(src) {
		f.rows = make([][]Cell, len(src))
	}
	f.rows = f.rows[:len(src)]
	for i := range src {
		if cap(f.rows[i]) < len(src[i]) {
			f.rows[i] = make([]Cell, len(src[i]))
		}
		f.rows[i] = f.rows[i][:len(src[i])]
		copy(f.rows[i], src[i])
	}
	return f.rows
}
