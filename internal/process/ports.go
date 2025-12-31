package process

import (
	"sync"
)

// PortAllocator manages port allocation for worktrees
type PortAllocator struct {
	mu        sync.Mutex
	portStart int
	rangeSize int
	allocated map[string]int // worktree root -> port base
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

// AllocatePort allocates a port range for a worktree
func (p *PortAllocator) AllocatePort(worktreeRoot string) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if already allocated
	if port, ok := p.allocated[worktreeRoot]; ok {
		return port
	}

	// Allocate new port range
	port := p.nextPort
	p.allocated[worktreeRoot] = port
	p.nextPort += p.rangeSize

	return port
}

// GetPort returns the allocated port for a worktree
func (p *PortAllocator) GetPort(worktreeRoot string) (int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	port, ok := p.allocated[worktreeRoot]
	return port, ok
}

// ReleasePort releases the port allocation for a worktree
func (p *PortAllocator) ReleasePort(worktreeRoot string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.allocated, worktreeRoot)
}

// PortRange returns the port and range size for a worktree
func (p *PortAllocator) PortRange(worktreeRoot string) (port int, rangeEnd int) {
	port = p.AllocatePort(worktreeRoot)
	return port, port + p.rangeSize - 1
}
