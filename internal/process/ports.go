package process

import (
	"errors"
	"sync"
)

// ErrPortRangeExhausted reports that no valid, non-overlapping port range remains.
var ErrPortRangeExhausted = errors.New("port allocator exhausted")

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
	used := p.usedRangesLocked()
	for n := len(p.freeBases); n > 0; n = len(p.freeBases) {
		port := p.freeBases[n-1]
		p.freeBases = p.freeBases[:n-1]
		if p.rangeAvailable(port, used) {
			return port
		}
	}

	if p.rangeFits(p.nextPort) {
		port := p.nextPort
		p.nextPort += p.rangeSize
		return port
	}

	if p.rangeSize > 0 {
		for base := p.portStart; p.rangeFits(base); base += p.rangeSize {
			if p.rangeAvailable(base, used) {
				return base
			}
		}
		for base := 1; p.rangeFits(base); base++ {
			if p.rangeAvailable(base, used) {
				return base
			}
		}
	}
	panic(ErrPortRangeExhausted)
}

func (p *PortAllocator) rangeFits(base int) bool {
	const maxPort = 65535
	return p.rangeSize > 0 && base >= 1 && base <= maxPort && base+p.rangeSize-1 <= maxPort
}

func (p *PortAllocator) usedRangesLocked() []portRange {
	used := make([]portRange, 0, len(p.allocated))
	for _, base := range p.allocated {
		used = append(used, portRange{start: base, end: base + p.rangeSize - 1})
	}
	return used
}

func (p *PortAllocator) rangeAvailable(base int, used []portRange) bool {
	if !p.rangeFits(base) {
		return false
	}
	end := base + p.rangeSize - 1
	for _, existing := range used {
		if base <= existing.end && end >= existing.start {
			return false
		}
	}
	return true
}

type portRange struct {
	start int
	end   int
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
