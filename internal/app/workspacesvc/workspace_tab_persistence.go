package workspacesvc

import (
	"errors"
	"io/fs"
	"slices"
	"sync"

	"github.com/andyrewlee/amux/internal/data"
)

// Ordered, narrow persistence of the UI-owned workspace fields
// (OpenTabs/ActiveTabIndex).
//
// The dashboard debounces tab captures into commands that run whenever the
// pump schedules them — a whole-Workspace save at that point would write the
// captured record's every field, resurrecting stale values over a concurrent
// rename/env/script edit. Two queued captures can also execute in reverse
// order. This coordinator fixes both: writes go through store.Update so only
// the tab fields change on disk, and a monotonic capture sequence makes the
// newest accepted capture win regardless of command execution order.
//
// A sequence at or below the last committed one is a benign superseded
// result (committed=false, nil error) — the newer state is already durable.
// A failed newest write returns the error and does NOT mark the sequence
// committed, so the app's re-dirty path can retry it.

// errTabSaveSkipped is the sentinel the Update callback returns when the
// workspace is mid-mutation: the delete/shelve flow owns its metadata, and a
// tab write then could recreate a record being removed. Surfaced to callers
// as a benign committed=false — the lifecycle's own requeue path re-dirties
// the workspace when the mutation resolves.
var errTabSaveSkipped = errors.New("tab save skipped: workspace mutation in flight")

type tabPersistence struct {
	mu        sync.Mutex
	locks     map[data.WorkspaceID]*sync.Mutex
	committed map[data.WorkspaceID]uint64
}

func newTabPersistence() *tabPersistence {
	return &tabPersistence{
		locks:     map[data.WorkspaceID]*sync.Mutex{},
		committed: map[data.WorkspaceID]uint64{},
	}
}

// writeLock returns the per-workspace-ID mutex serializing tab writes. Per-ID
// (not one global) so a slow write for one workspace never stalls another's.
func (t *tabPersistence) writeLock(id data.WorkspaceID) *sync.Mutex {
	t.mu.Lock()
	defer t.mu.Unlock()
	l, ok := t.locks[id]
	if !ok {
		l = &sync.Mutex{}
		t.locks[id] = l
	}
	return l
}

// committedSeq reports the newest sequence durably written for id.
func (t *tabPersistence) committedSeq(id data.WorkspaceID) uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.committed[id]
}

// markCommitted records seq as durably persisted for id. Callers hold the
// per-ID write lock and have a successful (or superseded-identical) write.
func (t *tabPersistence) markCommitted(id data.WorkspaceID, seq uint64) {
	t.mu.Lock()
	t.committed[id] = seq
	t.mu.Unlock()
}

// SaveWorkspaceTabs persists one tab-state capture as a narrow field
// transaction: only OpenTabs and ActiveTabIndex change on disk; every other
// field is read fresh inside the workspace lock. seq is the capture's
// monotonic sequence; a call carrying a sequence at or below the last
// committed one returns committed=false — the record already reflects a
// newer capture. The mutation-in-flight check runs inside the Update
// callback so it is atomic with the write.
//
// ws is the caller's workspace snapshot, used only to resolve the record ID
// and — when no record exists yet — as the body of an intentional create:
// persisting a workspace with no metadata is creation, not a field update.
func (s *Service) SaveWorkspaceTabs(ws *data.Workspace, seq uint64, tabs []data.TabInfo, activeIdx int) (bool, error) {
	if s == nil || s.store == nil || ws == nil {
		return false, nil
	}
	id := ws.MetadataID()
	if id == "" {
		return false, nil
	}
	lock := s.tabPersist.writeLock(id)
	lock.Lock()
	defer lock.Unlock()

	if seq <= s.tabPersist.committedSeq(id) {
		// Superseded: a newer capture already committed. Not an error, not
		// a write, and deliberately no local-save marker — nothing happened.
		return false, nil
	}
	err := s.store.Update(id, func(fresh *data.Workspace) (bool, error) {
		if s.isMutationInFlight(fresh) {
			return false, errTabSaveSkipped
		}
		cloned := slices.Clone(tabs)
		if slices.Equal(fresh.OpenTabs, cloned) && fresh.ActiveTabIndex == activeIdx {
			return false, nil
		}
		fresh.OpenTabs = cloned
		fresh.ActiveTabIndex = activeIdx
		return true, nil
	})
	if errors.Is(err, errTabSaveSkipped) {
		return false, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		if s.isMutationInFlight(ws) {
			return false, nil
		}
		// No record to apply the field write to — the workspace exists only
		// in memory. Write the captured record whole, once; this is the
		// create path, not a stale-snapshot resurrection.
		full := *ws
		full.OpenTabs = slices.Clone(tabs)
		full.ActiveTabIndex = activeIdx
		err = s.store.Save(&full)
	}
	if err != nil {
		return false, err
	}
	s.tabPersist.markCommitted(id, seq)
	return true, nil
}
