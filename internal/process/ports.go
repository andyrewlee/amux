package process

import (
	"errors"
	"os"
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
//     on-disk registry BEFORE the env reaches a spawn — workspaces under
//     their persisted metadata ID, workspaces without one (and raw root-keyed
//     callers) under a `transient-<pid>-<rootHash>` hold that sweeps when
//     the owning process dies. Durable reservations are never released —
//     retention is the cross-instance ownership contract — while transient
//     holds release on owner death or workspace delete; the registry
//     serializes concurrent instances through its own lock.
type PortAllocator struct {
	mu        sync.Mutex
	portStart int
	rangeSize int
	// allocated maps workspace root -> the interval actually held. Entries
	// mirrored from the durable registry keep the stored interval's true end,
	// which may exceed base+rangeSize-1 for reservations minted under an
	// older configured width.
	allocated map[string]portRange
	freeBases []int
	nextPort  int
	durable   *data.PortReservationStore
}

// NewPortAllocator creates a new port allocator
func NewPortAllocator(start, rangeSize int) *PortAllocator {
	return &PortAllocator{
		portStart: start,
		rangeSize: rangeSize,
		allocated: make(map[string]portRange),
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
	// p.mu is held across the whole reserve: the avoid-set snapshot, the
	// registry transaction, and the local mirror must serialize against local
	// transient allocations or a degrade could race in and take the interval
	// the registry is about to mint.
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.durable == nil {
		iv, err := p.allocateLocked(ws.Root)
		if err != nil {
			return 0, 0, err
		}
		return iv.start, iv.end, nil
	}
	id, ok := ws.StoredID()
	if !ok {
		// No persisted metadata ID means no durable key — a transient store
		// error during load or a never-saved record. Degrade to the
		// root-keyed allocation path instead of blocking every spawn behind
		// an unactionable error; in durable mode that path still publishes
		// a process-scoped transient hold into the shared registry, so a
		// second instance's mint can never select the same interval. A
		// later successful metadata load restores the durable path under
		// the real stored ID.
		logging.Warn("workspace %q has no persisted metadata record; using transient port allocation", ws.Name)
		iv, err := p.allocateLocked(ws.Root)
		if err != nil {
			return 0, 0, err
		}
		return iv.start, iv.end, nil
	}
	// The registry's mint scan only avoids persisted intervals — locally held
	// allocations that predate the durable store (or a published transient
	// hold's in-memory mirror) are invisible to it, so they pass through as
	// explicit exclusions or the mint could re-issue a range this process
	// already handed out.
	base, end, err := p.durable.Reserve(string(id), p.portStart, p.rangeSize, p.avoidAllocatedLocked(ws.Root)...)
	if errors.Is(err, data.ErrPortReservationsExhausted) {
		return 0, 0, ErrPortRangeExhausted
	}
	if err != nil {
		return 0, 0, err
	}
	// Mirror the commit into the local map so the synchronous memory-only
	// getters (GetPort/PortAllocated) stay truthful for this instance's
	// reservations without disk I/O on the read path — and so the transient
	// allocator's used set covers the stored interval at its true width.
	p.allocated[ws.Root] = portRange{start: base, end: end}
	return base, end, nil
}

// avoidAllocatedLocked returns the locally held intervals the registry mint
// must skip beyond the persisted set — excluding the workspace's own prior
// interval (its re-reserve is idempotent on the persisted record). Call
// under p.mu.
func (p *PortAllocator) avoidAllocatedLocked(exceptRoot string) []data.PortReservationInterval {
	avoid := make([]data.PortReservationInterval, 0, len(p.allocated))
	for root, iv := range p.allocated {
		if root == exceptRoot {
			continue
		}
		avoid = append(avoid, data.PortReservationInterval{Start: iv.start, End: iv.end})
	}
	return avoid
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

// ReservedIntervals returns the durable registry's workspace-ID → interval
// map (a locked copy) for read-only enumeration. Transient mode reports
// (nil, nil): there is no cross-instance registry to enumerate. Real I/O —
// callers route it through an async fetch path. Transient holds are
// per-process and release only through owner death or workspace delete —
// never user reclamation — so they are filtered out of the enumeration.
func (p *PortAllocator) ReservedIntervals() (map[string]data.PortReservationInterval, error) {
	p.mu.Lock()
	durable := p.durable
	p.mu.Unlock()
	if durable == nil {
		return nil, nil
	}
	snap, err := durable.Snapshot()
	if err != nil {
		return nil, err
	}
	for id := range snap {
		if data.IsTransientReservationID(id) {
			delete(snap, id)
		}
	}
	return snap, nil
}

// ReleaseReservedIntervals deletes the named workspace-ID reservations from
// the durable registry, returning the intervals actually released. Transient
// mode has no shared registry: (nil, nil). Real I/O under the registry flock —
// callers route it through an async path like the snapshot read.
func (p *PortAllocator) ReleaseReservedIntervals(ids []string) (map[string]data.PortReservationInterval, error) {
	p.mu.Lock()
	durable := p.durable
	p.mu.Unlock()
	if durable == nil {
		return nil, nil
	}
	return durable.ReleaseMany(ids)
}

// AllocatePort allocates a port range for a workspace. It returns
// ErrPortRangeExhausted when no valid, non-overlapping range remains —
// callers surface it as a typed spawn failure rather than crashing a Cmd.
func (p *PortAllocator) AllocatePort(workspaceRoot string) (int, error) {
	iv, err := p.allocate(workspaceRoot)
	if err != nil {
		return 0, err
	}
	return iv.start, nil
}

// allocate returns the workspace's interval, minting one on first use. In
// durable mode the mint publishes a process-scoped transient hold into the
// shared registry — never a purely local pick other instances cannot see.
func (p *PortAllocator) allocate(workspaceRoot string) (portRange, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.allocateLocked(workspaceRoot)
}

func (p *PortAllocator) allocateLocked(workspaceRoot string) (portRange, error) {
	// Check if already allocated
	if iv, ok := p.allocated[workspaceRoot]; ok {
		return iv, nil
	}

	if p.durable != nil {
		// A root-only allocation has no durable identity, but a purely
		// local mint would be invisible to every other instance — a second
		// instance's durable reservation or degraded pick could select the
		// same interval and hand two workspaces the same ports. Publish
		// the hold under the process-scoped transient key instead; it
		// sweeps when this process dies and releases on workspace delete.
		base, end, err := p.durable.Reserve(
			data.TransientReservationKey(os.Getpid(), workspaceRoot),
			p.portStart, p.rangeSize, p.avoidAllocatedLocked(workspaceRoot)...,
		)
		if errors.Is(err, data.ErrPortReservationsExhausted) {
			return portRange{}, ErrPortRangeExhausted
		}
		if err != nil {
			return portRange{}, err
		}
		iv := portRange{start: base, end: end}
		p.allocated[workspaceRoot] = iv
		return iv, nil
	}

	base, err := p.nextAvailablePortLocked(p.localRangesLocked())
	if err != nil {
		return portRange{}, err
	}
	iv := portRange{start: base, end: base + p.rangeSize - 1}
	p.allocated[workspaceRoot] = iv
	return iv, nil
}

func (p *PortAllocator) nextAvailablePortLocked(used []portRange) (int, error) {
	for n := len(p.freeBases); n > 0; n = len(p.freeBases) {
		port := p.freeBases[n-1]
		p.freeBases = p.freeBases[:n-1]
		if p.rangeAvailable(port, used) {
			return port, nil
		}
	}

	// rangeAvailable subsumes rangeFits, and durable-mirrored intervals make
	// the frontier itself a possible collision — the fast path must prove the
	// candidate free, not merely in-range.
	if p.rangeAvailable(p.nextPort, used) {
		port := p.nextPort
		p.nextPort += p.rangeSize
		return port, nil
	}

	if p.rangeSize > 0 {
		for base := p.portStart; p.rangeFits(base); base += p.rangeSize {
			if p.rangeAvailable(base, used) {
				if base >= p.nextPort {
					p.nextPort = base + p.rangeSize
				}
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

// localRangesLocked returns the locally held intervals the in-memory
// allocator must avoid — transient allocations plus durable mirrors at
// their true stored ends. Consulted only when no durable store exists; a
// durable-mode mint goes through the registry, which sees every published
// interval on its own.
func (p *PortAllocator) localRangesLocked() []portRange {
	used := make([]portRange, 0, len(p.allocated))
	for _, iv := range p.allocated {
		used = append(used, iv)
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

	iv, ok := p.allocated[workspaceRoot]
	return iv.start, ok
}

// ReleasePort releases the port allocation for a workspace so the base can
// be reused — transient mode only. In durable mode it is a deliberate no-op
// for durable records: the registry's reservations outlive every consumer,
// so a release arriving from a delete, quit, crash, or a stale
// pending-release sweep can never make a surviving session's interval
// reusable by a different workspace. The one exception is a transient hold
// published by THIS process for a StoredID-less workspace — its sessions
// are torn down by the delete path, so its ports are genuinely free.
func (p *PortAllocator) ReleasePort(workspaceRoot string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.durable != nil {
		released, err := p.durable.ReleaseMany([]string{
			data.TransientReservationKey(os.Getpid(), workspaceRoot),
		})
		if err != nil {
			logging.Warn("transient port-hold release for %q failed: %v", workspaceRoot, err)
			return
		}
		if len(released) == 0 {
			return // durable mirror or never published — keep the no-op contract
		}
		delete(p.allocated, workspaceRoot)
		return
	}

	if iv, ok := p.allocated[workspaceRoot]; ok {
		p.freeBases = append(p.freeBases, iv.start)
	}
	delete(p.allocated, workspaceRoot)
}

// PortRange returns the port and interval end for a workspace — the held
// interval's true end, which for a mirrored durable reservation can exceed
// base+rangeSize-1.
func (p *PortAllocator) PortRange(workspaceRoot string) (port, rangeEnd int, err error) {
	iv, err := p.allocate(workspaceRoot)
	if err != nil {
		return 0, 0, err
	}
	return iv.start, iv.end, nil
}
