package process

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func mustAlloc(t *testing.T, p *PortAllocator, root string) int {
	t.Helper()
	port, err := p.AllocatePort(root)
	if err != nil {
		t.Fatalf("AllocatePort(%q) error = %v", root, err)
	}
	return port
}

func allocOrErr(p *PortAllocator, root string) int {
	port, _ := p.AllocatePort(root)
	return port
}

func TestPortAllocator_AllocatePort(t *testing.T) {
	p := NewPortAllocator(6200, 10)

	// First allocation
	port1 := mustAlloc(t, p, "/workspace1")
	if port1 != 6200 {
		t.Errorf("First allocation = %d, want 6200", port1)
	}

	// Second allocation
	port2 := mustAlloc(t, p, "/workspace2")
	if port2 != 6210 {
		t.Errorf("Second allocation = %d, want 6210", port2)
	}

	// Same workspace should return same port
	port1Again := mustAlloc(t, p, "/workspace1")
	if port1Again != port1 {
		t.Errorf("Same workspace returned different port: %d != %d", port1Again, port1)
	}
}

func TestPortAllocator_GetPort(t *testing.T) {
	p := NewPortAllocator(6200, 10)

	// Before allocation
	_, ok := p.GetPort("/workspace1")
	if ok {
		t.Error("GetPort should return false for unallocated workspace")
	}

	// After allocation
	mustAlloc(t, p, "/workspace1")
	port, ok := p.GetPort("/workspace1")
	if !ok {
		t.Error("GetPort should return true for allocated workspace")
	}
	if port != 6200 {
		t.Errorf("GetPort = %d, want 6200", port)
	}
}

func TestPortAllocator_ReleasePort(t *testing.T) {
	p := NewPortAllocator(6200, 10)

	mustAlloc(t, p, "/workspace1")
	p.ReleasePort("/workspace1")

	_, ok := p.GetPort("/workspace1")
	if ok {
		t.Error("GetPort should return false after release")
	}
}

func TestPortAllocator_ReusesReleasedBase(t *testing.T) {
	p := NewPortAllocator(6200, 10)

	if got := mustAlloc(t, p, "/workspace-a"); got != 6200 {
		t.Fatalf("AllocatePort(A) = %d, want 6200", got)
	}
	p.ReleasePort("/workspace-a")
	if got := mustAlloc(t, p, "/workspace-b"); got != 6200 {
		t.Fatalf("AllocatePort(B after release) = %d, want 6200", got)
	}
	if got := mustAlloc(t, p, "/workspace-c"); got != 6210 {
		t.Fatalf("AllocatePort(C) = %d, want 6210", got)
	}
}

func TestPortAllocator_SameRootRecreateDoesNotLeak(t *testing.T) {
	p := NewPortAllocator(6200, 10)

	for i := 0; i < 100; i++ {
		if got := mustAlloc(t, p, "/workspace"); got != 6200 {
			t.Fatalf("iteration %d AllocatePort(/workspace) = %d, want 6200", i, got)
		}
		p.ReleasePort("/workspace")
	}
	if got := mustAlloc(t, p, "/fresh"); got != 6200 {
		t.Fatalf("AllocatePort(/fresh) = %d, want 6200", got)
	}
	if got := mustAlloc(t, p, "/fresh-2"); got >= 6230 {
		t.Fatalf("next fresh base ran away to %d", got)
	}
}

func TestPortAllocator_ExhaustionScanStaysInRange(t *testing.T) {
	p := NewPortAllocator(65500, 10)

	for i, want := range []int{65500, 65510, 65520} {
		if got := mustAlloc(t, p, fmt.Sprintf("/workspace-%d", i)); got != want {
			t.Fatalf("AllocatePort(%d) = %d, want %d", i, got, want)
		}
	}
	p.ReleasePort("/workspace-1")
	if got := mustAlloc(t, p, "/workspace-reused"); got != 65510 {
		t.Fatalf("AllocatePort(reused) = %d, want 65510", got)
	}
	if _, err := p.AllocatePort("/workspace-exhausted"); !errors.Is(err, ErrPortRangeExhausted) {
		t.Fatalf("AllocatePort(exhausted) error = %v, want ErrPortRangeExhausted", err)
	}
}

func TestPortAllocator_FullExhaustionReturnsError(t *testing.T) {
	p := NewPortAllocator(1, 32768)

	if got := mustAlloc(t, p, "/workspace-1"); got != 1 {
		t.Fatalf("AllocatePort(first) = %d, want 1", got)
	}
	// Exhaustion is a returned error, never a panic — a panic inside a Cmd
	// closure would degrade to a generic message and skip lifecycle cleanup.
	if _, err := p.AllocatePort("/workspace-2"); !errors.Is(err, ErrPortRangeExhausted) {
		t.Fatalf("AllocatePort(exhausted) error = %v, want ErrPortRangeExhausted", err)
	}
	if _, _, err := p.PortRange("/workspace-3"); !errors.Is(err, ErrPortRangeExhausted) {
		t.Fatalf("PortRange(exhausted) error = %v, want ErrPortRangeExhausted", err)
	}
}

