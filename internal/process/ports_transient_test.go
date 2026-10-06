package process

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

// --- Published transient holds (shared registry) -----------------------------

// TestPortAllocator_TransientHoldBlocksCrossInstanceDurableMint proves the
// published-ownership contract in the direction the defect named first: a
// degraded workspace's transient hold is visible to a second instance's
// durable mint, which must skip the held interval rather than re-issue it.
func TestPortAllocator_TransientHoldBlocksCrossInstanceDurableMint(t *testing.T) {
	home, meta := t.TempDir(), t.TempDir()
	a := durableAllocator(t, home, 6200, 10)
	b := durableAllocator(t, home, 6200, 10)

	unsaved := &data.Workspace{Name: "degraded", Repo: t.TempDir(), Root: t.TempDir()}
	baseU, endU, err := a.ReserveWorkspace(unsaved)
	if err != nil {
		t.Fatalf("a transient mint: %v", err)
	}

	wsB := savedWorkspace(t, meta, "b")
	baseB, endB, err := b.ReserveWorkspace(wsB)
	if err != nil {
		t.Fatalf("b durable mint: %v", err)
	}
	if baseB <= endU && endB >= baseU {
		t.Fatalf("b durable mint %d-%d collides with a's published transient %d-%d", baseB, endB, baseU, endU)
	}
	if baseB != endU+1 {
		t.Fatalf("b durable mint base = %d, want %d (first base past the hold)", baseB, endU+1)
	}
}

// TestPortAllocator_TransientHoldBlocksCrossInstanceTransientMint proves
// degraded-instance-vs-degraded-instance exclusion — the case a disjoint
// transient band could never cover, because two purely local allocators
// have no shared view. Both transient mints land in the registry, so the
// second must skip the first's interval.
func TestPortAllocator_TransientHoldBlocksCrossInstanceTransientMint(t *testing.T) {
	home := t.TempDir()
	a := durableAllocator(t, home, 6200, 10)
	b := durableAllocator(t, home, 6200, 10)

	wsA := &data.Workspace{Name: "ua", Repo: t.TempDir(), Root: t.TempDir()}
	wsB := &data.Workspace{Name: "ub", Repo: t.TempDir(), Root: t.TempDir()}

	baseA, endA, err := a.ReserveWorkspace(wsA)
	if err != nil {
		t.Fatalf("a transient mint: %v", err)
	}
	baseB, endB, err := b.ReserveWorkspace(wsB)
	if err != nil {
		t.Fatalf("b transient mint: %v", err)
	}
	if baseB <= endA && endB >= baseA {
		t.Fatalf("b transient mint %d-%d collides with a's %d-%d", baseB, endB, baseA, endA)
	}
	if baseB != endA+1 {
		t.Fatalf("b transient mint base = %d, want %d", baseB, endA+1)
	}
}

// TestPortAllocator_TransientHoldReReservesAcrossInstances proves the
// re-open path: instance A restarts and its degraded workspace re-reserves.
// A fresh allocator object replaying the same transient key gets the
// persisted interval verbatim — the hold is keyed by (pid, root), so a
// process restart mints a NEW key, but within one process the record is
// stable and idempotent.
func TestPortAllocator_TransientHoldIdempotentInProcess(t *testing.T) {
	home := t.TempDir()
	p := durableAllocator(t, home, 6200, 10)
	ws := &data.Workspace{Name: "u", Repo: t.TempDir(), Root: t.TempDir()}

	base, end, err := p.ReserveWorkspace(ws)
	if err != nil {
		t.Fatal(err)
	}
	again, againEnd, err := p.ReserveWorkspace(ws)
	if err != nil {
		t.Fatal(err)
	}
	if again != base || againEnd != end {
		t.Fatalf("re-reserve = %d-%d, want %d-%d", again, againEnd, base, end)
	}
	// Exactly one record total — the replay must not duplicate the hold.
	snap, err := data.NewPortReservationStore(home).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap) != 1 {
		t.Fatalf("registry holds %d records after replay, want 1", len(snap))
	}
}

