package process

import (
	"testing"
)

func TestPortAllocator_AllocatePort(t *testing.T) {
	p := NewPortAllocator(6200, 10)

	// First allocation
	port1 := p.AllocatePort("/worktree1")
	if port1 != 6200 {
		t.Errorf("First allocation = %d, want 6200", port1)
	}

	// Second allocation
	port2 := p.AllocatePort("/worktree2")
	if port2 != 6210 {
		t.Errorf("Second allocation = %d, want 6210", port2)
	}

	// Same worktree should return same port
	port1Again := p.AllocatePort("/worktree1")
	if port1Again != port1 {
		t.Errorf("Same worktree returned different port: %d != %d", port1Again, port1)
	}
}

func TestPortAllocator_GetPort(t *testing.T) {
	p := NewPortAllocator(6200, 10)

	// Before allocation
	_, ok := p.GetPort("/worktree1")
	if ok {
		t.Error("GetPort should return false for unallocated worktree")
	}

	// After allocation
	p.AllocatePort("/worktree1")
	port, ok := p.GetPort("/worktree1")
	if !ok {
		t.Error("GetPort should return true for allocated worktree")
	}
	if port != 6200 {
		t.Errorf("GetPort = %d, want 6200", port)
	}
}

func TestPortAllocator_ReleasePort(t *testing.T) {
	p := NewPortAllocator(6200, 10)

	p.AllocatePort("/worktree1")
	p.ReleasePort("/worktree1")

	_, ok := p.GetPort("/worktree1")
	if ok {
		t.Error("GetPort should return false after release")
	}
}

func TestPortAllocator_PortRange(t *testing.T) {
	p := NewPortAllocator(6200, 10)

	port, rangeEnd := p.PortRange("/worktree1")
	if port != 6200 {
		t.Errorf("port = %d, want 6200", port)
	}
	if rangeEnd != 6209 {
		t.Errorf("rangeEnd = %d, want 6209", rangeEnd)
	}
}

func TestPortAllocator_ConcurrentAccess(t *testing.T) {
	p := NewPortAllocator(6200, 10)

	// Run concurrent allocations
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(n int) {
			p.AllocatePort("/worktree" + string(rune('0'+n)))
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify no crashes occurred (mutex protection)
}
