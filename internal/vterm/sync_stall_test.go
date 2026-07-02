package vterm

import (
	"strings"
	"testing"
	"time"
)

const (
	syncBegin = "\x1b[?2026h"
	syncEnd   = "\x1b[?2026l"
)

// stubSyncClock replaces the sync clock with a controllable one and restores
// it when the test ends.
func stubSyncClock(t *testing.T) *time.Time {
	t.Helper()
	now := time.Unix(1000, 0)
	prev := syncNow
	syncNow = func() time.Time { return now }
	t.Cleanup(func() { syncNow = prev })
	return &now
}

// TestSyncStall_WriteReleasesOverdueSync: a sync-begin with no end must not
// freeze rendering past SyncStallTimeout; the next Write force-releases it.
func TestSyncStall_WriteReleasesOverdueSync(t *testing.T) {
	now := stubSyncClock(t)
	vt := New(20, 4)

	vt.Write([]byte("before\r\n"))
	vt.Write([]byte(syncBegin + "during"))
	if !vt.SyncActive() {
		t.Fatal("sync should be active after begin")
	}
	// Writes inside the timeout keep the sync open.
	*now = now.Add(SyncStallTimeout / 2)
	vt.Write([]byte(" more"))
	if !vt.SyncActive() {
		t.Fatal("sync should stay active within the stall timeout")
	}

	*now = now.Add(SyncStallTimeout + time.Second)
	vt.Write([]byte(" late"))
	if vt.SyncActive() {
		t.Fatal("overdue sync must be force-released on Write")
	}
	if !strings.Contains(vt.Render(), "during more late") {
		t.Fatalf("expected content written during stalled sync to be visible, got:\n%s", vt.Render())
	}
}

// TestSyncStall_RenderReleasesOverdueSync: with no further output at all, the
// next rendered frame must unfreeze an overdue sync.
func TestSyncStall_RenderReleasesOverdueSync(t *testing.T) {
	now := stubSyncClock(t)
	vt := New(20, 4)

	vt.Write([]byte("frozen-frame\r\n"))
	vt.Write([]byte(syncBegin + "hidden"))
	if got := vt.Render(); strings.Contains(got, "hidden") {
		t.Fatalf("content inside an open sync must stay hidden, got:\n%s", got)
	}

	*now = now.Add(SyncStallTimeout + time.Second)
	if got := vt.Render(); !strings.Contains(got, "hidden") {
		t.Fatalf("overdue sync must be force-released on render, got:\n%s", got)
	}
	if vt.SyncActive() {
		t.Fatal("sync should be released after stale render")
	}
}

// TestSyncStall_VersionReleasesOverdueSync pins the UI snapshot-cache path:
// center/sidebar compare cached snapshots by VTerm.Version before calling
// RenderBuffers, so Version must also release stale sync regions.
func TestSyncStall_VersionReleasesOverdueSync(t *testing.T) {
	now := stubSyncClock(t)
	vt := New(20, 4)

	vt.Write([]byte("before\r\n"))
	versionBeforeSync := vt.Version()
	vt.Write([]byte(syncBegin + "hidden"))
	versionDuringSync := vt.Version()
	if versionDuringSync == versionBeforeSync {
		t.Fatal("write during sync should still bump version")
	}
	if !vt.SyncActive() {
		t.Fatal("sync should be active before timeout")
	}

	*now = now.Add(SyncStallTimeout + time.Second)
	versionAfterTimeout := vt.Version()
	if vt.SyncActive() {
		t.Fatal("Version should release overdue sync before cache comparison")
	}
	if versionAfterTimeout == versionDuringSync {
		t.Fatal("Version should advance when it force-releases stale sync")
	}
}

// TestSync_PreservesLineDirtyTracking: a sync begin/end pair must not blow the
// per-line dirty state into a full invalidation. Only lines written during the
// sync window should render dirty afterwards.
func TestSync_PreservesLineDirtyTracking(t *testing.T) {
	vt := New(20, 4)
	vt.Write([]byte("l0\r\nl1\r\nl2"))

	// Establish a clean baseline.
	_ = vt.Render()
	vt.ClearDirty()
	if _, all := vt.DirtyLines(); all {
		t.Fatal("expected clean baseline before sync")
	}

	// One synced frame that touches only the current cursor line.
	vt.Write([]byte(syncBegin + "X" + syncEnd))

	dirty, all := vt.DirtyLines()
	if all {
		t.Fatal("sync end must not force a full invalidation")
	}
	wantDirty := []bool{false, false, true, false}
	for y, want := range wantDirty {
		if dirty[y] != want {
			t.Fatalf("line %d dirty = %v, want %v (dirty=%v)", y, dirty[y], want, dirty)
		}
	}
}

// TestSync_FrozenFrameHidesPartialWrites is the flicker property: content
// written between sync begin and end must never be observable in a render.
func TestSync_FrozenFrameHidesPartialWrites(t *testing.T) {
	vt := New(40, 6)
	vt.Write([]byte("stable\r\n"))

	vt.Write([]byte(syncBegin + "\x1b[2J\x1b[H")) // clear inside sync
	if got := vt.Render(); !strings.Contains(got, "stable") {
		t.Fatalf("frozen frame must keep pre-sync content visible, got:\n%s", got)
	}

	vt.Write([]byte("repainted" + syncEnd))
	got := vt.Render()
	if !strings.Contains(got, "repainted") {
		t.Fatalf("post-sync render must show the completed frame, got:\n%s", got)
	}
	if strings.Contains(got, "stable") {
		t.Fatalf("cleared content must be gone after the synced repaint, got:\n%s", got)
	}
}
