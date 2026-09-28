package app

import (
	"github.com/andyrewlee/amux/internal/data"
)

// workspaceLifecycleState's workspace-typed, identity-set-wide operations.
// A workspace's ComputedID drifts mid-mutation: NormalizePath resolves
// symlinks only for existing paths, so the worktree dir appearing (restore)
// or vanishing (shelve/delete) mints a different path-hash while the op is
// still in flight. Every method here therefore spans the full identity set
// plus the root bridge under a single lock hold — per-form probing would let
// a request carrying a partially drifted set slip past an in-flight mark.

// markMutatingWorkspaceIDs is the atomic identity-set form of
// markMutatingWorkspace: reject when ANY identity form or the root bridge is
// already mutating, and mark all-or-nothing (a failed transition rolls the
// partial mark back). A per-form mark loop would accept a request carrying
// the post-drift ComputedID alongside a marked storeID — running two
// worktree mutations concurrently.
func (w *workspaceLifecycleState) markMutatingWorkspaceIDs(ws *data.Workspace, mutating bool) bool {
	if ws == nil {
		return false
	}
	ids := data.WorkspaceIdentityStrings(ws)
	w.phaseMu.Lock()
	defer w.phaseMu.Unlock()
	if w.phases == nil {
		w.phases = make(map[string]lifecyclePhase)
	}
	if w.mutatingRootID == nil {
		w.mutatingRootID = make(map[string]string)
	}
	if !mutating {
		if markedID := w.mutatingRootID[ws.Root]; markedID != "" {
			delete(w.phases, markedID)
			delete(w.mutatingRootID, ws.Root)
		}
		for _, id := range ids {
			if w.phases[id] == lifecycleMutating {
				delete(w.phases, id)
			}
		}
		return true
	}
	for _, id := range ids {
		if id != "" && w.phases[id] == lifecycleMutating {
			return false
		}
	}
	if ws.Root != "" {
		if markedID := w.mutatingRootID[ws.Root]; markedID != "" && w.phases[markedID] == lifecycleMutating {
			return false
		}
	}
	var marked []string
	for _, id := range ids {
		if id == "" {
			continue
		}
		if !w.transitionLocked(id, lifecycleMutating) {
			for _, m := range marked {
				if w.phases[m] == lifecycleMutating {
					delete(w.phases, m)
				}
			}
			return false
		}
		marked = append(marked, id)
	}
	if len(marked) > 0 && ws.Root != "" {
		w.mutatingRootID[ws.Root] = marked[len(marked)-1]
	}
	return len(marked) > 0
}

func (w *workspaceLifecycleState) isMutatingWorkspace(wsID, root string) bool {
	if wsID == "" && root == "" {
		return false
	}
	w.phaseMu.RLock()
	defer w.phaseMu.RUnlock()
	return w.isMutatingLocked(wsID, root)
}

// isMutatingWorkspaceIDs probes the workspace's FULL identity set plus the
// root bridge — a mutation marked under any form (pre/post-drift ComputedID,
// MetadataID, storeID) is found regardless of which form this workspace
// value resolves to right now.
func (w *workspaceLifecycleState) isMutatingWorkspaceIDs(ws *data.Workspace) bool {
	if ws == nil {
		return false
	}
	w.phaseMu.RLock()
	defer w.phaseMu.RUnlock()
	for _, id := range data.WorkspaceIdentityStrings(ws) {
		if w.isMutatingLocked(id, "") {
			return true
		}
	}
	return w.isMutatingLocked("", ws.Root)
}

// runUnlessMutatingWorkspaceIDs is the set-wide counterpart of
// runUnlessMutating: the check and fn run under the same phaseMu hold, so a
// mutation marked mid-flight between the probe and the store write can't
// slip past. Used by the service's rescan/import guards.
func (w *workspaceLifecycleState) runUnlessMutatingWorkspaceIDs(ws *data.Workspace, fn func()) bool {
	if ws == nil {
		return false
	}
	w.phaseMu.RLock()
	defer w.phaseMu.RUnlock()
	for _, id := range data.WorkspaceIdentityStrings(ws) {
		if w.isMutatingLocked(id, "") {
			return false
		}
	}
	if w.isMutatingLocked("", ws.Root) {
		return false
	}
	if fn != nil {
		fn()
	}
	return true
}
