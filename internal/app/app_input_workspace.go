package app

import (
	"fmt"
	"os"
	"path/filepath"

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
	cmds = append(cmds, a.createWorkspace(msg.Project, msg.Name, msg.Base, msg.AllowEdits, msg.Isolated, msg.SkipPermissions))
	return cmds
}

// handleRenameWorkspace handles the RenameWorkspace message.
// Everything runs synchronously: kill tabs, rename git branch + worktree,
// update store, update UI state, and relaunch agents.
func (a *App) handleRenameWorkspace(msg messages.RenameWorkspace) []tea.Cmd {
	if msg.Project == nil || msg.Workspace == nil {
		return nil
	}

	ws := msg.Workspace
	newName := msg.NewName
	oldBranch := ws.Branch
	newBranch := newName
	oldRoot := ws.Root
	newRoot := filepath.Join(filepath.Dir(oldRoot), newName)
	repoPath := ws.Repo
	opts := a.tmuxOptions
	oldWsID := string(ws.ID())

	// 1. Capture running agent tab info for restart after rename.
	tabsInfo, _ := a.center.GetTabsInfoForWorkspace(oldWsID)
	var agentTabs []data.TabInfo
	for _, t := range tabsInfo {
		if t.Assistant != "" {
			agentTabs = append(agentTabs, t)
		}
	}

	// 2. Close all tabs and kill their tmux sessions.
	a.center.CleanupWorkspace(ws)
	for _, t := range tabsInfo {
		if t.SessionName != "" {
			_ = tmux.KillSession(t.SessionName, opts)
		}
	}

	// 3. Validate: branch and target directory must not already exist.
	if git.BranchExists(repoPath, newBranch) {
		return []tea.Cmd{a.toast.ShowError(fmt.Sprintf("Branch '%s' already exists", newBranch))}
	}
	if _, err := os.Stat(newRoot); err == nil {
		return []tea.Cmd{a.toast.ShowError(fmt.Sprintf("Directory '%s' already exists", filepath.Base(newRoot)))}
	}

	// 4. Rename branch.
	if err := git.RenameBranch(repoPath, oldBranch, newBranch); err != nil {
		return []tea.Cmd{a.toast.ShowError("Rename failed: " + err.Error())}
	}

	// 5. Move worktree.
	if err := git.MoveWorkspace(repoPath, oldRoot, newRoot); err != nil {
		_ = git.RenameBranch(repoPath, newBranch, oldBranch)
		return []tea.Cmd{a.toast.ShowError("Rename failed: " + err.Error())}
	}

	// 6. Update store.
	stored, err := a.workspaces.Load(ws.ID())
	if err != nil {
		_ = git.MoveWorkspace(repoPath, newRoot, oldRoot)
		_ = git.RenameBranch(repoPath, newBranch, oldBranch)
		return []tea.Cmd{a.toast.ShowError("Rename failed: " + err.Error())}
	}
	stored.Name = newName
	stored.Branch = newBranch
	stored.Root = newRoot
	if err := a.workspaces.Save(stored); err != nil {
		_ = git.MoveWorkspace(repoPath, newRoot, oldRoot)
		_ = git.RenameBranch(repoPath, newBranch, oldBranch)
		return []tea.Cmd{a.toast.ShowError("Rename failed: " + err.Error())}
	}
	newWs := stored

	// 7. Update in-memory UI state.
	if a.activeWorkspace != nil && string(a.activeWorkspace.ID()) == oldWsID {
		newWs.Profile = a.activeWorkspace.Profile
		a.activeWorkspace = newWs
		a.center.SetWorkspace(newWs)
	}
	newID := string(newWs.ID())
	for i := range a.projects {
		for j := range a.projects[i].Workspaces {
			pw := &a.projects[i].Workspaces[j]
			if string(pw.ID()) == oldWsID {
				pw.Name = newWs.Name
				pw.Branch = newWs.Branch
				pw.Root = newWs.Root
				pw.OpenTabs = nil
			}
		}
	}
	if a.dirtyWorkspaces[oldWsID] {
		delete(a.dirtyWorkspaces, oldWsID)
		a.dirtyWorkspaces[newID] = true
	}
	if a.fileWatcher != nil {
		a.fileWatcher.Unwatch(oldRoot)
		_ = a.fileWatcher.Watch(newWs.Root)
	}
	if a.permissionWatcher != nil {
		a.permissionWatcher.Unwatch(oldRoot)
		_ = a.permissionWatcher.Watch(newWs.Root)
	}
	if a.statusManager != nil {
		a.statusManager.Invalidate(oldRoot)
	}

	// 8. Persist tab state, toast, reload, and relaunch agents.
	var cmds []tea.Cmd
	if cmd := a.persistWorkspaceTabs(newID); cmd != nil {
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds,
		a.toast.ShowSuccess(fmt.Sprintf("Renamed to '%s'", newWs.Name)),
		a.loadProjects(),
	)
	for _, tabInfo := range agentTabs {
		assistant := tabInfo.Assistant
		w := newWs
		cmds = append(cmds, func() tea.Msg {
			return messages.LaunchAgent{Assistant: assistant, Workspace: w}
		})
	}
	return cmds
}

