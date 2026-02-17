package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/git"
	"github.com/andyrewlee/medusa/internal/logging"
	"github.com/andyrewlee/medusa/internal/messages"
	"github.com/andyrewlee/medusa/internal/tmux"
)

// handleWorkspaceFetchDone handles the WorkspaceFetchDone message (step 1 of creation).
func (a *App) handleWorkspaceFetchDone(msg messages.WorkspaceFetchDone) []tea.Cmd {
	var cmds []tea.Cmd
	// Advance overlay to step 1 ("Creating worktree")
	if a.creationOverlay != nil {
		a.creationOverlay.AdvanceStep()
	}
	// Show the "creating" indicator in the dashboard
	if msg.Project != nil && msg.Name != "" {
		workspacePath := filepath.Join(
			a.config.Paths.WorkspacesRoot,
			msg.Project.Name,
			msg.Name,
		)
		pending := data.NewWorkspace(msg.Name, msg.Name, msg.Base, msg.Project.Path, workspacePath)
		if cmd := a.dashboard.SetWorkspaceCreating(pending, true); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	cmds = append(cmds, a.createWorkspace(msg.Project, msg.Name, msg.Base, msg.AllowEdits))
	return cmds
}

// handleRenameWorkspace handles the RenameWorkspace message.
// Phase A: dispatches a background command that renames the git branch,
// moves the worktree directory, updates the store, and migrates tmux tags.
// On completion it returns WorkspaceRenamed or WorkspaceRenameFailed.
func (a *App) handleRenameWorkspace(msg messages.RenameWorkspace) tea.Cmd {
	if msg.Project == nil || msg.Workspace == nil {
		return nil
	}

	ws := msg.Workspace
	project := msg.Project
	newName := msg.NewName
	oldBranch := ws.Branch
	newBranch := newName
	oldRoot := ws.Root
	newRoot := filepath.Join(filepath.Dir(oldRoot), newName)
	repoPath := ws.Repo
	opts := a.tmuxOptions

	// Capture a snapshot of the old workspace for the UI migration phase.
	oldWs := &data.Workspace{
		Name:   ws.Name,
		Branch: ws.Branch,
		Repo:   ws.Repo,
		Root:   ws.Root,
	}

	return func() tea.Msg {
		// 1. Validate: branch and target directory must not already exist.
		if git.BranchExists(repoPath, newBranch) {
			return messages.WorkspaceRenameFailed{
				Project:   project,
				Workspace: ws,
				Err:       fmt.Errorf("branch '%s' already exists", newBranch),
			}
		}
		if _, err := os.Stat(newRoot); err == nil {
			return messages.WorkspaceRenameFailed{
				Project:   project,
				Workspace: ws,
				Err:       fmt.Errorf("directory '%s' already exists", filepath.Base(newRoot)),
			}
		}

		// 2. Rename branch: git branch -m oldBranch newBranch
		if err := git.RenameBranch(repoPath, oldBranch, newBranch); err != nil {
			return messages.WorkspaceRenameFailed{
				Project:   project,
				Workspace: ws,
				Err:       fmt.Errorf("rename branch: %w", err),
			}
		}

		// 3. Move worktree: git worktree move oldRoot newRoot
		if err := git.MoveWorkspace(repoPath, oldRoot, newRoot); err != nil {
			// Rollback branch rename
			_ = git.RenameBranch(repoPath, newBranch, oldBranch)
			return messages.WorkspaceRenameFailed{
				Project:   project,
				Workspace: ws,
				Err:       fmt.Errorf("move worktree: %w", err),
			}
		}

		// 4. Update store: load, set new fields, Save() auto-migrates the ID.
		stored, err := a.workspaces.Load(ws.ID())
		if err != nil {
			// Rollback both git operations
			_ = git.MoveWorkspace(repoPath, newRoot, oldRoot)
			_ = git.RenameBranch(repoPath, newBranch, oldBranch)
			return messages.WorkspaceRenameFailed{
				Project:   project,
				Workspace: ws,
				Err:       fmt.Errorf("load workspace: %w", err),
			}
		}
		stored.Name = newName
		stored.Branch = newBranch
		stored.Root = newRoot
		if err := a.workspaces.Save(stored); err != nil {
			// Rollback both git operations
			_ = git.MoveWorkspace(repoPath, newRoot, oldRoot)
			_ = git.RenameBranch(repoPath, newBranch, oldBranch)
			return messages.WorkspaceRenameFailed{
				Project:   project,
				Workspace: ws,
				Err:       fmt.Errorf("save workspace: %w", err),
			}
		}

		// 5. Update tmux session tags from old workspace ID to new ID (best-effort).
		oldID := oldWs.ID()
		newID := stored.ID()
		sessions, _ := tmux.ListSessionsMatchingTags(map[string]string{
			"@medusa":           "1",
			"@medusa_workspace": string(oldID),
		}, opts)
		for _, sess := range sessions {
			_ = tmux.SetSessionOption(sess, "@medusa_workspace", string(newID), opts)
		}

		// 6. Rename tmux sessions: medusa-{oldName}-N → medusa-{newName}-N (cosmetic, best-effort).
		oldPrefix := tmux.SessionName("medusa", ws.Name) + "-"
		newPrefix := tmux.SessionName("medusa", newName) + "-"
		allSessions, _ := tmux.ListSessions(opts)
		for _, sess := range allSessions {
			if strings.HasPrefix(sess, oldPrefix) {
				suffix := strings.TrimPrefix(sess, oldPrefix)
				_ = tmux.RenameSession(sess, newPrefix+suffix, opts)
			}
		}

		return messages.WorkspaceRenamed{
			Project:      project,
			OldWorkspace: oldWs,
			NewWorkspace: stored,
		}
	}
}

// handleWorkspaceRenamed handles Phase B of the rename: synchronous UI state migration.
func (a *App) handleWorkspaceRenamed(msg messages.WorkspaceRenamed) []tea.Cmd {
	var cmds []tea.Cmd
	oldID := string(msg.OldWorkspace.ID())
	newID := string(msg.NewWorkspace.ID())
	oldName := msg.OldWorkspace.Name
	newName := msg.NewWorkspace.Name
	newWs := msg.NewWorkspace

	// 1. Update activeWorkspace pointer if it matches old ID.
	if a.activeWorkspace != nil && string(a.activeWorkspace.ID()) == oldID {
		a.activeWorkspace = newWs
	}

	// 2. Update projects array in-place, including OpenTabs session names.
	oldPrefix := tmux.SessionName("medusa", oldName) + "-"
	newPrefix := tmux.SessionName("medusa", newName) + "-"
	for i := range a.projects {
		for j := range a.projects[i].Workspaces {
			ws := &a.projects[i].Workspaces[j]
			if string(ws.ID()) == oldID {
				ws.Name = newWs.Name
				ws.Branch = newWs.Branch
				ws.Root = newWs.Root
				// Update session names in persisted OpenTabs so tmux sync
				// checks the correct (renamed) sessions.
				for k := range ws.OpenTabs {
					if strings.HasPrefix(ws.OpenTabs[k].SessionName, oldPrefix) {
						ws.OpenTabs[k].SessionName = newPrefix + strings.TrimPrefix(ws.OpenTabs[k].SessionName, oldPrefix)
					}
				}
			}
		}
	}

	// 3. Migrate center pane tabs (also updates tmux session names on each tab).
	a.center.MigrateWorkspaceTabs(oldID, newID, newWs, oldName, newName)

	// 4. Migrate sidebar terminal tabs.
	a.sidebarTerminal.MigrateWorkspaceTabs(oldID, newID, newWs)

	// 5. Migrate agent manager (also updates tmux session names on each agent).
	a.center.AgentManager().MigrateWorkspaceAgents(
		data.WorkspaceID(oldID),
		data.WorkspaceID(newID),
		newWs,
		oldName, newName,
	)

	// 6. Migrate dirtyWorkspaces tracking.
	if a.dirtyWorkspaces[oldID] {
		delete(a.dirtyWorkspaces, oldID)
		a.dirtyWorkspaces[newID] = true
	}

	// 7. Update file watcher: unwatch old root, watch new root.
	if a.fileWatcher != nil {
		a.fileWatcher.Unwatch(msg.OldWorkspace.Root)
		_ = a.fileWatcher.Watch(newWs.Root)
	}

	// 8. Invalidate git status cache for old root.
	if a.statusManager != nil {
		a.statusManager.Invalidate(msg.OldWorkspace.Root)
	}

	// 9. Persist updated tab state (new session names) so tmux sync uses correct names.
	if cmd := a.persistWorkspaceTabs(newID); cmd != nil {
		cmds = append(cmds, cmd)
	}

	// 10. Show success toast + reload projects.
	cmds = append(cmds,
		a.toast.ShowSuccess(fmt.Sprintf("Renamed to '%s'", newWs.Name)),
		a.loadProjects(),
	)

	return cmds
}

// handleWorkspaceRenameFailed handles a failed workspace rename.
func (a *App) handleWorkspaceRenameFailed(msg messages.WorkspaceRenameFailed) tea.Cmd {
	logging.Error("Failed to rename workspace %s: %v", msg.Workspace.Name, msg.Err)
	return a.toast.ShowError("Rename failed: " + msg.Err.Error())
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
	// Clean up tabs first so that killing tmux sessions doesn't trigger
	// auto-reattach logic in the now-removed PTY readers.
	a.center.CleanupWorkspace(msg.Workspace)
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
	a.creationOverlay = nil
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
	a.creationOverlay = nil
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
