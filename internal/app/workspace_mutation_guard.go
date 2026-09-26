package app

import (
	"github.com/andyrewlee/amux/internal/data"
)

// Thin App wrappers over workspaceLifecycleState's mutation-in-flight guard —
// one phase serializes delete, shelve, and restore and suppresses
// persistence/rescan for all of them. These exist so the workspace service can
// be wired to App methods in app_init.

func (a *App) markWorkspaceMutationInFlight(ws *data.Workspace, mutating bool) bool {
	if ws == nil {
		return false
	}
	// Set-wide atomic mark/probe — the identity set drifts when the worktree
	// dir appears/disappears (NormalizePath resolves symlinks only for
	// existing paths). A per-form loop would accept a request carrying the
	// post-drift form alongside a marked one, running two lifecycle ops
	// concurrently; the atomic form rejects when ANY form is in flight.
	return a.lifecycle.markMutatingWorkspaceIDs(ws, mutating)
}

func (a *App) isWorkspaceMutationInFlight(wsID string) bool {
	return a.lifecycle.isMutating(wsID)
}

// isWorkspaceMutationInFlightWS is the workspace-typed probe injected into
// the workspace service — it checks the full identity set plus the root
// bridge so a mark stamped under a pre-drift ID form is still found.
func (a *App) isWorkspaceMutationInFlightWS(ws *data.Workspace) bool {
	return a.lifecycle.isMutatingWorkspaceIDs(ws)
}

// runUnlessWorkspaceMutationInFlightWS is the set-wide guard counterpart —
// check and callback stay atomic under the phase lock.
func (a *App) runUnlessWorkspaceMutationInFlightWS(ws *data.Workspace, fn func()) bool {
	return a.lifecycle.runUnlessMutatingWorkspaceIDs(ws, fn)
}

func (a *App) snapshotMutatingWorkspaceIDs() map[string]bool {
	return a.lifecycle.snapshotMutating()
}

func (a *App) runUnlessWorkspaceMutationInFlight(wsID string, fn func()) bool {
	return a.lifecycle.runUnlessMutating(wsID, fn)
}
