package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
)

// handleRenameWorkspace handles the RenameWorkspace message.
func (a *App) handleRenameWorkspace(msg messages.RenameWorkspace) tea.Cmd {
	if msg.Project == nil || msg.Workspace == nil {
		return nil
	}

	wsID := msg.Workspace.ID()

	// 1. Load from store and update name
	stored, err := a.workspaces.Load(wsID)
	if err != nil {
		logging.Error("Failed to load workspace for rename: %v", err)
		return a.toast.ShowError("Failed to rename: " + err.Error())
	}
	stored.Name = msg.NewName
	if err := a.workspaces.Save(stored); err != nil {
		logging.Error("Failed to save renamed workspace: %v", err)
		return a.toast.ShowError("Failed to save rename: " + err.Error())
	}

	// 2. Update activeWorkspace in-place
	if a.activeWorkspace != nil && a.activeWorkspace.ID() == wsID {
		a.activeWorkspace.Name = msg.NewName
	}

	// 3. Update projects array in-place
	for i := range a.projects {
		for j := range a.projects[i].Workspaces {
			if a.projects[i].Workspaces[j].ID() == wsID {
				a.projects[i].Workspaces[j].Name = msg.NewName
			}
		}
	}

	// 4. Update center pane tab references
	a.center.UpdateWorkspaceName(string(wsID), msg.NewName)

	// 5. Reload projects + toast
	return a.safeBatch(
		a.toast.ShowSuccess(fmt.Sprintf("Renamed to '%s'", msg.NewName)),
		a.loadProjects(),
	)
}

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

	// Reset agent state tracking immediately to avoid false positive transitions
	// during workspace creation. This prevents existing workspaces from being
	// marked as unread due to timing-based state fluctuations.
	a.prevAgentStates = make(map[string]int)

	if msg.Workspace != nil {
		if cmd := a.dashboard.SetWorkspaceCreating(msg.Workspace, false); cmd != nil {
			cmds = append(cmds, cmd)
		}
		cmds = append(cmds, a.runSetupAsync(msg.Workspace))
		// Mark for auto-launch after projects reload
		if a.config.UI.AutoStartAgent {
			a.pendingAutoLaunch = msg.Workspace.Root
		}
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
