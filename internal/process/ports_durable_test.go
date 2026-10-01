package process

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

// --- Durable mode (shared reservation registry) -----------------------------

// durableAllocator wires a PortAllocator to the shared registry under home —
// the pair of objects two app processes would independently construct.
func durableAllocator(t *testing.T, home string, start, size int) *PortAllocator {
	t.Helper()
	store := data.NewPortReservationStore(home)
	if err := store.Initialize(nil); err != nil {
		t.Fatalf("store Initialize: %v", err)
	}
	p := NewPortAllocator(start, size)
	p.SetDurableStore(store)
	return p
}

// savedWorkspace returns a workspace whose metadata record exists — the only
// shape durable allocation accepts.
func savedWorkspace(t *testing.T, metaRoot, name string) *data.Workspace {
	t.Helper()
	store := data.NewWorkspaceStore(metaRoot)
	ws := &data.Workspace{
		Name: name,
		Repo: t.TempDir(),
		Root: t.TempDir(),
	}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save(%s): %v", name, err)
	}
	return ws
}

// TestPortAllocator_DurableTwoAllocatorsShareRegistry proves two allocator
// objects on the same state home draw from one reservation space: same stored
// ID → identical interval, different ID → disjoint.
func TestPortAllocator_DurableTwoAllocatorsShareRegistry(t *testing.T) {
	home, meta := t.TempDir(), t.TempDir()
	a := durableAllocator(t, home, 6200, 10)
	b := durableAllocator(t, home, 6200, 10)

	wsA := savedWorkspace(t, meta, "a")
	wsB := savedWorkspace(t, meta, "b")

	baseA, endA, err := a.ReserveWorkspace(wsA)
	if err != nil {
		t.Fatalf("ReserveWorkspace(a) = %v", err)
	}
	// A different allocator object re-reserving the same stored ID gets the
	// persisted interval verbatim.
	againBase, againEnd, err := b.ReserveWorkspace(wsA)
	if err != nil {
		t.Fatalf("ReserveWorkspace(a via b) = %v", err)
	}
	if againBase != baseA || againEnd != endA {
		t.Fatalf("id re-reserve = %d-%d, want %d-%d", againBase, againEnd, baseA, endA)
	}
	baseB, endB, err := b.ReserveWorkspace(wsB)
	if err != nil {
		t.Fatalf("ReserveWorkspace(b) = %v", err)
	}
	if baseB <= endA && endB >= baseA {
		t.Fatalf("b interval %d-%d overlaps a's %d-%d", baseB, endB, baseA, endA)
	}
}

// TestPortAllocator_DurableReleaseRetains proves durable release is a no-op:
// a released reservation can never be reissued to a different workspace.
func TestPortAllocator_DurableReleaseRetains(t *testing.T) {
	home, meta := t.TempDir(), t.TempDir()
	a := durableAllocator(t, home, 6200, 10)
	b := durableAllocator(t, home, 6200, 10)

	wsA := savedWorkspace(t, meta, "a")
	wsB := savedWorkspace(t, meta, "b")

	baseA, _, err := a.ReserveWorkspace(wsA)
	if err != nil {
		t.Fatal(err)
	}
	a.ReleasePort(wsA.Root)

	// B's first reservation must skip a's retained interval — no reuse.
	baseB, endB, err := b.ReserveWorkspace(wsB)
	if err != nil {
		t.Fatal(err)
	}
	if baseB == baseA || (baseB <= baseA+9 && endB >= baseA) {
		t.Fatalf("b got %d-%d overlapping a's retained base %d", baseB, endB, baseA)
	}
	// And a's own re-reserve restores its original interval.
	againBase, _, err := a.ReserveWorkspace(wsA)
	if err != nil {
		t.Fatal(err)
	}
	if againBase != baseA {
		t.Fatalf("a re-reserve = %d, want original %d", againBase, baseA)
	}
}

