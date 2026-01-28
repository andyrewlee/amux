package process

import (
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
	p := NewPortAllocator(6200, 10)

	// Run concurrent allocations
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(n int) {
			p.AllocatePort("/workspace" + string(rune('0'+n)))
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify no crashes occurred (mutex protection)
}
