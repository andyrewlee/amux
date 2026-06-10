package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// handleDeleteWorkspace handles the DeleteWorkspace message.
func (a *App) handleDeleteWorkspace(msg messages.DeleteWorkspace) []tea.Cmd {
	var cmds []tea.Cmd
	if msg.Project == nil || msg.Workspace == nil {
		logging.Warn("DeleteWorkspace received with nil project or workspace")
		return nil
	}
	a.markWorkspaceDeleteInFlight(msg.Workspace, true)
	// Do NOT kill the workspace's tmux sessions here. All real delete validation
	// (primary-checkout guard, repo/path checks, worktree removal) runs later in
	// the async DeleteWorkspace cmd; killing up-front means a rejected or failed
	// delete still destroys live agent sessions and scrollback. The kill now runs
	// only on the confirmed-success path in handleWorkspaceDeleted.
	if cmd := a.dashboard.SetWorkspaceDeleting(msg.Workspace.Root, true); cmd != nil {
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, a.deleteWorkspace(msg.Project, msg.Workspace))
	return cmds
}

// handleWorkspaceCreatedWithWarning handles the WorkspaceCreatedWithWarning message.
func (a *App) handleWorkspaceCreatedWithWarning(msg messages.WorkspaceCreatedWithWarning) []tea.Cmd {
	var cmds []tea.Cmd
	a.err = fmt.Errorf("workspace created with warning: %s", msg.Warning)
	if msg.Workspace != nil {
		a.lifecycle.clearCreating(string(msg.Workspace.ID()))
		if cmd := a.dashboard.SetWorkspaceCreating(msg.Workspace, false); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	cmds = append(cmds, a.loadProjects())
	return cmds
}

// handleWorkspaceCreated handles the WorkspaceCreated message.
func (a *App) handleWorkspaceCreated(msg messages.WorkspaceCreated) []tea.Cmd {
	var cmds []tea.Cmd
	if msg.Workspace != nil {
		a.lifecycle.clearCreating(string(msg.Workspace.ID()))
		if cmd := a.dashboard.SetWorkspaceCreating(msg.Workspace, false); cmd != nil {
			cmds = append(cmds, cmd)
		}
		cmds = append(cmds, a.runSetupAsync(msg.Workspace))
	}
	cmds = append(cmds, a.loadProjects())
	return cmds
}

// handleWorkspaceSetupComplete handles the WorkspaceSetupComplete message.
func (a *App) handleWorkspaceSetupComplete(msg messages.WorkspaceSetupComplete) tea.Cmd {
	if msg.Err != nil {
		return a.toast.ShowWarning(fmt.Sprintf("Setup failed for %s: %v", msg.Workspace.Name, msg.Err))
	}
	return nil
}

// handleWorkspaceCreateFailed handles the WorkspaceCreateFailed message.
func (a *App) handleWorkspaceCreateFailed(msg messages.WorkspaceCreateFailed) tea.Cmd {
	var cmds []tea.Cmd
	if msg.Workspace != nil {
		a.lifecycle.clearCreating(string(msg.Workspace.ID()))
		if cmd := a.dashboard.SetWorkspaceCreating(msg.Workspace, false); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if errCmd := common.ReportError(errorContext(errorServiceWorkspace, "creating workspace"), msg.Err, ""); errCmd != nil {
		cmds = append(cmds, errCmd)
	}
	return common.SafeBatch(cmds...)
}

// handleWorkspaceDeleted handles the WorkspaceDeleted message.
func (a *App) handleWorkspaceDeleted(msg messages.WorkspaceDeleted) []tea.Cmd {
	var cmds []tea.Cmd
	if msg.Warning != "" {
		cmds = append(cmds, a.toast.ShowWarning(msg.Warning))
	}
	if msg.Workspace != nil {
		a.markWorkspaceDeleteInFlight(msg.Workspace, false)
		// Drop the deleted workspace from the active set now rather than waiting
		// for the async loadProjects -> scan reconcile, so a killed-but-not-yet-
		// reaped agent session cannot keep it shown as active by tag alone.
		delete(a.tmuxActivity.activeWorkspaceIDs, string(msg.Workspace.ID()))
		a.syncActiveWorkspacesToDashboard()
		// Navigate home only now that the delete is confirmed (moved off the
		// up-front deleteWorkspace path so a failed delete leaves the user put).
		if a.activeWorkspace != nil && a.activeWorkspace.Root == msg.Workspace.Root {
			a.goHome()
		}
		delete(a.lifecycle.dirty, string(msg.Workspace.ID()))
		// No trailing tmux cleanup here: the validated delete path already tore
		// down this workspace's sessions before removing the worktree. Re-running
		// it after the delete-in-flight flag is cleared would, on a delete-then-
		// recreate at the same project+name (same wsID, same session names), match
		// and kill the brand-new agent session by tag.
		if cmd := a.dashboard.SetWorkspaceDeleting(msg.Workspace.Root, false); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if a.gitStatus != nil {
			a.gitStatus.Invalidate(msg.Workspace.Root)
		}
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
	if msg.Err != nil {
		a.removeWorkspaceFromLoadedProjects(msg.Workspace)
		if a.dashboard != nil {
			a.dashboard.SetProjects(a.projects)
		}
		if errCmd := common.ReportError(errorContext(errorServiceWorkspace, "removing workspace metadata"), msg.Err, ""); errCmd != nil {
			cmds = append(cmds, errCmd)
		}
		return cmds
	}
	cmds = append(cmds, a.loadProjects())
	return cmds
}

func (a *App) removeWorkspaceFromLoadedProjects(ws *data.Workspace) {
	if ws == nil {
		return
	}
	wsID := string(ws.ID())
	for i := range a.projects {
		workspaces := a.projects[i].Workspaces
		filtered := workspaces[:0]
		for j := range workspaces {
			candidate := &workspaces[j]
			if string(candidate.ID()) == wsID || candidate.Root == ws.Root {
				continue
			}
			filtered = append(filtered, workspaces[j])
		}
		a.projects[i].Workspaces = filtered
	}
}

// handleWorkspaceDeleteFailed handles the WorkspaceDeleteFailed message.
func (a *App) handleWorkspaceDeleteFailed(msg messages.WorkspaceDeleteFailed) tea.Cmd {
	var cmds []tea.Cmd
	if msg.Workspace != nil {
		// Ordering is intentional: clear delete-in-flight first so the
		// persistence requeue below is not suppressed.
		a.markWorkspaceDeleteInFlight(msg.Workspace, false)
		// Clear the delete tombstone only when the worktree is still present (the
		// delete failed before removing it, so the workspace stays usable). If the
		// worktree is already gone — e.g. metadata removal failed after the worktree
		// was deleted — leave the tombstone so startup recovery finishes the delete
		// rather than resurfacing a dir-less ghost.
		if a.workspaceService != nil && dirExists(msg.Workspace.Root) {
			a.workspaceService.clearDeleteTombstone(msg.Workspace.ID())
		}
		if cmd := a.dashboard.SetWorkspaceDeleting(msg.Workspace.Root, false); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if a.tmuxAvailable {
			if cmd := a.scanTmuxActivityNow(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if cmd := a.persistWorkspaceTabs(string(msg.Workspace.ID())); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if errCmd := common.ReportError(errorContext(errorServiceWorkspace, "removing workspace"), msg.Err, ""); errCmd != nil {
		cmds = append(cmds, errCmd)
	}
	return common.SafeBatch(cmds...)
}