// handleWorkspaceRenameFailed handles a failed workspace rename.
func (a *App) handleWorkspaceRenameFailed(msg messages.WorkspaceRenameFailed) tea.Cmd {
	logging.Error("Failed to rename workspace %s: %v", msg.Workspace.Name, msg.Err)
	return a.toast.ShowError("Rename failed: " + msg.Err.Error())
}

// handleRenameGroup handles renaming a project group.
// This migrates workspace storage and kills old tmux sessions since the
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

		// Kill old tmux sessions for this workspace.
		tmux.KillSessionsMatchingTags(map[string]string{
			"@medusa":           "1",
			"@medusa_workspace": string(oldID),
		}, opts)

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
// Everything runs synchronously: kill tabs, rename git branches in all repos,
// move all worktrees, rename the group root directory, update store, update UI
// state, and relaunch agents.
func (a *App) handleRenameGroupWorkspace(msg messages.RenameGroupWorkspace) []tea.Cmd {
	if msg.Group == nil || msg.Workspace == nil {
		return nil
	}

	gw := msg.Workspace
	newName := msg.NewName
	oldName := gw.Name
	opts := a.tmuxOptions
	oldPrimaryID := string(gw.Primary.ID())

	// 1. Capture running agent tab info for restart after rename.
	tabsInfo, _ := a.center.GetTabsInfoForWorkspace(oldPrimaryID)
	var agentTabs []data.TabInfo
	for _, t := range tabsInfo {
		if t.Assistant != "" {
			agentTabs = append(agentTabs, t)
		}
	}

	// 2. Close all tabs and kill their tmux sessions.
	a.center.CleanupWorkspace(&gw.Primary)
	for _, t := range tabsInfo {
		if t.SessionName != "" {
			_ = tmux.KillSession(t.SessionName, opts)
		}
	}

	// Snapshot old secondary roots for file watcher migration.
	oldSecondaryRoots := make([]string, len(gw.Secondary))
	for i, ws := range gw.Secondary {
		oldSecondaryRoots[i] = ws.Root
	}

	// 3. Validate: check that the new branch doesn't exist in any repo
	//    and that no target directories exist.
	newGroupRoot := filepath.Join(filepath.Dir(gw.Primary.Root), newName)
	if _, err := os.Stat(newGroupRoot); err == nil {
		return []tea.Cmd{a.toast.ShowError(fmt.Sprintf("Directory '%s' already exists", filepath.Base(newGroupRoot)))}
	}
	for _, ws := range gw.Secondary {
		if git.BranchExists(ws.Repo, newName) {
			return []tea.Cmd{a.toast.ShowError(fmt.Sprintf("Branch '%s' already exists in %s", newName, filepath.Base(ws.Repo)))}
		}
	}

	// 4. Rename branches in all secondary repos.
	var renamedBranches []int
	for i, ws := range gw.Secondary {
		if err := git.RenameBranch(ws.Repo, oldName, newName); err != nil {
			for j := len(renamedBranches) - 1; j >= 0; j-- {
				idx := renamedBranches[j]
				_ = git.RenameBranch(gw.Secondary[idx].Repo, newName, oldName)
			}
			return []tea.Cmd{a.toast.ShowError(fmt.Sprintf("Rename failed in %s: %s", filepath.Base(ws.Repo), err.Error()))}
		}
		renamedBranches = append(renamedBranches, i)
	}

	// 5. Create the new group root directory so worktrees can be moved into it.
	if err := os.MkdirAll(newGroupRoot, 0755); err != nil {
		for _, idx := range renamedBranches {
			_ = git.RenameBranch(gw.Secondary[idx].Repo, newName, oldName)
		}
		return []tea.Cmd{a.toast.ShowError("Rename failed: " + err.Error())}
	}

	// 6. Move all secondary worktrees into the new group root.
	var movedWorktrees []int
	for i, ws := range gw.Secondary {
		newRoot := filepath.Join(newGroupRoot, filepath.Base(ws.Root))
		if err := git.MoveWorkspace(ws.Repo, ws.Root, newRoot); err != nil {
			for j := len(movedWorktrees) - 1; j >= 0; j-- {
				idx := movedWorktrees[j]
				oldWsRoot := gw.Secondary[idx].Root
				newWsRoot := filepath.Join(newGroupRoot, filepath.Base(oldWsRoot))
				_ = git.MoveWorkspace(gw.Secondary[idx].Repo, newWsRoot, oldWsRoot)
			}
			_ = os.Remove(newGroupRoot)
			for _, idx := range renamedBranches {
				_ = git.RenameBranch(gw.Secondary[idx].Repo, newName, oldName)
			}
			return []tea.Cmd{a.toast.ShowError(fmt.Sprintf("Rename failed for %s: %s", filepath.Base(ws.Repo), err.Error()))}
		}
		movedWorktrees = append(movedWorktrees, i)
	}

	// 7. Move remaining non-worktree files (e.g. .claude/) from old root
	//    to new root, then remove the old root directory.
	entries, _ := os.ReadDir(gw.Primary.Root)
	for _, entry := range entries {
		oldPath := filepath.Join(gw.Primary.Root, entry.Name())
		newPath := filepath.Join(newGroupRoot, entry.Name())
		if _, err := os.Stat(newPath); err == nil {
			continue
		}
		_ = os.Rename(oldPath, newPath)
	}
	_ = os.Remove(gw.Primary.Root)

	// 8. Update store: load, update fields, save under new ID, delete old ID.
	oldGwID := gw.ID()
	stored, err := a.workspaces.LoadGroupWorkspace(oldGwID)
	if err != nil {
		return []tea.Cmd{a.toast.ShowError("Rename failed: " + err.Error())}
	}
	stored.Name = newName
	stored.Primary.Name = newName
	stored.Primary.Branch = newName
	stored.Primary.Root = newGroupRoot
	for i := range stored.Secondary {
		stored.Secondary[i].Name = newName
		stored.Secondary[i].Branch = newName
		stored.Secondary[i].Root = filepath.Join(newGroupRoot, filepath.Base(stored.Secondary[i].Root))
	}
	if err := a.workspaces.SaveGroupWorkspace(stored); err != nil {
		return []tea.Cmd{a.toast.ShowError("Rename failed: " + err.Error())}
	}
	if stored.ID() != oldGwID {
		_ = a.workspaces.DeleteGroupWorkspace(oldGwID)
	}

	newPrimary := &stored.Primary
	newPrimaryID := string(newPrimary.ID())

	// 9. Update in-memory UI state.
	if a.activeWorkspace != nil && string(a.activeWorkspace.ID()) == oldPrimaryID {
		newPrimary.Profile = a.activeWorkspace.Profile
		a.activeWorkspace = newPrimary
		a.center.SetWorkspace(newPrimary)
	}
	if a.activeGroupWs != nil && a.activeGroupWs.ID() == gw.ID() {
		stored.Profile = a.activeGroupWs.Profile
		stored.Primary.Profile = a.activeGroupWs.Profile
		for i := range stored.Secondary {
			stored.Secondary[i].Profile = a.activeGroupWs.Profile
		}
		a.activeGroupWs = stored
	}

	for i := range a.groups {
		groupProfile := a.groups[i].Profile
		for j := range a.groups[i].Workspaces {
			gw := &a.groups[i].Workspaces[j]
			if gw.ID() == oldGwID {
				gw.Name = newName
				gw.Primary = stored.Primary
				gw.Secondary = stored.Secondary
				gw.Profile = groupProfile
				gw.Primary.Profile = groupProfile
				for k := range gw.Secondary {
					gw.Secondary[k].Profile = groupProfile
				}
				gw.OpenTabs = nil
			}
		}
	}

	if a.dirtyWorkspaces[oldPrimaryID] {
		delete(a.dirtyWorkspaces, oldPrimaryID)
		a.dirtyWorkspaces[newPrimaryID] = true
	}

	if a.fileWatcher != nil {
		for _, root := range oldSecondaryRoots {
			a.fileWatcher.Unwatch(root)
		}
		for _, ws := range stored.Secondary {
			_ = a.fileWatcher.Watch(ws.Root)
		}
	}
	if a.permissionWatcher != nil {
		for _, root := range oldSecondaryRoots {
			a.permissionWatcher.Unwatch(root)
		}
		for _, ws := range stored.Secondary {
			_ = a.permissionWatcher.Watch(ws.Root)
		}
	}

	if a.statusManager != nil {
		for _, root := range oldSecondaryRoots {
			a.statusManager.Invalidate(root)
		}
	}

	// 10. Persist tab state, toast, reload, and relaunch agents.
	var cmds []tea.Cmd
	if cmd := a.persistWorkspaceTabs(newPrimaryID); cmd != nil {
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds,
		a.toast.ShowSuccess(fmt.Sprintf("Renamed to '%s'", newName)),
		a.loadGroups(),
	)
	for _, tabInfo := range agentTabs {
		assistant := tabInfo.Assistant
		ws := newPrimary
		cmds = append(cmds, func() tea.Msg {
			return messages.LaunchAgent{Assistant: assistant, Workspace: ws}
		})
	}
	return cmds
}

// handleGroupWorkspaceRenameFailed handles a failed group workspace rename.
func (a *App) handleGroupWorkspaceRenameFailed(msg messages.GroupWorkspaceRenameFailed) tea.Cmd {
	logging.Error("Failed to rename group workspace %s: %v", msg.Workspace.Name, msg.Err)
	return a.toast.ShowError("Rename failed: " + msg.Err.Error())
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
	if msg.BranchWarning != "" {
		cmds = append(cmds, a.toast.ShowWarning(msg.BranchWarning))
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
