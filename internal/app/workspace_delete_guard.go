package app

import "github.com/andyrewlee/amux/internal/data"

func (a *App) markWorkspaceDeleteInFlight(ws *data.Workspace, deleting bool) {
	a.deletingWorkspaceMu.Lock()
	defer a.deletingWorkspaceMu.Unlock()

	if ws == nil {
		return
	}
	wsID := string(ws.ID())
	if wsID == "" {
		return
	}
	if a.deletingWorkspaceIDs == nil {
		a.deletingWorkspaceIDs = make(map[string]bool)
	}
	if deleting {
		a.deletingWorkspaceIDs[wsID] = true
		return
	}
	delete(a.deletingWorkspaceIDs, wsID)
}

func (a *App) isWorkspaceDeleteInFlight(wsID string) bool {
	a.deletingWorkspaceMu.RLock()
	defer a.deletingWorkspaceMu.RUnlock()

	if wsID == "" || a.deletingWorkspaceIDs == nil {
		return false
	}
	return a.deletingWorkspaceIDs[wsID]
}

// snapshotDeletingWorkspaceIDs returns a copy of the IDs currently marked
// delete-in-flight. The RLock is required because callers like
// collectKnownWorkspaceIDs run on the Update goroutine while the map is also
// mutated from worker goroutines.
func (a *App) snapshotDeletingWorkspaceIDs() map[string]bool {
	a.deletingWorkspaceMu.RLock()
	defer a.deletingWorkspaceMu.RUnlock()

	if len(a.deletingWorkspaceIDs) == 0 {
		return nil
	}
	out := make(map[string]bool, len(a.deletingWorkspaceIDs))
	for id := range a.deletingWorkspaceIDs {
		out[id] = true
	}
	return out
}

// runUnlessWorkspaceDeleteInFlight runs fn while holding a shared delete-state
// lock only when wsID is not currently marked delete-in-flight. Holding the
// lock across fn keeps the check and side effect atomic with respect to
// markWorkspaceDeleteInFlight updates.
func (a *App) runUnlessWorkspaceDeleteInFlight(wsID string, fn func()) bool {
	a.deletingWorkspaceMu.RLock()
	defer a.deletingWorkspaceMu.RUnlock()

	if wsID == "" || a.deletingWorkspaceIDs[wsID] {
		return false
	}
	if fn != nil {
		fn()
	}
	return true
}