// TestPortAllocator_DurableUnsavedWorkspaceDegrades pins the degrade
// contract: with no persisted store key there is no durable key, so the
// allocator falls back to the root-keyed in-memory map rather than blocking
// the spawn — a transient store error during load must not wedge the
// workspace for the rest of the session. The durable registry stays
// untouched: no phantom record is minted under a drifting computed ID.
func TestPortAllocator_DurableUnsavedWorkspaceDegrades(t *testing.T) {
	home := t.TempDir()
	p := durableAllocator(t, home, 6200, 10)
	ws := &data.Workspace{Name: "unsaved", Repo: t.TempDir(), Root: t.TempDir()}

	base, end, err := p.ReserveWorkspace(ws)
	if err != nil {
		t.Fatalf("ReserveWorkspace(unsaved) error = %v, want transient fallback", err)
	}
	if base != 6200 || end != 6209 {
		t.Fatalf("transient fallback interval = %d-%d, want 6200-6209", base, end)
	}
	if port, ok := p.GetPort(ws.Root); !ok || port != base {
		t.Fatalf("in-memory map missing transient base: got %d,%v want %d,true", port, ok, base)
	}
	// The status lookup reports the transient interval truthfully, and the
	// registry itself still holds no record for the unsaved workspace.
	lb, le, found, err := p.LookupWorkspaceInterval(ws)
	if err != nil || !found || lb != base || le != end {
		t.Fatalf("LookupWorkspaceInterval(unsaved) = (%d,%d,%v,%v), want (%d,%d,true,nil)", lb, le, found, err, base, end)
	}
}

// TestPortAllocator_DurableMirrorSkipsTransientDegrade pins the forward
// collision: a durable reservation mirrored into the local map must block the
// transient degrade path — both the nextPort fast path and the scan — even
// though the mirror never advanced nextPort.
func TestPortAllocator_DurableMirrorSkipsTransientDegrade(t *testing.T) {
	home, meta := t.TempDir(), t.TempDir()
	p := durableAllocator(t, home, 6200, 10)
	wsA := savedWorkspace(t, meta, "a")

	baseA, endA, err := p.ReserveWorkspace(wsA)
	if err != nil {
		t.Fatal(err)
	}
	if baseA != 6200 || endA != 6209 {
		t.Fatalf("durable reserve = %d-%d, want 6200-6209", baseA, endA)
	}
	unsaved := &data.Workspace{Name: "u", Repo: t.TempDir(), Root: t.TempDir()}
	baseU, endU, err := p.ReserveWorkspace(unsaved)
	if err != nil {
		t.Fatalf("ReserveWorkspace(unsaved) error = %v", err)
	}
	if baseU <= endA && endU >= baseA {
		t.Fatalf("transient degrade interval %d-%d collides with durable %d-%d", baseU, endU, baseA, endA)
	}
	if baseU != 6210 || endU != 6219 {
		t.Fatalf("transient degrade interval = %d-%d, want 6210-6219", baseU, endU)
	}
}

// TestPortAllocator_TransientAllocAvoidsDurableMint pins the reverse
// collision: a transient range handed to an unsaved workspace must be carried
// into the durable mint as an exclusion, or the registry would re-issue it to
// a saved workspace.
func TestPortAllocator_TransientAllocAvoidsDurableMint(t *testing.T) {
	home, meta := t.TempDir(), t.TempDir()
	p := durableAllocator(t, home, 6200, 10)

	unsaved := &data.Workspace{Name: "u", Repo: t.TempDir(), Root: t.TempDir()}
	baseU, endU, err := p.ReserveWorkspace(unsaved)
	if err != nil {
		t.Fatalf("ReserveWorkspace(unsaved) error = %v", err)
	}
	if baseU != 6200 || endU != 6209 {
		t.Fatalf("transient interval = %d-%d, want 6200-6209", baseU, endU)
	}

	wsA := savedWorkspace(t, meta, "a")
	baseA, endA, err := p.ReserveWorkspace(wsA)
	if err != nil {
		t.Fatal(err)
	}
	if baseA <= endU && endA >= baseU {
		t.Fatalf("durable mint %d-%d collides with live transient %d-%d", baseA, endA, baseU, endU)
	}
	if baseA != 6210 || endA != 6219 {
		t.Fatalf("durable mint = %d-%d, want 6210-6219", baseA, endA)
	}
}

