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

	// Allocate new port range
	port := p.nextPort
	p.allocated[workspaceRoot] = port
	p.nextPort += p.rangeSize

	return port
}

// GetPort returns the allocated port for a workspace
func (p *PortAllocator) GetPort(workspaceRoot string) (int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	port, ok := p.allocated[workspaceRoot]
	return port, ok
}

// ReleasePort releases the port allocation for a workspace
func (p *PortAllocator) ReleasePort(workspaceRoot string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.allocated, workspaceRoot)
}

// PortRange returns the port and range size for a workspace
func (p *PortAllocator) PortRange(workspaceRoot string) (port, rangeEnd int) {
	port = p.AllocatePort(workspaceRoot)
	return port, port + p.rangeSize - 1
}