func TestPortAllocator_PortRange(t *testing.T) {
	p := NewPortAllocator(6200, 10)

	port, rangeEnd, err := p.PortRange("/workspace1")
	if err != nil {
		t.Fatalf("PortRange() error = %v", err)
	}
	if port != 6200 {
		t.Errorf("port = %d, want 6200", port)
	}
	if rangeEnd != 6209 {
		t.Errorf("rangeEnd = %d, want 6209", rangeEnd)
	}
}

func TestPortAllocator_ConcurrentAccess(t *testing.T) {
	const (
		n         = 50
		portStart = 6200
		rangeSize = 10
	)
	p := NewPortAllocator(portStart, rangeSize)

	// Each goroutine allocates a distinct workspaceRoot. The real invariant is
	// that distinct workspaces receive distinct, non-overlapping port ranges,
	// regardless of the order the mutex serializes concurrent callers in.
	bases := make([]int, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			bases[i] = allocOrErr(p, fmt.Sprintf("/ws%d", i))
		}(i)
	}
	wg.Wait()

	// (1) Exactly N distinct bases (no duplicate handed to two workspaces).
	seen := make(map[int]int, n)
	for i, base := range bases {
		if prev, dup := seen[base]; dup {
			t.Fatalf("duplicate base port %d allocated to /ws%d and /ws%d", base, prev, i)
		}
		seen[base] = i
	}
	if len(seen) != n {
		t.Fatalf("got %d distinct bases, want %d", len(seen), n)
	}

	// (2) Every base lies within [portStart, portStart+N*rangeSize).
	limit := portStart + n*rangeSize
	for i, base := range bases {
		if base < portStart || base >= limit {
			t.Errorf("/ws%d base %d out of range [%d, %d)", i, base, portStart, limit)
		}
		// A valid base must sit on a rangeSize boundary from portStart.
		if (base-portStart)%rangeSize != 0 {
			t.Errorf("/ws%d base %d not aligned to rangeSize %d from %d", i, base, rangeSize, portStart)
		}
	}

	// (3) Ranges [base, base+rangeSize-1] are pairwise non-overlapping.
	sorted := append([]int(nil), bases...)
	sort.Ints(sorted)
	for i := 1; i < len(sorted); i++ {
		prevEnd := sorted[i-1] + rangeSize - 1
		if sorted[i] <= prevEnd {
			t.Errorf("overlapping ranges: [%d, %d] and [%d, %d]",
				sorted[i-1], prevEnd, sorted[i], sorted[i]+rangeSize-1)
		}
	}
}

func TestPortAllocator_ConcurrentAllocateRelease(t *testing.T) {
	const (
		workers   = 8
		cycles    = 100
		portStart = 6200
		rangeSize = 10
	)
	p := NewPortAllocator(portStart, rangeSize)
	errCh := make(chan string, workers*cycles)

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			root := fmt.Sprintf("/workspace-%d", i)
			for j := 0; j < cycles; j++ {
				base := allocOrErr(p, root)
				if base < portStart || base > 65535 || (base-portStart)%rangeSize != 0 {
					errCh <- fmt.Sprintf("worker %d cycle %d got invalid base %d", i, j, base)
				}
				p.ReleasePort(root)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}

// TestPortAllocator_ConcurrentSameWorkspace asserts the already-allocated
// branch is idempotent: many goroutines racing to allocate the SAME workspace
// must all observe one identical base port.
func TestPortAllocator_ConcurrentSameWorkspace(t *testing.T) {
	const n = 50
	p := NewPortAllocator(6200, 10)

	results := make([]int, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			results[i] = allocOrErr(p, "/shared")
		}(i)
	}
	wg.Wait()

	want := results[0]
	for i, got := range results {
		if got != want {
			t.Errorf("goroutine %d got base %d, want identical base %d", i, got, want)
		}
	}
	// Only one range should have been consumed for the single workspace.
	if base, ok := p.GetPort("/shared"); !ok || base != want {
		t.Errorf("GetPort(/shared) = (%d, %v), want (%d, true)", base, ok, want)
	}
}

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

// TestPortAllocator_DurableUnsavedWorkspaceRefused pins the fail-closed
// identity contract: with no persisted store key there is no durable key —
// refusing the spawn beats silently keying on a drifting root path.
func TestPortAllocator_DurableUnsavedWorkspaceRefused(t *testing.T) {
	home := t.TempDir()
	p := durableAllocator(t, home, 6200, 10)
	ws := &data.Workspace{Name: "unsaved", Repo: t.TempDir(), Root: t.TempDir()}

	if _, _, err := p.ReserveWorkspace(ws); !errors.Is(err, ErrWorkspaceMetadataNotPersisted) {
		t.Fatalf("ReserveWorkspace(unsaved) error = %v, want ErrWorkspaceMetadataNotPersisted", err)
	}
	// The transient path is untouched: without a durable store the same
	// unsaved workspace still allocates by root.
	transient := NewPortAllocator(6200, 10)
	if _, _, err := transient.ReserveWorkspace(ws); err != nil {
		t.Fatalf("transient ReserveWorkspace(unsaved) = %v, want nil", err)
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
