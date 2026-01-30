package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
)

// handleDeleteWorkspace handles the DeleteWorkspace message.
func (a *App) handleDeleteWorkspace(msg messages.DeleteWorkspace) []tea.Cmd {
	var cmds []tea.Cmd
	if msg.Project == nil || msg.Workspace == nil {
		logging.Warn("DeleteWorkspace received with nil project or workspace")
		return nil
	}
	if cleanup := a.cleanupWorkspaceTmuxSessions(msg.Workspace); cleanup != nil {
		cmds = append(cmds, cleanup)
	}
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
	if msg.Workspace != nil {
		if cmd := a.dashboard.SetWorkspaceCreating(msg.Workspace, false); cmd != nil {
			return cmd
		}
	}
	a.err = msg.Err
	logging.Error("Error in creating workspace: %v", msg.Err)
	return nil
}

// handleWorkspaceDeleted handles the WorkspaceDeleted message.
func (a *App) handleWorkspaceDeleted(msg messages.WorkspaceDeleted) []tea.Cmd {
	var cmds []tea.Cmd
	if msg.Workspace != nil {
		if cleanup := a.cleanupWorkspaceTmuxSessions(msg.Workspace); cleanup != nil {
			cmds = append(cmds, cleanup)
		}
		if cmd := a.dashboard.SetWorkspaceDeleting(msg.Workspace.Root, false); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if a.statusManager != nil {
			a.statusManager.Invalidate(msg.Workspace.Root)
		}
	}
	cmds = append(cmds, a.loadProjects())
	return cmds
}

// handleWorkspaceDeleteFailed handles the WorkspaceDeleteFailed message.
func (a *App) handleWorkspaceDeleteFailed(msg messages.WorkspaceDeleteFailed) tea.Cmd {
	if msg.Workspace != nil {
		if cmd := a.dashboard.SetWorkspaceDeleting(msg.Workspace.Root, false); cmd != nil {
			return cmd
		}
	}
	a.err = msg.Err
	logging.Error("Error in removing workspace: %v", msg.Err)
	return nil
}
