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
// field is read fresh inside the workspace lock. The bool reports whether
// bytes were committed: a call carrying a sequence at or below the last
// committed one returns false (the record already reflects a newer capture),
// and so does a transaction the store accepts as a no-op — the fresh record
// already held these tabs. Callers that fingerprint the file for self-write
// suppression rely on this: false means "we wrote nothing."
//
// The mutation-in-flight guard wraps the ENTIRE ordered save once, at this
// boundary: when an atomic guard is installed, the phase check and the write
// are atomic with the lifecycle mutation transition — and nothing inside
// may re-enter the predicate, because a writer queued between two read
// acquisitions would deadlock the lifecycle RWMutex (the first hold waits
// on the second, the writer waits on the first). With no atomic guard the
// predicate-only compatibility remains: checked inside the Update callback
// and again before the missing-record create.
//
// Lock order: workspace lifecycle guard → per-ID tab write lock → store
// transaction. No caller may wrap this method in the lifecycle guard.
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
	if s.mutationInFlightGuard != nil {
		wrote := false
		var saveErr error
		ran := s.mutationInFlightGuard(ws, func() {
			wrote, saveErr = s.saveWorkspaceTabs(id, ws, seq, tabs, activeIdx, false)
		})
		if !ran {
			// Mutation began between call and admission — nothing was
			// written and nothing failed; the mutation's own resolution
			// path requeues.
			return false, nil
		}
		return wrote, saveErr
	}
	return s.saveWorkspaceTabs(id, ws, seq, tabs, activeIdx, true)
}

// saveWorkspaceTabs is the ordered write proper: per-ID write lock,
// sequence check, narrow Update, missing-record whole-create fallback,
// sequence commit. checkMutation selects the predicate probes inside the
// transaction and before the fallback — required in predicate-only mode,
// forbidden while an atomic guard is already held.
func (s *Service) saveWorkspaceTabs(id data.WorkspaceID, ws *data.Workspace, seq uint64, tabs []data.TabInfo, activeIdx int, checkMutation bool) (bool, error) {
	lock := s.tabPersist.writeLock(id)
	lock.Lock()
	defer lock.Unlock()

	if seq <= s.tabPersist.committedSeq(id) {
		// Superseded: a newer capture already committed. Not an error, not
		// a write, and deliberately no local-save marker — nothing happened.
		return false, nil
	}
	// committed tracks the callback's changed flag: a no-op Update (the
	// fresh record already held these tabs) writes nothing, and the caller
	// must not fingerprint the current file as ours — an external write
	// committed before our no-op would otherwise be suppressed as local.
	committed := false
	err := s.store.Update(id, func(fresh *data.Workspace) (bool, error) {
		if checkMutation && s.isMutationInFlight(fresh) {
			return false, errTabSaveSkipped
		}
		cloned := slices.Clone(tabs)
		if slices.Equal(fresh.OpenTabs, cloned) && fresh.ActiveTabIndex == activeIdx {
			return false, nil
		}
		fresh.OpenTabs = cloned
		fresh.ActiveTabIndex = activeIdx
		committed = true
		return true, nil
	})
	if errors.Is(err, errTabSaveSkipped) {
		return false, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		if checkMutation && s.isMutationInFlight(ws) {
			return false, nil
		}
		// No record to apply the field write to — the workspace exists only
		// in memory. Write the captured record whole, once; this is the
		// create path, not a stale-snapshot resurrection.
		full := *ws
		full.OpenTabs = slices.Clone(tabs)
		full.ActiveTabIndex = activeIdx
		err = s.store.Save(&full)
		committed = err == nil
	}
	if err != nil {
		return false, err
	}
	// The seq ledger is about ordering, not bytes: a no-op still consumed
	// this capture, so it commits the sequence either way. Only a real
	// write reports wrote=true — the marker contract is "we committed".
	s.tabPersist.markCommitted(id, seq)
	return committed, nil
}
