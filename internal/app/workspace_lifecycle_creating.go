package app

import "github.com/andyrewlee/amux/internal/data"

// markCreatingWorkspace is markCreating plus the root bridge stamp — the
// wsID is computed while the worktree dir may not exist, so it can differ
// from every form the post-create workspace reproduces. The bridge lets
// clearCreatingWorkspace find that exact key.
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
	if !w.transitionLocked(wsID, lifecycleCreating) {
		return false
	}
	w.creatingRootID[root] = wsID
	return true
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
