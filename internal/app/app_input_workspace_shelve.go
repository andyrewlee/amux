package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

// handleShelveWorkspace handles the ShelveWorkspace message. It mirrors
// handleDeleteWorkspace's prelude — the shared mutation-in-flight guard
// serializes the worktree removal and suppresses rescan/persistence, and the
// dashboard "shelving" spinner marks the in-progress op — but the service op
// keeps branch + metadata instead of removing them.
func (a *App) handleShelveWorkspace(msg messages.ShelveWorkspace) []tea.Cmd {
	var cmds []tea.Cmd
	if msg.Project == nil || msg.Workspace == nil {
		logging.Warn("ShelveWorkspace received with nil project or workspace")
		return nil
	}
	msg.Workspace = snapshotWorkspaceForSave(msg.Workspace)
	if !a.markWorkspaceMutationInFlight(msg.Workspace, true) {
		logging.Warn("ShelveWorkspace rejected while workspace %s is in another lifecycle phase", msg.Workspace.ID())
		return nil
	}
	a.lifecycle.clearCreatedProjectLoadBarrier(string(msg.Workspace.ID()), msg.Workspace.Root)
	// A pending auto-launch must not fire an agent into a worktree that is
	// about to vanish — same disarm as delete.
	a.clearPendingAgentLaunchFor(msg.Workspace)
	if cmd := a.dashboard.SetWorkspaceBusy(msg.Workspace.Root, dashboard.WorkspaceOpShelve, true); cmd != nil {
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, a.shelveWorkspace(msg.Project, msg.Workspace))
	return cmds
}