// TestPortAllocator_CrossInstanceDegradeAvoidsRegistry proves the degrade path
// consults the persisted registry, not just the local mirror: a second
// allocator that never mirrored wsA's interval must still skip it.
func TestPortAllocator_CrossInstanceDegradeAvoidsRegistry(t *testing.T) {
	home, meta := t.TempDir(), t.TempDir()
	a := durableAllocator(t, home, 6200, 10)
	wsA := savedWorkspace(t, meta, "a")
	baseA, endA, err := a.ReserveWorkspace(wsA)
	if err != nil {
		t.Fatal(err)
	}

	b := durableAllocator(t, home, 6200, 10)
	unsaved := &data.Workspace{Name: "u", Repo: t.TempDir(), Root: t.TempDir()}
	baseU, endU, err := b.ReserveWorkspace(unsaved)
	if err != nil {
		t.Fatalf("b ReserveWorkspace(unsaved) error = %v", err)
	}
	if baseU <= endA && endU >= baseA {
		t.Fatalf("cross-instance degrade %d-%d collides with persisted %d-%d", baseU, endU, baseA, endA)
	}
	if baseU != 6210 {
		t.Fatalf("cross-instance degrade base = %d, want 6210", baseU)
	}
}

// TestPortAllocator_LegacyWiderIntervalFullyAvoided covers intervals minted
// under a wider configured width: the stored end — not base+currentSize-1 —
// is what the transient used set must avoid, or the tail of a legacy-wide
// reservation is re-issued.
func TestPortAllocator_LegacyWiderIntervalFullyAvoided(t *testing.T) {
	home, meta := t.TempDir(), t.TempDir()
	store := data.NewPortReservationStore(home)
	if err := store.Initialize(nil); err != nil {
		t.Fatal(err)
	}
	wsA := savedWorkspace(t, meta, "a")
	id, ok := wsA.StoredID()
	if !ok {
		t.Fatal("savedWorkspace produced no stored ID")
	}
	// A previous release reserved wsA at width 25: stored interval 6200-6224.
	if _, _, err := store.Reserve(string(id), 6200, 25); err != nil {
		t.Fatal(err)
	}

	// Current config runs at width 10; the stored record wins verbatim.
	p := durableAllocator(t, home, 6200, 10)
	baseA, endA, err := p.ReserveWorkspace(wsA)
	if err != nil {
		t.Fatal(err)
	}
	if baseA != 6200 || endA != 6224 {
		t.Fatalf("re-reserve = %d-%d, want stored 6200-6224", baseA, endA)
	}

	unsaved := &data.Workspace{Name: "u", Repo: t.TempDir(), Root: t.TempDir()}
	baseU, endU, err := p.ReserveWorkspace(unsaved)
	if err != nil {
		t.Fatalf("ReserveWorkspace(unsaved) error = %v", err)
	}
	// Under the old base+rangeSize-1 accounting, 6210-6219 would look free;
	// the true stored end covers it, so the first legal base is 6230.
	if baseU != 6230 || endU != 6239 {
		t.Fatalf("transient degrade interval = %d-%d, want 6230-6239", baseU, endU)
	}
}

// TestPortAllocator_DurableLookupNeverAllocates proves the authoritative read
// observes without creating reservations.
func TestPortAllocator_DurableLookupNeverAllocates(t *testing.T) {
	home, meta := t.TempDir(), t.TempDir()
	p := durableAllocator(t, home, 6200, 10)
	ws := savedWorkspace(t, meta, "a")

	if _, _, found, err := p.LookupWorkspaceInterval(ws); err != nil || found {
		t.Fatalf("Lookup before reserve = found %v err %v, want false/nil", found, err)
	}
	base, end, err := p.ReserveWorkspace(ws)
	if err != nil {
		t.Fatal(err)
	}
	lb, le, found, err := p.LookupWorkspaceInterval(ws)
	if err != nil || !found || lb != base || le != end {
		t.Fatalf("Lookup after reserve = (%d,%d,%v,%v), want (%d,%d,true,nil)", lb, le, found, err, base, end)
	}
	// An unsaved workspace reports not-found, not an error.
	unsaved := &data.Workspace{Name: "u", Repo: t.TempDir(), Root: t.TempDir()}
	if _, _, found, err := p.LookupWorkspaceInterval(unsaved); err != nil || found {
		t.Fatalf("Lookup(unsaved) = found %v err %v, want false/nil", found, err)
	}
}
