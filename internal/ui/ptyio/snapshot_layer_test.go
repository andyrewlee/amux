package ptyio

import (
	"testing"

	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/vterm"
)

func TestCachedSnapshotLayerLockedReusesUntilVersionChanges(t *testing.T) {
	restore := perf.EnableForTest()
	defer restore()
	perf.Snapshot() // clear

	vt := vterm.New(20, 5)
	st := &State{}

	if layer := st.CachedSnapshotLayerLocked(vt, vt.Version(), true); layer == nil {
		t.Fatalf("first CachedSnapshotLayerLocked returned nil")
	}
	snapA := st.CachedSnap
	if snapA == nil {
		t.Fatalf("CachedSnap not populated on first (miss) call")
	}
	_, counters := perf.Snapshot()
	if v := counterValue(counters, "vterm_snapshot_cache_miss"); v != 1 {
		t.Fatalf("cache_miss = %d, want 1 after first call", v)
	}
	if v := counterValue(counters, "vterm_snapshot_cache_hit"); v != 0 {
		t.Fatalf("cache_hit = %d, want 0 after first call", v)
	}

	// Same version + showCursor: reuse the cached snapshot (hit, pointer stable).
	if layer := st.CachedSnapshotLayerLocked(vt, vt.Version(), true); layer == nil {
		t.Fatalf("second CachedSnapshotLayerLocked returned nil")
	}
	if st.CachedSnap != snapA {
		t.Fatalf("CachedSnap changed on a cache hit")
	}
	_, counters = perf.Snapshot()
	if v := counterValue(counters, "vterm_snapshot_cache_hit"); v != 1 {
		t.Fatalf("cache_hit = %d, want 1 after a reuse", v)
	}

	// A version bump invalidates the cache (miss, fresh snapshot).
	vt.Write([]byte("x"))
	if layer := st.CachedSnapshotLayerLocked(vt, vt.Version(), true); layer == nil {
		t.Fatalf("post-write CachedSnapshotLayerLocked returned nil")
	}
	if st.CachedSnap == snapA {
		t.Fatalf("CachedSnap not refreshed after a version bump")
	}
	_, counters = perf.Snapshot()
	if v := counterValue(counters, "vterm_snapshot_cache_miss"); v != 1 {
		t.Fatalf("cache_miss = %d, want 1 after version bump", v)
	}
}
