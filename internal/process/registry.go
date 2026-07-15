package process

import (
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/andyrewlee/amux/internal/fsatomic"
)

// ServiceRecord is the durable identity of one managed service process group:
// enough to find and stop it from a later amux process, without the worktree
// directory existing. StartedAt (with PID/PGID) is the identity: a recycled
// PID has a different start time, and it survives the shell's exec
// optimization (`sh -c cmd` replaces its own image, changing the command line
// ps reports, but never the start time).
type ServiceRecord struct {
	WorkspaceRoot string    `json:"workspace_root"`
	PID           int       `json:"pid"`
	PGID          int       `json:"pgid"`
	Command       string    `json:"command"`
	StartedAt     time.Time `json:"started_at"`
}

// startTimeTolerance absorbs the skew between Record's time.Now() (taken just
// after spawn) and the kernel's process start time as ps reports it.
const startTimeTolerance = 10 * time.Second

// Matches reports whether the record still identifies a live process in snap:
// same PID and process group, plus identity. When both start times are known
// they decide alone — command lines change across exec, and an equal generic
// command ("sh -c pnpm run dev") on a recycled PID must NOT match. Only when
// a start time is missing does the command-line comparison decide.
func (r ServiceRecord) Matches(snap []ProcessInfo) bool {
	for _, p := range snap {
		if p.PID != r.PID {
			continue
		}
		if p.PGID != r.PGID {
			return false
		}
		if !r.StartedAt.IsZero() && !p.StartedAt.IsZero() {
			delta := p.StartedAt.Sub(r.StartedAt)
			if delta < 0 {
				delta = -delta
			}
			return delta <= startTimeTolerance
		}
		return p.Command == r.Command
	}
	return false
}

// ServiceRegistry persists ServiceRecords to a single JSON file so managed
// services survive amux restarts as known, stoppable entities instead of
// untracked orphans. All mutations rewrite the file atomically; a missing or
// corrupt file degrades to an empty registry (never blocks startup).
type ServiceRegistry struct {
	mu      sync.Mutex
	path    string
	records map[string]ServiceRecord // workspace key -> record
}

// NewServiceRegistry loads (or lazily creates) the registry at path.
func NewServiceRegistry(path string) *ServiceRegistry {
	reg := &ServiceRegistry{path: path, records: make(map[string]ServiceRecord)}
	data, err := os.ReadFile(path)
	if err != nil {
		return reg
	}
	var records map[string]ServiceRecord
	if json.Unmarshal(data, &records) == nil && records != nil {
		reg.records = records
	}
	return reg
}

// Record stores rec under its workspace root, replacing any prior entry.
func (g *ServiceRegistry) Record(rec ServiceRecord) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.records[rec.WorkspaceRoot] = rec
	return g.persistLocked()
}

// Clear removes the entry for workspaceRoot when it refers to pid (pid 0
// clears unconditionally). A mismatched pid is left alone so a stale clear
// cannot drop a newer service's record.
func (g *ServiceRegistry) Clear(workspaceRoot string, pid int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	rec, ok := g.records[workspaceRoot]
	if !ok || (pid != 0 && rec.PID != pid) {
		return nil
	}
	delete(g.records, workspaceRoot)
	return g.persistLocked()
}

// Get returns the record for workspaceRoot, if any.
func (g *ServiceRegistry) Get(workspaceRoot string) (ServiceRecord, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	rec, ok := g.records[workspaceRoot]
	return rec, ok
}

// Records returns a copy of all entries.
func (g *ServiceRegistry) Records() []ServiceRecord {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]ServiceRecord, 0, len(g.records))
	for _, rec := range g.records {
		out = append(out, rec)
	}
	return out
}

// reconcileGrace protects records younger than this from being dropped by a
// non-matching reconcile: the snapshot is taken before Reconcile runs, so a
// service recorded while ps was executing is legitimately absent from it.
const reconcileGrace = 2 * time.Minute

// Reconcile drops entries whose process no longer matches its recorded
// identity and returns the records that are still live (including
// grace-period keeps). Call it at startup with a fresh Snapshot before
// trusting the registry for stops.
func (g *ServiceRegistry) Reconcile(snap []ProcessInfo) []ServiceRecord {
	g.mu.Lock()
	defer g.mu.Unlock()
	live := make([]ServiceRecord, 0, len(g.records))
	changed := false
	for key, rec := range g.records {
		if rec.Matches(snap) || time.Since(rec.StartedAt) < reconcileGrace {
			live = append(live, rec)
			continue
		}
		delete(g.records, key)
		changed = true
	}
	if changed {
		if err := g.persistLocked(); err != nil {
			slog.Debug("service registry reconcile persist failed", "error", err)
		}
	}
	return live
}

func (g *ServiceRegistry) persistLocked() error {
	return fsatomic.WriteJSON(g.path, g.records)
}
