package ptyio

import (
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/compositor"
	"github.com/andyrewlee/amux/internal/vterm"
)

// CachedSnapshotLayerLocked returns a VTermLayer for term, reusing the cached
// snapshot when the terminal version and cursor visibility are both unchanged
// and otherwise taking a fresh double-buffered snapshot. It emits the shared
// vterm_snapshot_cache_hit/miss counters and refreshes the snapshot cache. It
// returns nil when the double buffer cannot build a snapshot. The caller must
// hold the pane mutex (the same lock held when snapshotting the VTerm).
//
// This is the shared non-chat snapshot skeleton used by both the sidebar
// terminal and the center pane. Center wraps it with chat-tab cursor
// post-processing on its own; only the non-chat arm routes here.
func (st *State) CachedSnapshotLayerLocked(term *vterm.VTerm, version uint64, showCursor bool) *compositor.VTermLayer {
	if st.CachedSnap != nil && st.CachedVersion == version && st.CachedShowCursor == showCursor {
		perf.Count("vterm_snapshot_cache_hit", 1)
		return compositor.NewVTermLayer(st.CachedSnap)
	}

	// SnapshotDoubleBuffer reuses rows without mutating the last handed-out layer.
	snap := st.SnapshotBuffer.Snapshot(term, showCursor)
	if snap == nil {
		return nil
	}
	perf.Count("vterm_snapshot_cache_miss", 1)

	st.CachedSnap = snap
	st.CachedVersion = version
	st.CachedShowCursor = showCursor
	return compositor.NewVTermLayer(snap)
}
