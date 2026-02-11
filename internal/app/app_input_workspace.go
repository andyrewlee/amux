package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/medusa/internal/logging"
	"github.com/andyrewlee/medusa/internal/messages"
	"github.com/andyrewlee/medusa/internal/tmux"
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

// handleRenameGroup handles renaming a project group.
// This migrates workspace storage and updates tmux session tags since the
// workspace ID (which includes group name) changes.
func (a *App) handleRenameGroup(msg messages.RenameGroup) tea.Cmd {
	if msg.Group == nil {
		return nil
	}
	oldName := msg.Group.Name
	newName := msg.NewName
	opts := a.tmuxOptions

	// 1. Migrate each workspace: old ID → new ID
	for i := range msg.Group.Workspaces {
		gw := &msg.Group.Workspaces[i]
		oldID := gw.ID()

		stored, err := a.workspaces.LoadGroupWorkspace(oldID)
		if err != nil {
			logging.Error("Failed to load group workspace %s for group rename: %v", gw.Name, err)
			continue
		}

		// Update group name and save to new location (new ID)
		stored.GroupName = newName
		if err := a.workspaces.SaveGroupWorkspace(stored); err != nil {
			logging.Error("Failed to save migrated group workspace %s: %v", gw.Name, err)
			continue
		}

		// Update tmux session tags from old ID to new ID
		newID := stored.ID()
		sessions, _ := tmux.ListSessionsMatchingTags(map[string]string{
			"@medusa":           "1",
			"@medusa_workspace": string(oldID),
		}, opts)
		for _, sess := range sessions {
			_ = tmux.SetSessionOption(sess, "@medusa_workspace", string(newID), opts)
		}

		// Delete old storage directory
		_ = a.workspaces.DeleteGroupWorkspace(oldID)
	}

	// 2. Rename group in registry
	if err := a.registry.RenameGroup(oldName, newName); err != nil {
		logging.Error("Failed to rename group: %v", err)
		return a.toast.ShowError("Failed to rename: " + err.Error())
	}

	// 3. Update in-memory state
	if a.activeGroup != nil && a.activeGroup.Name == oldName {
		a.activeGroup.Name = newName
	}
	for i := range a.groups {
		if a.groups[i].Name == oldName {
			a.groups[i].Name = newName
			for j := range a.groups[i].Workspaces {
				a.groups[i].Workspaces[j].GroupName = newName
			}
		}
	}

	// 4. Reload groups + toast
	return a.safeBatch(
		a.toast.ShowSuccess(fmt.Sprintf("Renamed group to '%s'", newName)),
		a.loadGroups(),
	)
}

// handleRenameGroupWorkspace handles renaming a group workspace.
// Only the display name changes — the workspace ID (based on group+repo+root)
// stays the same, so tmux sessions tagged with the ID remain valid.
func (a *App) handleRenameGroupWorkspace(msg messages.RenameGroupWorkspace) tea.Cmd {
	if msg.Group == nil || msg.Workspace == nil {
		return nil
	}

	wsID := msg.Workspace.ID()

	// 1. Load from store and update name
	stored, err := a.workspaces.LoadGroupWorkspace(wsID)
	if err != nil {
		logging.Error("Failed to load group workspace for rename: %v", err)
		return a.toast.ShowError("Failed to rename: " + err.Error())
	}
	stored.Name = msg.NewName
	if err := a.workspaces.SaveGroupWorkspace(stored); err != nil {
		logging.Error("Failed to save renamed group workspace: %v", err)
		return a.toast.ShowError("Failed to save rename: " + err.Error())
	}

	// 2. Update activeGroupWs in-place
	if a.activeGroupWs != nil && a.activeGroupWs.ID() == wsID {
		a.activeGroupWs.Name = msg.NewName
	}

	// 3. Update groups array in-place
	for i := range a.groups {
		for j := range a.groups[i].Workspaces {
			if a.groups[i].Workspaces[j].ID() == wsID {
				a.groups[i].Workspaces[j].Name = msg.NewName
			}
		}
	}

	// 4. Update center pane tab references
	a.center.UpdateWorkspaceName(string(wsID), msg.NewName)

	// 5. Reload groups + toast
	return a.safeBatch(
		a.toast.ShowSuccess(fmt.Sprintf("Renamed to '%s'", msg.NewName)),
		a.loadGroups(),
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