// TestPortAllocator_StoredIDUpgradeCoexists proves the upgrade-continuity
// contract: a degraded workspace mints a transient hold; once its metadata
// record lands and a stored ID exists, the durable mint gets a DISJOINT
// interval alongside the still-live transient hold. The running sessions
// bound to the transient ports are never shadowed, and nothing double-books
// the durable range.
func TestPortAllocator_StoredIDUpgradeCoexists(t *testing.T) {
	home, meta := t.TempDir(), t.TempDir()
	p := durableAllocator(t, home, 6200, 10)

	root := t.TempDir()
	ws := &data.Workspace{Name: "upgrade", Repo: t.TempDir(), Root: root}
	baseU, endU, err := p.ReserveWorkspace(ws)
	if err != nil {
		t.Fatalf("transient mint: %v", err)
	}

	// The metadata store materializes — the workspace gains a durable key.
	store := data.NewWorkspaceStore(meta)
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, ok := ws.StoredID(); !ok {
		t.Fatal("Save produced no stored ID")
	}

	baseD, endD, err := p.ReserveWorkspace(ws)
	if err != nil {
		t.Fatalf("durable mint after upgrade: %v", err)
	}
	if baseD <= endU && endD >= baseU {
		t.Fatalf("durable mint %d-%d collides with live transient hold %d-%d", baseD, endD, baseU, endU)
	}

	// Both records coexist in the registry: the transient hold keeps the
	// running sessions' interval covered; the durable record owns future
	// spawns.
	snap, err := data.NewPortReservationStore(home).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap) != 2 {
		t.Fatalf("registry holds %d records, want transient + durable", len(snap))
	}
}

// TestPortAllocator_ReservedIntervalsHidesTransientHolds proves transient
// holds never surface in the reclaim enumeration — they are per-process and
// must not be user-releasable.
func TestPortAllocator_ReservedIntervalsHidesTransientHolds(t *testing.T) {
	home, meta := t.TempDir(), t.TempDir()
	p := durableAllocator(t, home, 6200, 10)

	unsaved := &data.Workspace{Name: "u", Repo: t.TempDir(), Root: t.TempDir()}
	if _, _, err := p.ReserveWorkspace(unsaved); err != nil {
		t.Fatal(err)
	}
	ws := savedWorkspace(t, meta, "durable")
	if _, _, err := p.ReserveWorkspace(ws); err != nil {
		t.Fatal(err)
	}

	snap, err := p.ReservedIntervals()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap) != 1 {
		t.Fatalf("ReservedIntervals = %d entries, want durable only", len(snap))
	}
	for id := range snap {
		if data.IsTransientReservationID(id) {
			t.Fatalf("transient hold %q leaked into reclaim enumeration", id)
		}
	}
}

// TestPortAllocator_ReleasePortFreesTransientHold proves workspace deletion
// frees the caller's own transient hold: after release, another instance's
// mint may reclaim the interval — the deleted workspace's sessions are gone,
// so its ports are genuinely free.
func TestPortAllocator_ReleasePortFreesTransientHold(t *testing.T) {
	home, meta := t.TempDir(), t.TempDir()
	a := durableAllocator(t, home, 6200, 10)
	b := durableAllocator(t, home, 6200, 10)

	unsaved := &data.Workspace{Name: "u", Repo: t.TempDir(), Root: t.TempDir()}
	baseU, endU, err := a.ReserveWorkspace(unsaved)
	if err != nil {
		t.Fatal(err)
	}
	a.ReleasePort(unsaved.Root)

	wsB := savedWorkspace(t, meta, "b")
	baseB, endB, err := b.ReserveWorkspace(wsB)
	if err != nil {
		t.Fatal(err)
	}
	if baseB != baseU || endB != endU {
		t.Fatalf("b mint %d-%d, want released interval %d-%d reclaimed", baseB, endB, baseU, endU)
	}
	if _, ok := a.GetPort(unsaved.Root); ok {
		t.Fatal("released workspace still reports a held port")
	}
}

// TestPortAllocator_ReleasePortLeavesDurableRecord pins that ReleasePort
// still never frees a durable reservation — only the transient-hold
// exception applies.
func TestPortAllocator_ReleasePortLeavesDurableRecord(t *testing.T) {
	home, meta := t.TempDir(), t.TempDir()
	p := durableAllocator(t, home, 6200, 10)
	ws := savedWorkspace(t, meta, "a")

	base, end, err := p.ReserveWorkspace(ws)
	if err != nil {
		t.Fatal(err)
	}
	p.ReleasePort(ws.Root)

	snap, err := data.NewPortReservationStore(home).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	id, _ := ws.StoredID()
	iv, ok := snap[string(id)]
	if !ok || iv.Start != base || iv.End != end {
		t.Fatalf("durable record lost to ReleasePort: snap[%q] = %+v,%v", id, iv, ok)
	}
}