// handleWorkspaceShelved completes a confirmed shelve. The worktree-specific
// cleanup mirrors the confirmed-delete path (sessions are already reaped by
// the service op; watches, status cache, and the port allocation release
// here), except navigation-home still applies: the user was inside a
// worktree that no longer exists.
func (a *App) handleWorkspaceShelved(msg messages.WorkspaceShelved) []tea.Cmd {
	var cmds []tea.Cmd
	if msg.Warning != "" {
		cmds = append(cmds, a.toast.ShowWarning(msg.Warning))
	}
	if msg.Workspace != nil {
		cmds = append(cmds, a.finishWorkspaceShelved(msg.Workspace, msg.WorkspaceIDs)...)
		// Same teardown delete performs: shelve kills the workspace's tmux
		// sessions, so center/sidebar tabs and agents keyed to them must come
		// down too — otherwise they resurface stale on restore and a late
		// PtyTabCreateResult can file a live orphan the GC skips.
		newCenter, cmd := a.center.Update(msg)
		a.center = newCenter
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		newTerminal, cmd := a.sidebarTerminal.Update(msg)
		a.sidebarTerminal = newTerminal
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	cmds = append(cmds, a.loadProjects())
	// A bulk batch member's completion advances the queue to the next
	// workspace; foreign shelve completions leave the batch untouched.
	if cmd := a.bulkFinished(msg.Workspace, true); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return cmds
}

// finishWorkspaceShelved runs the post-shelve cleanup for a workspace whose
// worktree is gone: tombstone the row until the next projects load, drop all
// ID-keyed state under the stamped pre-removal identity set (ws.ID() drifts
// across the removal — NormalizePath only resolves existing paths), release
// the port, and drop the loaded project entry.
func (a *App) finishWorkspaceShelved(ws *data.Workspace, stampedIDs []string) []tea.Cmd {
	var cmds []tea.Cmd
	for _, id := range workspacesvc.WorkspaceIDsOrComputed(ws, stampedIDs) {
		a.lifecycle.markDeletedUntilProjectsLoad(id, ws.Root, a.lifecycle.projectsLoadToken)
		delete(a.tmuxActivity.activeWorkspaceIDs, id)
		delete(a.lifecycle.dirty, id)
	}
	a.markWorkspaceMutationInFlight(ws, false)
	if cmd := a.syncActiveWorkspacesToDashboard(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if a.activeWorkspace != nil && a.activeWorkspace.Root == ws.Root {
		a.goHome()
	}
	if cmd := a.dashboard.SetWorkspaceBusy(ws.Root, dashboard.WorkspaceOpShelve, false); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if a.gitStatus != nil {
		a.gitStatus.Invalidate(ws.Root)
	}
	if a.fileWatcher != nil {
		a.fileWatcher.Unwatch(ws.Root)
	}
	if a.workspaceService != nil {
		a.workspaceService.ReleaseWorkspacePort(ws)
	}
	a.removeWorkspaceFromLoadedProjects(ws)
	if a.dashboard != nil {
		a.dashboard.SetProjects(a.projects)
	}
	// Per-row toasts are suppressed while a bulk shelve is draining — the
	// batch emits one summary toast at the end instead of N row toasts.
	if !a.bulk.activeFor(bulkOpShelve) {
		if cmd := a.toast.ShowSuccess(fmt.Sprintf("Shelved %s — branch and settings kept", ws.Name)); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return cmds
}

// handleWorkspaceShelveFailed surfaces a shelve failure: the in-flight flag
// and spinner come down, and when the worktree still exists the intent flag
// the service saved is rolled back there too (service-side). Nothing else
// needs cleanup — shelve sets no tombstone and never deletes metadata.
func (a *App) handleWorkspaceShelveFailed(msg messages.WorkspaceShelveFailed) tea.Cmd {
	var cmds []tea.Cmd
	if msg.Workspace != nil {
		a.markWorkspaceMutationInFlight(msg.Workspace, false)
		if cmd := a.dashboard.SetWorkspaceBusy(msg.Workspace.Root, dashboard.WorkspaceOpShelve, false); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := a.loadProjects(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		// A debounced tab save collected before this op may have been
		// skipped mid-flight by the lifecycle guard — re-dirty so the
		// pending state isn't lost (the delete path already requeues).
		if cmd := a.persistWorkspaceTabs(string(msg.Workspace.ID())); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if errCmd := common.ReportError(errorContext(errorServiceWorkspace, "shelving workspace"), msg.Err, ""); errCmd != nil {
		cmds = append(cmds, errCmd)
	}
	if cmd := a.bulkFinished(msg.Workspace, false); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return common.SafeBatch(cmds...)
}

// handleRestoreWorkspace starts a shelved workspace's restore. It takes the
// shared mutation-in-flight guard: restore is a lifecycle mutation too, and
// without the guard a second Enter (or a shelve/purge landing mid-restore)
// would run a concurrent worktree add/remove against the same root.
func (a *App) handleRestoreWorkspace(msg messages.RestoreWorkspace) []tea.Cmd {
	if msg.Project == nil || msg.Workspace == nil {
		logging.Warn("RestoreWorkspace received with nil project or workspace")
		return nil
	}
	msg.Workspace = snapshotWorkspaceForSave(msg.Workspace)
	if !a.markWorkspaceMutationInFlight(msg.Workspace, true) {
		logging.Warn("RestoreWorkspace rejected while workspace %s is in another lifecycle phase", msg.Workspace.ID())
		return nil
	}
	var cmds []tea.Cmd
	if cmd := a.dashboard.SetWorkspaceBusy(msg.Workspace.Root, dashboard.WorkspaceOpRestore, true); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return append(cmds, a.restoreWorkspace(msg.Project, msg.Workspace))
}

// handleWorkspaceRestored completes a restore: the worktree exists again, so
// the setup script re-runs against it (a restore is a fresh worktree — same
// reasoning as create), then the projects reload re-surfaces the row as a
// normal workspace.
func (a *App) handleWorkspaceRestored(msg messages.WorkspaceRestored) []tea.Cmd {
	var cmds []tea.Cmd
	if msg.Workspace != nil {
		a.markWorkspaceMutationInFlight(msg.Workspace, false)
		if cmd := a.dashboard.SetWorkspaceBusy(msg.Workspace.Root, dashboard.WorkspaceOpRestore, false); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := a.runSetupAsync(msg.Workspace); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := a.restoredToastCmd(msg.Workspace); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	cmds = append(cmds, a.loadProjects())
	if cmd := a.bulkFinished(msg.Workspace, true); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return cmds
}

// handleWorkspaceRestoreSkipped completes a restore that found the record
// already live — a stale duplicate (a second Enter racing the first
// restore's completion). The workspace is in the desired state, so the op
// is a no-op success: release the guard and spinner, converge the row via a
// reload, and advance any bulk drain.
func (a *App) handleWorkspaceRestoreSkipped(msg messages.WorkspaceRestoreSkipped) []tea.Cmd {
	if msg.Workspace == nil {
		return nil
	}
	var cmds []tea.Cmd
	a.markWorkspaceMutationInFlight(msg.Workspace, false)
	if cmd := a.dashboard.SetWorkspaceBusy(msg.Workspace.Root, dashboard.WorkspaceOpRestore, false); cmd != nil {
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, a.loadProjects())
	if cmd := a.bulkFinished(msg.Workspace, true); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return cmds
}

// restoredToastCmd is the per-row "Restored X" toast — suppressed while a
// bulk restore drains, since the batch emits one summary toast at the end
// instead of N row toasts (same rule as shelve).
func (a *App) restoredToastCmd(ws *data.Workspace) tea.Cmd {
	if a.bulk.activeFor(bulkOpRestore) {
		return nil
	}
	return a.toast.ShowSuccess("Restored " + ws.Name)
}

// handleWorkspaceRestoreFailed surfaces a restore failure and releases the
// lifecycle guard so the shelved row can be retried.
func (a *App) handleWorkspaceRestoreFailed(msg messages.WorkspaceRestoreFailed) tea.Cmd {
	if msg.Workspace != nil {
		a.markWorkspaceMutationInFlight(msg.Workspace, false)
		a.dashboard.SetWorkspaceBusy(msg.Workspace.Root, dashboard.WorkspaceOpRestore, false)
	}
	var cmds []tea.Cmd
	if msg.Workspace != nil {
		// Same mid-flight-skip compensation as the shelve-failure path: a
		// save skipped by the guard must not strand the dirty marker.
		if cmd := a.persistWorkspaceTabs(string(msg.Workspace.ID())); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if errCmd := common.ReportError(errorContext(errorServiceWorkspace, "restoring workspace"), msg.Err, ""); errCmd != nil {
		cmds = append(cmds, errCmd)
	}
	if cmd := a.bulkFinished(msg.Workspace, false); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return common.SafeBatch(cmds...)
}
