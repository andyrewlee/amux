package process

import (
	"fmt"
	"sort"
	"sync"
	"testing"
)

func TestPortAllocator_AllocatePort(t *testing.T) {
	p := NewPortAllocator(6200, 10)

	// First allocation
	port1 := p.AllocatePort("/workspace1")
	if port1 != 6200 {
		t.Errorf("First allocation = %d, want 6200", port1)
	}

	// Second allocation
	port2 := p.AllocatePort("/workspace2")
	if port2 != 6210 {
		t.Errorf("Second allocation = %d, want 6210", port2)
	}

	// Same workspace should return same port
	port1Again := p.AllocatePort("/workspace1")
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
	p.AllocatePort("/workspace1")
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

	p.AllocatePort("/workspace1")
	p.ReleasePort("/workspace1")

	_, ok := p.GetPort("/workspace1")
	if ok {
		t.Error("GetPort should return false after release")
	}
}

func TestPortAllocator_PortRange(t *testing.T) {
	p := NewPortAllocator(6200, 10)

	port, rangeEnd := p.PortRange("/workspace1")
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
			bases[i] = p.AllocatePort(fmt.Sprintf("/ws%d", i))
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
			results[i] = p.AllocatePort("/shared")
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
