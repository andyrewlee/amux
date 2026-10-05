package app

import "github.com/andyrewlee/amux/internal/data"

// markCreatingWorkspace is markCreating plus the root bridge stamp — the
// wsID is computed while the worktree dir may not exist, so it can differ
// from every form the post-create workspace reproduces. The bridge lets
// clearCreatingWorkspace find that exact key.
//
// The creating phase has exactly one operation owner: a second create under
// the same identity, or under a different ID whose root is already claimed by
// an in-flight create, is refused rather than admitted — two creates fighting
// over one root would orphan the first operation's identity bridge and let
// either result tear down the other's guard.
func (w *workspaceLifecycleState) markCreatingWorkspace(wsID, root string) bool {
	if root == "" {
		return w.markCreating(wsID)
	}
	w.phaseMu.Lock()
	defer w.phaseMu.Unlock()
	if w.phases == nil {
		w.phases = make(map[string]lifecyclePhase)
	}
	if w.creatingRootID == nil {
		w.creatingRootID = make(map[string]string)
	}
	if w.creatingInFlightLocked(wsID, root) {
		return false
	}
	if !w.transitionLocked(wsID, lifecycleCreating) {
		return false
	}
	w.creatingRootID[root] = wsID
	return true
}

// creatingInFlightLocked reports whether a create for wsID at root would
// collide with a create already in flight: the same identity already
// creating, or the root bridge claimed under a different ID by a live create.
// Caller holds phaseMu (read or write).
func (w *workspaceLifecycleState) creatingInFlightLocked(wsID, root string) bool {
	if wsID != "" && w.phases[wsID] == lifecycleCreating {
		return true
	}
	if root != "" {
		markedID := w.creatingRootID[root]
		if markedID != "" && markedID != wsID && w.phases[markedID] == lifecycleCreating {
			return true
		}
	}
	return false
}

// creatingInFlight is the Update-goroutine probe for distinguishing an
// admission refusal: a false markCreatingWorkspace means either an in-flight
// create (this probe) or an in-flight mutation (isMutating) blocked it.
func (w *workspaceLifecycleState) creatingInFlight(wsID, root string) bool {
	w.phaseMu.RLock()
	defer w.phaseMu.RUnlock()
	return w.creatingInFlightLocked(wsID, root)
}

// clearCreatingWorkspace settles the creating phase over the workspace's
// full identity set plus the root-bridged mark key — the symmetric release
// for markCreatingWorkspace, which can stamp a pre-drift ID the resulting
// workspace value no longer reproduces.
func (w *workspaceLifecycleState) clearCreatingWorkspace(ws *data.Workspace) {
	if ws == nil {
		return
	}
	w.phaseMu.Lock()
	defer w.phaseMu.Unlock()
	for _, id := range data.WorkspaceIdentityStrings(ws) {
		if w.phases[id] == lifecycleCreating {
			delete(w.phases, id)
		}
	}
	if markedID := w.creatingRootID[ws.Root]; markedID != "" {
		if w.phases[markedID] == lifecycleCreating {
			delete(w.phases, markedID)
		}
		delete(w.creatingRootID, ws.Root)
	}
}
