package app

import "sync"

// workspaceLifecycleState groups the workspace create/delete/persist
// bookkeeping that previously lived as loose fields on App. The creating and
// dirty maps are touched only from App.Update handlers (single writer); the
// deleting map and local-save markers are also read from Cmd/worker
// goroutines, so they carry their own locks.
type workspaceLifecycleState struct {
	// creating tracks workspaces in the creation flow that have not been
	// loaded into the projects list yet.
	creating map[string]bool
	// deletingMu guards deleting; see markDeleting/isDeleting.
	deletingMu sync.RWMutex
	deleting   map[string]bool
	// dirty tracks workspaces with unsaved tab state (persist debounce).
	dirty map[string]bool
	// persistToken is the current persist-debounce generation.
	persistToken persistToken
	// projectsLoadToken is the next load generation to issue; lastApplied is
	// the highest applied, so handleProjectsLoaded can drop stale reloads.
	projectsLoadToken            projectsLoadToken
	lastAppliedProjectsLoadToken projectsLoadToken
	// localSaveMu guards localSavesAt (written from Cmd goroutines).
	localSaveMu  sync.Mutex
	localSavesAt map[string]localWorkspaceSaveMarker
}

func newWorkspaceLifecycleState() workspaceLifecycleState {
	return workspaceLifecycleState{
		creating:     make(map[string]bool),
		deleting:     make(map[string]bool),
		dirty:        make(map[string]bool),
		localSavesAt: make(map[string]localWorkspaceSaveMarker),
	}
}

// markCreating records a workspace as create-in-flight.
func (w *workspaceLifecycleState) markCreating(wsID string) {
	if wsID == "" {
		return
	}
	if w.creating == nil {
		w.creating = make(map[string]bool)
	}
	w.creating[wsID] = true
}

// clearCreating removes a workspace from the create-in-flight set.
func (w *workspaceLifecycleState) clearCreating(wsID string) {
	delete(w.creating, wsID)
}

// markDirty records a workspace as having unsaved tab state.
func (w *workspaceLifecycleState) markDirty(wsID string) {
	if wsID == "" {
		return
	}
	if w.dirty == nil {
		w.dirty = make(map[string]bool)
	}
	w.dirty[wsID] = true
}

// markDeleting sets or clears the delete-in-flight flag for a workspace.
func (w *workspaceLifecycleState) markDeleting(wsID string, deleting bool) {
	w.deletingMu.Lock()
	defer w.deletingMu.Unlock()

	if wsID == "" {
		return
	}
	if w.deleting == nil {
		w.deleting = make(map[string]bool)
	}
	if deleting {
		w.deleting[wsID] = true
		return
	}
	delete(w.deleting, wsID)
}

// isDeleting reports whether a workspace is currently delete-in-flight.
func (w *workspaceLifecycleState) isDeleting(wsID string) bool {
	w.deletingMu.RLock()
	defer w.deletingMu.RUnlock()

	if wsID == "" || w.deleting == nil {
		return false
	}
	return w.deleting[wsID]
}

// snapshotDeleting returns a copy of the IDs currently marked
// delete-in-flight. The RLock is required because callers like
// collectKnownWorkspaceIDs run on the Update goroutine while the map is also
// mutated from worker goroutines.
func (w *workspaceLifecycleState) snapshotDeleting() map[string]bool {
	w.deletingMu.RLock()
	defer w.deletingMu.RUnlock()

	if len(w.deleting) == 0 {
		return nil
	}
	out := make(map[string]bool, len(w.deleting))
	for id := range w.deleting {
		out[id] = true
	}
	return out
}

// runUnlessDeleting runs fn while holding the shared delete-state lock only
// when wsID is not currently marked delete-in-flight. Holding the lock across
// fn keeps the check and side effect atomic with respect to markDeleting.
func (w *workspaceLifecycleState) runUnlessDeleting(wsID string, fn func()) bool {
	w.deletingMu.RLock()
	defer w.deletingMu.RUnlock()

	if wsID == "" || w.deleting[wsID] {
		return false
	}
	if fn != nil {
		fn()
	}
	return true
}
