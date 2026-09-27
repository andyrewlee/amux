package process

import (
	"errors"
	"sync"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
)

// ErrPortRangeExhausted reports that no valid, non-overlapping port range remains.
var ErrPortRangeExhausted = errors.New("port allocator exhausted")

// PortAllocator manages port allocation for workspaces. It runs in one of two
// modes:
//
//   - Transient (the default): an in-memory map keyed by workspace root, used
//     by tests and isolated callers. Released bases are reused.
//   - Durable (after SetDurableStore): every reservation commits to the shared
//     on-disk registry keyed by the workspace's persisted metadata ID BEFORE
//     the env reaches a spawn. Reservations are never released — retention is
//     the cross-instance ownership contract — and the registry serializes
//     concurrent instances through its own lock.
type PortAllocator struct {
	mu        sync.Mutex
	portStart int
	rangeSize int
	allocated map[string]int // workspace root -> port base (transient + durable local cache)
	freeBases []int
	nextPort  int
	durable   *data.PortReservationStore
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

// SetDurableStore installs the shared reservation registry the app layer owns.
// It must run before any environment provider built on this allocator is
// exposed — injecting it later would let early spawns draw transient ports a
// second instance cannot see.
func (p *PortAllocator) SetDurableStore(store *data.PortReservationStore) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.durable = store
}

// ReserveWorkspace returns the workspace's reserved port base and the
// interval's inclusive end, allocating on first use. Transient mode delegates
// to the root-keyed in-memory path; durable mode requires the workspace's
// persisted metadata ID and commits through the registry. The returned end is
// the interval's actual end — a record minted under an older configured width
// keeps its original bounds.
func (p *PortAllocator) ReserveWorkspace(ws *data.Workspace) (port, rangeEnd int, err error) {
	if ws == nil {
		return 0, 0, errors.New("workspace is required")
	}
	p.mu.Lock()
	durable := p.durable
	p.mu.Unlock()
	if durable == nil {
		return p.PortRange(ws.Root)
	}
	id, ok := ws.StoredID()
	if !ok {
		// No persisted metadata ID means no durable key — a transient store
		// error during load or a never-saved record. Degrade to the in-memory
		// allocator (the pre-registry contract) instead of blocking every
		// spawn behind an unactionable error: this process's map cannot
		// contradict a live registry record because the registry never handed
		// this workspace an interval. A later successful metadata load
		// restores the durable path under the real stored ID.
		logging.Warn("workspace %q has no persisted metadata record; using transient port allocation", ws.Name)
		return p.PortRange(ws.Root)
	}
	base, end, err := durable.Reserve(string(id), p.portStart, p.rangeSize)
	if errors.Is(err, data.ErrPortReservationsExhausted) {
		return 0, 0, ErrPortRangeExhausted
	}
	if err != nil {
		return 0, 0, err
	}
	// Mirror the commit into the local map so the synchronous memory-only
	// getters (GetPort/PortAllocated) stay truthful for this instance's
	// reservations without disk I/O on the read path.
	p.mu.Lock()
	if p.durable != nil {
		p.allocated[ws.Root] = base
	}
	p.mu.Unlock()
	return base, end, nil
}

// LookupWorkspaceInterval reports the workspace's reserved interval without
// allocating one. Durable mode reads the registry — real I/O, which is why it
// lives on the async status fetch rather than the synchronous getters — and an
// unsaved workspace reports whatever the transient fallback allocated (or
// not-found before it spawns). Transient mode reads the in-memory map.
func (p *PortAllocator) LookupWorkspaceInterval(ws *data.Workspace) (base, end int, found bool, err error) {
	if ws == nil {
		return 0, 0, false, nil
	}
	p.mu.Lock()
	durable := p.durable
	p.mu.Unlock()
	if durable == nil {
		if port, ok := p.GetPort(ws.Root); ok {
			return port, port + p.rangeSize - 1, true, nil
		}
		return 0, 0, false, nil
	}
	id, ok := ws.StoredID()
	if !ok {
		if port, found := p.GetPort(ws.Root); found {
			return port, port + p.rangeSize - 1, true, nil
		}
		return 0, 0, false, nil
	}
	return durable.Lookup(string(id))
}

// AllocatePort allocates a port range for a workspace. It returns
// ErrPortRangeExhausted when no valid, non-overlapping range remains —
// callers surface it as a typed spawn failure rather than crashing a Cmd.
func (p *PortAllocator) AllocatePort(workspaceRoot string) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if already allocated
	if port, ok := p.allocated[workspaceRoot]; ok {
		return port, nil
	}

	port, err := p.nextAvailablePortLocked()
	if err != nil {
		return 0, err
	}
	p.allocated[workspaceRoot] = port

	return port, nil
}

func (p *PortAllocator) nextAvailablePortLocked() (int, error) {
	used := p.usedRangesLocked()
	for n := len(p.freeBases); n > 0; n = len(p.freeBases) {
		port := p.freeBases[n-1]
		p.freeBases = p.freeBases[:n-1]
		if p.rangeAvailable(port, used) {
			return port, nil
		}
	}

	if p.rangeFits(p.nextPort) {
		port := p.nextPort
		p.nextPort += p.rangeSize
		return port, nil
	}

	if p.rangeSize > 0 {
		for base := p.portStart; p.rangeFits(base); base += p.rangeSize {
			if p.rangeAvailable(base, used) {
				return base, nil
			}
		}
	}
	return 0, ErrPortRangeExhausted
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

// ReleasePort releases the port allocation for a workspace so the base can be
// reused — transient mode only. In durable mode it is a deliberate no-op: the
// registry's reservations outlive every consumer, so a release arriving from
// a delete, quit, crash, or a stale pending-release sweep can never make a
// surviving session's interval reusable by a different workspace.
func (p *PortAllocator) ReleasePort(workspaceRoot string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.durable != nil {
		return
	}

	if base, ok := p.allocated[workspaceRoot]; ok {
		p.freeBases = append(p.freeBases, base)
	}
	delete(p.allocated, workspaceRoot)
}

// PortRange returns the port and range size for a workspace
func (p *PortAllocator) PortRange(workspaceRoot string) (port, rangeEnd int, err error) {
	port, err = p.AllocatePort(workspaceRoot)
	if err != nil {
		return 0, 0, err
	}
	return port, port + p.rangeSize - 1, nil
}
