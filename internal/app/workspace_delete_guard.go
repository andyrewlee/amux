package app

import "github.com/andyrewlee/amux/internal/data"

// Thin App wrappers over workspaceLifecycleState's delete-in-flight guard;
// these exist so the workspace service can be wired to App methods in app_init.

func (a *App) markWorkspaceDeleteInFlight(ws *data.Workspace, deleting bool) bool {
	if ws == nil {
		return false
	}
	return a.lifecycle.markDeletingWorkspace(string(ws.ID()), ws.Root, deleting)
}

func (a *App) isWorkspaceDeleteInFlight(wsID string) bool {
	return a.lifecycle.isDeleting(wsID)
}

func (a *App) snapshotDeletingWorkspaceIDs() map[string]bool {
	return a.lifecycle.snapshotDeleting()
}

func (a *App) runUnlessWorkspaceDeleteInFlight(wsID string, fn func()) bool {
	return a.lifecycle.runUnlessDeleting(wsID, fn)
}
