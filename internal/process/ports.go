package process

import (
	"sync"
)

// PortAllocator manages port allocation for workspaces
type PortAllocator struct {
	mu        sync.Mutex
	portStart int
	rangeSize int
	allocated map[string]int // workspace root -> port base
	freeBases []int
	nextPort  int
}

// NewPortAllocator creates a new port allocator
func NewPortAllocator(start, rangeSize int) *PortAllocator {
	return &PortAllocator{
		portStart: start,
		rangeSize: rangeSize,
		allocated: make(map[string]int),
		nextPort:  start,
	}
}

// AllocatePort allocates a port range for a workspace
func (p *PortAllocator) AllocatePort(workspaceRoot string) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if already allocated
	if port, ok := p.allocated[workspaceRoot]; ok {
		return port
	}

	port := p.nextAvailablePortLocked()
	p.allocated[workspaceRoot] = port

	return port
}

func (p *PortAllocator) nextAvailablePortLocked() int {
	if n := len(p.freeBases); n > 0 {
		port := p.freeBases[n-1]
		p.freeBases = p.freeBases[:n-1]
		return port
	}

	if p.rangeFits(p.nextPort) {
		port := p.nextPort
		p.nextPort += p.rangeSize
		return port
	}

	if p.rangeSize > 0 {
		used := make(map[int]bool, len(p.allocated))
		for _, base := range p.allocated {
			used[base] = true
		}
		for base := p.portStart; p.rangeFits(base); base += p.rangeSize {
			if !used[base] {
				return base
			}
		}
	}
	return p.portStart
}

func (p *PortAllocator) rangeFits(base int) bool {
	const maxPort = 65535
	return p.rangeSize > 0 && base <= maxPort && base+p.rangeSize-1 <= maxPort
}

// GetPort returns the allocated port for a workspace
func (p *PortAllocator) GetPort(workspaceRoot string) (int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	port, ok := p.allocated[workspaceRoot]
	return port, ok
}

// ReleasePort releases the port allocation for a workspace so the base can be reused.
func (p *PortAllocator) ReleasePort(workspaceRoot string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if base, ok := p.allocated[workspaceRoot]; ok {
		p.freeBases = append(p.freeBases, base)
	}
	delete(p.allocated, workspaceRoot)
}

// PortRange returns the port and range size for a workspace
func (p *PortAllocator) PortRange(workspaceRoot string) (port, rangeEnd int) {
	port = p.AllocatePort(workspaceRoot)
	return port, port + p.rangeSize - 1
}
