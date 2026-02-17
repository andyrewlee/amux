package app

// TODO: Rename internal code to match new user-facing terminology:
//   - Struct/type: Workspace → Worktree, Project/ProjectGroup → Workspace, GroupWorkspace → Worktree
//   - Variables/functions: e.g. activeWorkspace → activeWorktree, handleCreateWorkspace → handleCreateWorktree
//   - Dialog IDs: DialogCreateWorkspace → DialogCreateWorktree, DialogRemoveProject → DialogRemoveWorkspace, etc.
//   - Message types: messages.CreateWorkspace → messages.CreateWorktree, messages.RemoveProject → messages.RemoveWorkspace, etc.
//   - File paths & JSON keys (breaking change, needs migration): workspaces.json, workspace.json, setup-workspace, etc.
// This was deferred to avoid breaking changes in the initial terminology fix.

import (
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/medusa/internal/config"
	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/git"
	"github.com/andyrewlee/medusa/internal/ide"
	"github.com/andyrewlee/medusa/internal/logging"
	"github.com/andyrewlee/medusa/internal/messages"
	"github.com/andyrewlee/medusa/internal/tmux"
	"github.com/andyrewlee/medusa/internal/ui/common"
	"github.com/andyrewlee/medusa/internal/validation"
)

var randomAnimals = []string{
	"falcon", "otter", "panda", "wolf", "hawk", "lynx", "fox", "bear",
	"eagle", "cobra", "raven", "tiger", "shark", "crane", "bison", "viper",
	"whale", "heron", "moose", "gecko", "horse", "finch", "manta", "newt",
}

var randomColors = []string{
	"red", "blue", "green", "amber", "coral", "ivory", "onyx", "jade",
	"gold", "teal", "plum", "sage", "ruby", "slate", "peach", "rust",
	"cyan", "lime", "navy", "sand", "rose", "mint", "dusk", "gray",
}

// generateWorkspaceName generates a unique random name in the format
// {project}-{animal}-{color} that doesn't conflict with existing workspaces.
func generateWorkspaceName(project *data.Project) string {
	const maxAttempts = 50
	for range maxAttempts {
		name := fmt.Sprintf("%s-%s-%s",
			project.Name,
			randomAnimals[rand.IntN(len(randomAnimals))],
			randomColors[rand.IntN(len(randomColors))],
		)
		if project.FindWorkspaceByName(name) == nil {
			return name
		}
	}
	// Fallback: append a random number to guarantee uniqueness
	return fmt.Sprintf("%s-%s-%s-%d",
		project.Name,
		randomAnimals[rand.IntN(len(randomAnimals))],
		randomColors[rand.IntN(len(randomColors))],
		rand.IntN(1000),
	)
}

// handleProjectsLoaded processes the ProjectsLoaded message.
func (a *App) handleProjectsLoaded(msg messages.ProjectsLoaded) []tea.Cmd {
	a.projects = msg.Projects
	a.dashboard.SetProjects(a.projects)
	var cmds []tea.Cmd
	cmds = append(cmds, a.scanTmuxActivityNow())
	// Request git status for all workspaces (skip when sidebar is hidden)
	if !a.layout.SidebarHidden() {
		for i := range a.projects {
			for j := range a.projects[i].Workspaces {
				ws := &a.projects[i].Workspaces[j]
				cmds = append(cmds, a.requestGitStatus(ws.Root))
			}
		}
	}

	// Eagerly restore agent tabs for all workspaces on startup.
	// This loads agents for workspaces that have running or detached tabs,
	// without requiring the user to click on each workspace first.
	for i := range a.projects {
		for j := range a.projects[i].Workspaces {
			ws := &a.projects[i].Workspaces[j]
			if workspaceHasLiveTabs(ws) {
				if restoreCmd := a.center.RestoreTabsFromWorkspace(ws); restoreCmd != nil {
					cmds = append(cmds, restoreCmd)
				}
			}
		}
	}

	// Start watching workspace permissions if enabled
	if a.config.UI.GlobalPermissions && a.permissionWatcher != nil {
		for i := range a.projects {
			for j := range a.projects[i].Workspaces {
				_ = a.permissionWatcher.Watch(a.projects[i].Workspaces[j].Root)
			}
		}
	}

	// Auto-activate a newly created workspace for auto-launch.
	// Only clear pendingAutoLaunch when the workspace is found; a stale
	// ProjectsLoaded from a concurrent loadProjects() call may arrive
	// before the workspace exists in the store.
	if a.pendingAutoLaunch != "" {
		root := a.pendingAutoLaunch
		for i := range a.projects {
			for j := range a.projects[i].Workspaces {
				ws := &a.projects[i].Workspaces[j]
				if ws.Root == root {
					a.pendingAutoLaunch = ""
					a.pendingAgentLaunch = root
					project := &a.projects[i]
					cmds = append(cmds, func() tea.Msg {
						return messages.WorkspaceActivated{
							Project:   project,
							Workspace: ws,
						}
					})
					goto pendingFound
				}
			}
		}
	pendingFound:
	}

	if a.pendingNewProjectPath != "" {
		path := a.pendingNewProjectPath
		// Don't clear pendingNewProjectPath yet — it's cleared by
		// handleDialogResult (cancel) or handleSetProfile (confirm)
		// so we can remove the project if the user cancels profile selection.
		for i := range a.projects {
			if a.projects[i].Path == path {
				project := &a.projects[i]
				cmds = append(cmds, func() tea.Msg {
					return messages.ShowSetProfileDialog{Project: project}
				})
				break
			}
		}
	}

	return cmds
}

// handleWorkspaceActivated processes the WorkspaceActivated message.
func (a *App) handleWorkspaceActivated(msg messages.WorkspaceActivated) []tea.Cmd {
	var cmds []tea.Cmd
	a.activeProject = msg.Project
	a.activeWorkspace = msg.Workspace
	a.showWelcome = false
	a.centerBtnFocused = false
	a.centerBtnIndex = 0
	a.center.SetWorkspace(msg.Workspace)
	a.sidebar.SetWorkspace(msg.Workspace)
	if msg.Workspace != nil {
		a.dashboard.MarkRead(string(msg.Workspace.ID()))
	}
	// Discover shared tmux tabs first; restore/sync happens below.
	if discoverCmd := a.discoverWorkspaceTabsFromTmux(msg.Workspace); discoverCmd != nil {
		cmds = append(cmds, discoverCmd)
	}
	if syncCmd := a.syncWorkspaceTabsFromTmux(msg.Workspace); syncCmd != nil {
		cmds = append(cmds, syncCmd)
	}
	if restoreCmd := a.center.RestoreTabsFromWorkspace(msg.Workspace); restoreCmd != nil {
		cmds = append(cmds, restoreCmd)
	}
	// Set up sidebar terminal for the workspace (skip when sidebar is hidden)
	if !a.layout.SidebarHidden() {
		if termCmd := a.sidebarTerminal.SetWorkspace(msg.Workspace); termCmd != nil {
			cmds = append(cmds, termCmd)
		}
	}
	// Sync active workspaces to dashboard (fixes spinner race condition)
	if startCmd := a.syncActiveWorkspacesToDashboard(); startCmd != nil {
		cmds = append(cmds, startCmd)
	}
	newDashboard, cmd := a.dashboard.Update(msg)
	a.dashboard = newDashboard
	cmds = append(cmds, cmd)

	// Refresh git status and set up file watching (skip when sidebar is hidden)
	if msg.Workspace != nil && !a.layout.SidebarHidden() {
		cmds = append(cmds, a.requestGitStatus(msg.Workspace.Root))
		if a.fileWatcher != nil {
			_ = a.fileWatcher.Watch(msg.Workspace.Root)
		}
	}
	// Watch workspace permissions if enabled
	if msg.Workspace != nil && a.config.UI.GlobalPermissions && a.permissionWatcher != nil {
		_ = a.permissionWatcher.Watch(msg.Workspace.Root)
	}
	// Ensure spinner starts if needed after sync
	if startCmd := a.dashboard.StartSpinnerIfNeeded(); startCmd != nil {
		cmds = append(cmds, startCmd)
	}

	// Auto-start agent when activating a workspace with no tabs.
	// Two triggers:
	//   1. pendingAgentLaunch — set after workspace creation, bypasses
	//      IsPrimaryCheckout guard since we know it's a fresh worktree.
	//   2. General AutoStartAgent setting — fires for any non-primary
	//      workspace that the user activates with no existing tabs.
	autoLaunch := false
	if a.pendingAgentLaunch != "" && msg.Workspace != nil && msg.Workspace.Root == a.pendingAgentLaunch {
		a.pendingAgentLaunch = ""
		autoLaunch = true
	} else if a.config.UI.AutoStartAgent && msg.Workspace != nil && !msg.Workspace.IsPrimaryCheckout() {
		autoLaunch = true
	}
	if autoLaunch {
		wsID := string(msg.Workspace.ID())
		if !a.center.HasTabsForWorkspace(wsID) && !workspaceHasLiveTabs(msg.Workspace) {
			ws := msg.Workspace
			if a.config.UI.DefaultAgent != "" {
				agent := a.config.UI.DefaultAgent
				cmds = append(cmds, func() tea.Msg {
					return messages.LaunchAgent{Assistant: agent, Workspace: ws}
				})
			} else {
				cmds = append(cmds, func() tea.Msg {
					return messages.ShowSelectAssistantDialog{}
				})
			}
		}
	}

	// Focus center pane when workspace has active tabs (keyboard activation parity with click)
	if msg.Workspace != nil {
		wsID := string(msg.Workspace.ID())
		if a.center.HasTabsForWorkspace(wsID) || workspaceHasLiveTabs(msg.Workspace) {
			if a.monitorMode {
				a.focusPane(messages.PaneMonitor)
			} else {
				a.focusPane(messages.PaneCenter)
			}
		}
	}

	return cmds
}

// Concurrency safety: takes a snapshot of ws.OpenTabs in the Update loop before
// spawning the Cmd. Results return as messages processed where mutations are safe.
func (a *App) syncWorkspaceTabsFromTmux(ws *data.Workspace) tea.Cmd {
	if ws == nil || len(ws.OpenTabs) == 0 {
		return nil
	}
	if !a.tmuxAvailable {
		return nil // Error shown on startup, don't repeat
	}
	// Mutate workspace state on the Bubble Tea update goroutine only.
	wsID := string(ws.ID())
	tabsSnapshot := make([]data.TabInfo, len(ws.OpenTabs))
	copy(tabsSnapshot, ws.OpenTabs)
	opts := a.tmuxOptions
	return func() tea.Msg {
		var updates []tmuxTabStatusUpdate
		for _, tab := range tabsSnapshot {
			if tab.SessionName == "" {
				continue
			}
			state, err := tmux.SessionStateFor(tab.SessionName, opts)
			if err != nil {
				// Tolerate transient errors; next tick reconciles. tmuxAvailable gates full outages.
				continue
			}
			if strings.EqualFold(tab.Status, "detached") {
				if !(state.Exists && state.HasLivePane) {
					updates = append(updates, tmuxTabStatusUpdate{
						SessionName:   tab.SessionName,
						Status:        "stopped",
						NotifyStopped: true,
					})
				}
				continue
			}
			status := "stopped"
			if state.Exists && state.HasLivePane {
				status = "running"
			}
			if tab.Status != status {
				updates = append(updates, tmuxTabStatusUpdate{
					SessionName:   tab.SessionName,
					Status:        status,
					NotifyStopped: status == "stopped",
				})
			}
		}
		if len(updates) == 0 {
			return nil
		}
		return tmuxTabsSyncResult{
			WorkspaceID: wsID,
			Updates:     updates,
		}
	}
}

type tmuxTabStatusUpdate struct {
	SessionName   string
	Status        string
	NotifyStopped bool
}

type tmuxTabsSyncResult struct {
	WorkspaceID string
	Updates     []tmuxTabStatusUpdate
}

// handleWorkspacePreviewed processes the WorkspacePreviewed message.
func (a *App) handleWorkspacePreviewed(msg messages.WorkspacePreviewed) []tea.Cmd {
	var cmds []tea.Cmd
	a.activeProject = msg.Project
	a.activeWorkspace = msg.Workspace
	a.showWelcome = false
	a.centerBtnFocused = false
	a.centerBtnIndex = 0
	a.center.SetWorkspace(msg.Workspace)
	a.sidebar.SetWorkspace(msg.Workspace)
	a.sidebarTerminal.SetWorkspacePreview(msg.Workspace)
	if msg.Workspace != nil {
		a.dashboard.MarkRead(string(msg.Workspace.ID()))
	}
	// Sync active workspaces to dashboard (fixes spinner race condition)
	if startCmd := a.syncActiveWorkspacesToDashboard(); startCmd != nil {
		cmds = append(cmds, startCmd)
	}
	if msg.Workspace != nil && a.statusManager != nil {
		if cached := a.statusManager.GetCached(msg.Workspace.Root); cached != nil {
			a.sidebar.SetGitStatus(cached)
		} else {
			a.sidebar.SetGitStatus(nil)
			a.dashboard.InvalidateStatus(msg.Workspace.Root)
		}
	} else {
		a.sidebar.SetGitStatus(nil)
	}

	newDashboard, cmd := a.dashboard.Update(msg)
	a.dashboard = newDashboard
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Ensure spinner starts if needed after sync
	if startCmd := a.dashboard.StartSpinnerIfNeeded(); startCmd != nil {
		cmds = append(cmds, startCmd)
	}

	return cmds
}

// handleShowAddProjectDialog shows the unified add project / create group file picker.
// If the user selects 1 repo, it's added as a project. If 2+, a group is created.
func (a *App) handleShowAddProjectDialog() {
	logging.Info("Showing Add Project file picker (unified)")
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/"
	}
	a.filePicker = common.NewFilePicker(DialogAddProject, home, true)
	a.filePicker.SetTitle("Add Workspace")
	a.filePicker.SetPrimaryActionLabel("Add repo")
	a.filePicker.SetMultiSelect(true)
	a.filePicker.SetValidatePath(func(path string, existing []string) string {
		if !git.IsGitRepository(path) {
			return "Not a git repository"
		}
		for _, p := range existing {
			if p == path {
				return "Already added"
			}
			if strings.HasPrefix(path, p+"/") {
				return "Nested inside " + filepath.Base(p)
			}
			if strings.HasPrefix(p, path+"/") {
				return "Contains already-added " + filepath.Base(p)
			}
		}
		return ""
	})
	a.filePicker.SetSize(a.width, a.height)
	a.filePicker.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.filePicker.Show()
}

// handleShowSetProfileDialog shows the set profile input dialog or profile
// picker if profiles already exist on disk.
func (a *App) handleShowSetProfileDialog(msg messages.ShowSetProfileDialog) {
	a.dialogProject = msg.Project
	currentProfile := ""
	if msg.Project != nil {
		currentProfile = msg.Project.Profile
	}

	profiles := a.listProfiles()
	if len(profiles) > 0 {
		a.dialog = common.NewProfilePicker(DialogSetProfile, profiles, currentProfile)
	} else {
		a.dialogDefaultName = "Default"
		a.dialog = common.NewInputDialog(DialogSetProfile, "Set Profile", "Default")
		a.dialog.SetMessage("Profile isolates Claude settings (permissions, memory) for this project.")
	}
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// profileHasActiveWorkspaces checks if any project using the given profile
// has workspaces with active sessions (running or detached agents).
func (a *App) profileHasActiveWorkspaces(profile string) bool {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return false
	}

	// Build a set of workspace roots that have running agents from the center model
	runningRoots := make(map[string]bool)
	for _, root := range a.center.GetRunningWorkspaceRoots() {
		runningRoots[root] = true
	}

	logging.Debug("profileHasActiveWorkspaces: checking profile=%q, runningRoots=%v", profile, runningRoots)

	for i := range a.projects {
		projectProfile := strings.TrimSpace(a.projects[i].Profile)
		if projectProfile != profile {
			continue
		}
		logging.Debug("profileHasActiveWorkspaces: found project %s with matching profile", a.projects[i].Name)

		for j := range a.projects[i].Workspaces {
			ws := &a.projects[i].Workspaces[j]
			wsID := string(ws.ID())

			logging.Debug("profileHasActiveWorkspaces: checking workspace %s (root=%s, wsID=%s)", ws.Name, ws.Root, wsID)

			// Check if workspace has running tabs in center (most reliable for current session)
			if runningRoots[ws.Root] {
				logging.Debug("profileHasActiveWorkspaces: workspace %s has running root", ws.Name)
				return true
			}

			// Check if center model has any tabs for this workspace
			hasTabs := a.center.HasTabsForWorkspace(wsID)
			logging.Debug("profileHasActiveWorkspaces: workspace %s hasTabs=%v", ws.Name, hasTabs)
			if hasTabs {
				// Further check if any of those tabs are running/not-stopped
				hasRunning := a.center.HasRunningTabsInWorkspace(wsID)
				logging.Debug("profileHasActiveWorkspaces: workspace %s hasRunning=%v", ws.Name, hasRunning)
				if hasRunning {
					return true
				}
			}

			// Check if workspace has live tabs (running/detached) via persisted state
			// This catches detached tmux sessions that aren't in the center model
			hasLive := workspaceHasLiveTabs(ws)
			logging.Debug("profileHasActiveWorkspaces: workspace %s hasLiveTabs=%v (OpenTabs=%d)", ws.Name, hasLive, len(ws.OpenTabs))
			if hasLive {
				return true
			}
		}
	}
	logging.Debug("profileHasActiveWorkspaces: no active workspaces found for profile %q", profile)
	return false
}

// listProfiles returns the names of existing profile directories,
// sorted alphabetically with the most recently used profile first.
func (a *App) listProfiles() []string {
	entries, err := os.ReadDir(a.config.Paths.ProfilesRoot)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "shared" {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	// Move the most recently used profile to the front.
	last := a.config.UI.LastProfile
	if last != "" {
		for i, name := range names {
			if name == last {
				names = append(names[:i], names[i+1:]...)
				names = append([]string{last}, names...)
				break
			}
		}
	}
	return names
}

// handleShowRenameProfileDialog shows the rename profile input dialog.
func (a *App) handleShowRenameProfileDialog(msg messages.ShowRenameProfileDialog) {
	a.dialogProfile = msg.Profile
	a.dialog = common.NewInputDialog(DialogRenameProfile, "Rename Profile", msg.Profile)
	a.dialog.SetMessage("Enter a new name for the profile.")
	a.dialog.SetInputValidate(func(s string) string {
		s = validation.SanitizeInput(s)
		if s == "" {
			return ""
		}
		if err := validation.ValidateProfileName(s); err != nil {
			return err.Error()
		}
		return ""
	})
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	a.dialog.SetValue(msg.Profile)
}

// handleRenameProfile renames a profile directory and updates all projects using it.
func (a *App) handleRenameProfile(msg messages.RenameProfile) tea.Cmd {
	oldName := strings.TrimSpace(msg.OldName)
	newName := strings.TrimSpace(msg.NewName)

	if oldName == "" || newName == "" || oldName == newName {
		return nil
	}

	// Check if any project using this profile has active workspaces
	if a.profileHasActiveWorkspaces(oldName) {
		return a.toast.ShowError("Cannot rename profile while workspaces have active sessions")
	}

	oldDir := filepath.Join(a.config.Paths.ProfilesRoot, oldName)
	newDir := filepath.Join(a.config.Paths.ProfilesRoot, newName)

	// Check if old profile exists
	if _, err := os.Stat(oldDir); os.IsNotExist(err) {
		return a.toast.ShowError("Profile not found: " + oldName)
	}

	// Check if new name already exists
	if _, err := os.Stat(newDir); err == nil {
		return a.toast.ShowError("Profile already exists: " + newName)
	}

	// Rename the profile directory
	if err := os.Rename(oldDir, newDir); err != nil {
		logging.Error("Failed to rename profile directory: %v", err)
		return a.toast.ShowError("Failed to rename profile: " + err.Error())
	}

	// Update all projects using this profile
	if err := a.registry.RenameProfile(oldName, newName); err != nil {
		logging.Error("Failed to update projects with renamed profile: %v", err)
		// Try to revert the directory rename
		_ = os.Rename(newDir, oldDir)
		return a.toast.ShowError("Failed to update projects: " + err.Error())
	}

	// Update last profile if it was the renamed one
	if a.config.UI.LastProfile == oldName {
		a.config.UI.LastProfile = newName
		_ = a.config.SaveUISettings()
	}

	// Update in-memory state
	for i := range a.projects {
		if a.projects[i].Profile == oldName {
			a.projects[i].Profile = newName
			for j := range a.projects[i].Workspaces {
				a.projects[i].Workspaces[j].Profile = newName
			}
		}
	}

	var cmds []tea.Cmd
	cmds = append(cmds, a.toast.ShowSuccess(fmt.Sprintf("Profile renamed to '%s'", newName)))
	cmds = append(cmds, a.loadProjects())
	// Re-show profile manager with updated list
	cmds = append(cmds, func() tea.Msg { return common.ShowProfileManager{} })
	return a.safeBatch(cmds...)
}

// handleShowCreateProfileDialog shows the create profile input dialog.
func (a *App) handleShowCreateProfileDialog() {
	a.dialog = common.NewInputDialog(DialogCreateProfile, "Create Profile", "")
	a.dialog.SetMessage("Enter a name for the new profile.")
	a.dialog.SetInputValidate(func(s string) string {
		s = validation.SanitizeInput(s)
		if s == "" {
			return ""
		}
		if err := validation.ValidateProfileName(s); err != nil {
			return err.Error()
		}
		// Check if profile already exists
		profileDir := filepath.Join(a.config.Paths.ProfilesRoot, s)
		if _, err := os.Stat(profileDir); err == nil {
			return "profile already exists"
		}
		return ""
	})
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleCreateProfile creates a new profile directory.
func (a *App) handleCreateProfile(msg messages.CreateProfile) tea.Cmd {
	name := strings.TrimSpace(msg.Name)
	if name == "" {
		return func() tea.Msg { return common.ShowProfileManager{} }
	}

	profileDir := filepath.Join(a.config.Paths.ProfilesRoot, name)

	// Check if profile already exists
	if _, err := os.Stat(profileDir); err == nil {
		return a.toast.ShowError("Profile already exists: " + name)
	}

	// Create the profile directory
	if err := os.MkdirAll(profileDir, 0755); err != nil {
		logging.Error("Failed to create profile directory: %v", err)
		return a.toast.ShowError("Failed to create profile: " + err.Error())
	}

	var cmds []tea.Cmd
	cmds = append(cmds, a.toast.ShowSuccess(fmt.Sprintf("Profile '%s' created", name)))
	// Re-show profile manager with updated list
	cmds = append(cmds, func() tea.Msg { return common.ShowProfileManager{} })
	return a.safeBatch(cmds...)
}

// handleShowDeleteProfileDialog shows the delete profile confirmation dialog.
func (a *App) handleShowDeleteProfileDialog(msg messages.ShowDeleteProfileDialog) {
	a.dialogProfile = msg.Profile
	a.dialog = common.NewConfirmDialog(
		DialogDeleteProfile,
		"Delete Profile",
		fmt.Sprintf("Delete profile '%s'? Projects using this profile will have their profile cleared.", msg.Profile),
	)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleDeleteProfile deletes a profile directory and clears it from all projects.
func (a *App) handleDeleteProfile(msg messages.DeleteProfile) tea.Cmd {
	profile := strings.TrimSpace(msg.Profile)
	if profile == "" {
		return nil
	}

	// Check if any project using this profile has active workspaces
	if a.profileHasActiveWorkspaces(profile) {
		return a.toast.ShowError("Cannot delete profile while workspaces have active sessions")
	}

	profileDir := filepath.Join(a.config.Paths.ProfilesRoot, profile)

	// Check if profile exists
	if _, err := os.Stat(profileDir); os.IsNotExist(err) {
		return a.toast.ShowError("Profile not found: " + profile)
	}

	// Clear the profile from all projects first
	if err := a.registry.ClearProfile(profile); err != nil {
		logging.Error("Failed to clear profile from projects: %v", err)
		return a.toast.ShowError("Failed to update projects: " + err.Error())
	}

	// Delete the profile directory
	if err := os.RemoveAll(profileDir); err != nil {
		logging.Error("Failed to delete profile directory: %v", err)
		return a.toast.ShowError("Failed to delete profile: " + err.Error())
	}

	// Clear last profile if it was the deleted one
	if a.config.UI.LastProfile == profile {
		a.config.UI.LastProfile = ""
		_ = a.config.SaveUISettings()
	}

	// Update in-memory state
	for i := range a.projects {
		if a.projects[i].Profile == profile {
			a.projects[i].Profile = ""
			for j := range a.projects[i].Workspaces {
				a.projects[i].Workspaces[j].Profile = ""
			}
		}
	}

	var cmds []tea.Cmd
	cmds = append(cmds, a.toast.ShowSuccess(fmt.Sprintf("Profile '%s' deleted", profile)))
	cmds = append(cmds, a.loadProjects())
	// Re-show profile manager with updated list
	cmds = append(cmds, func() tea.Msg { return common.ShowProfileManager{} })
	return a.safeBatch(cmds...)
}

// handleSetProfile persists a profile for a project and reloads.
func (a *App) handleSetProfile(msg messages.SetProfile) tea.Cmd {
	if msg.Project == nil {
		return nil
	}
	profile := strings.TrimSpace(msg.Profile)

	if err := a.registry.SetProfile(msg.Project.Path, profile); err != nil {
		logging.Error("Failed to set profile: %v", err)
		return a.toast.ShowError("Failed to set profile: " + err.Error())
	}

	// Create profile directory if non-empty and record it as the most
	// recently used profile so it sorts first in the picker.
	if profile != "" {
		profileDir := filepath.Join(a.config.Paths.ProfilesRoot, profile)
		if err := os.MkdirAll(profileDir, 0755); err != nil {
			logging.Warn("Failed to create profile directory: %v", err)
		}

		a.config.UI.LastProfile = profile
		if err := a.config.SaveUISettings(); err != nil {
			logging.Warn("Failed to save last profile: %v", err)
		}

		if a.config.UI.SyncProfilePlugins {
			_ = config.SyncProfileSharedDirs(a.config.Paths.ProfilesRoot, profile)
		}
	}

	// Update profile in-place on current projects so that any pending
	// LaunchAgent sees the new value without waiting for loadProjects.
	for i := range a.projects {
		if a.projects[i].Path == msg.Project.Path {
			a.projects[i].Profile = profile
			for j := range a.projects[i].Workspaces {
				a.projects[i].Workspaces[j].Profile = profile
			}
			break
		}
	}

	var cmds []tea.Cmd
	if profile != "" {
		cmds = append(cmds, a.toast.ShowSuccess(fmt.Sprintf("Profile set to '%s'", profile)))
	} else {
		cmds = append(cmds, a.toast.ShowSuccess("Profile cleared"))
	}
	cmds = append(cmds, a.loadProjects())

	// Resume a pending agent launch that was blocked on profile selection.
	if a.pendingProfileLaunch != "" && profile != "" {
		assistant := a.pendingProfileLaunch
		root := a.pendingProfileLaunchRoot
		a.pendingProfileLaunch = ""
		a.pendingProfileLaunchRoot = ""
		// Find the workspace by root in the (now-updated) projects.
		for i := range a.projects {
			for j := range a.projects[i].Workspaces {
				if a.projects[i].Workspaces[j].Root == root {
					ws := &a.projects[i].Workspaces[j]
					cmds = append(cmds, func() tea.Msg {
						return messages.LaunchAgent{Assistant: assistant, Workspace: ws}
					})
					goto foundPending
				}
			}
		}
	foundPending:
	} else {
		// Profile was empty or no pending launch — just clear state.
		a.pendingProfileLaunch = ""
		a.pendingProfileLaunchRoot = ""
	}

	return a.safeBatch(cmds...)
}

// handleShowRenameWorkspaceDialog shows the rename workspace dialog.
func (a *App) handleShowRenameWorkspaceDialog(msg messages.ShowRenameWorkspaceDialog) tea.Cmd {
	if msg.Workspace.IsPrimaryCheckout() {
		return a.toast.ShowError("Cannot rename the primary checkout")
	}
	if msg.Workspace.IsMainBranch() {
		return a.toast.ShowError("Cannot rename main/master branch")
	}
	a.dialogProject = msg.Project
	a.dialogWorkspace = msg.Workspace
	a.dialog = common.NewInputDialog(DialogRenameWorkspace, "Rename Worktree", msg.Workspace.Name)
	a.dialog.SetInputValidate(func(s string) string {
		s = validation.SanitizeInput(s)
		if s == "" {
			return ""
		}
		if err := validation.ValidateWorkspaceName(s); err != nil {
			return err.Error()
		}
		return ""
	})
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	a.dialog.SetValue(msg.Workspace.Name)
	return nil
}

// handleShowCreateWorkspaceDialog shows the create workspace dialog.
func (a *App) handleShowCreateWorkspaceDialog(msg messages.ShowCreateWorkspaceDialog) {
	a.dialogProject = msg.Project
	a.dialogDefaultName = generateWorkspaceName(msg.Project)
	a.dialog = common.NewInputDialog(DialogCreateWorkspace, "Create Worktree", a.dialogDefaultName)
	a.dialog.SetInputValidate(func(s string) string {
		s = validation.SanitizeInput(s)
		if s == "" {
			return "" // Don't show error for empty input
		}
		if err := validation.ValidateWorkspaceName(s); err != nil {
			return err.Error()
		}
		return ""
	})
	a.dialog.SetCheckbox("Immediately allow edits", a.config.UI.LastAllowEdits)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleShowDeleteWorkspaceDialog shows the delete workspace dialog.
func (a *App) handleShowDeleteWorkspaceDialog(msg messages.ShowDeleteWorkspaceDialog) {
	a.dialogProject = msg.Project
	a.dialogWorkspace = msg.Workspace
	a.dialog = common.NewConfirmDialog(
		DialogDeleteWorkspace,
		"Delete Worktree",
		fmt.Sprintf("Delete worktree '%s' and its branch?", msg.Workspace.Name),
	)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleShowRemoveProjectDialog shows the remove project dialog.
func (a *App) handleShowRemoveProjectDialog(msg messages.ShowRemoveProjectDialog) {
	a.dialogProject = msg.Project
	projectName := ""
	if msg.Project != nil {
		projectName = msg.Project.Name
	}
	a.dialog = common.NewConfirmDialog(
		DialogRemoveProject,
		"Remove Workspace",
		fmt.Sprintf("Remove workspace '%s' from MEDUSA? This won't delete any files.", projectName),
	)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleShowSelectAssistantDialog shows the select assistant dialog.
// If ForceDialog is false and a default agent is saved, it skips the picker
// and launches the agent directly.
func (a *App) handleShowSelectAssistantDialog(msg messages.ShowSelectAssistantDialog) tea.Cmd {
	if a.activeWorkspace == nil {
		return nil
	}
	// Use the saved default agent when not forcing the dialog
	if !msg.ForceDialog && a.config.UI.DefaultAgent != "" {
		ws := a.activeWorkspace
		agent := a.config.UI.DefaultAgent
		return func() tea.Msg {
			return messages.LaunchAgent{
				Assistant: agent,
				Workspace: ws,
			}
		}
	}
	a.dialog = common.NewAgentPicker()
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	return nil
}

// handleShowCleanupTmuxDialog shows the tmux cleanup dialog.
func (a *App) handleShowCleanupTmuxDialog() {
	if a.dialog != nil && a.dialog.Visible() {
		return
	}
	a.dialog = common.NewConfirmDialog(
		DialogCleanupTmux,
		"Cleanup tmux sessions",
		fmt.Sprintf("Kill all medusa-* tmux sessions on server %q?", a.tmuxOptions.ServerName),
	)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleShowSettingsDialog shows the settings dialog.
func (a *App) handleShowSettingsDialog() {
	a.settingsDialog = common.NewSettingsDialog(
		common.ThemeID(a.config.UI.Theme),
		a.config.UI.ShowKeymapHints,
		a.config.UI.HideSidebar,
		a.config.UI.AutoStartAgent,
		a.config.UI.SyncProfilePlugins,
		a.config.UI.GlobalPermissions,
		a.config.UI.AutoAddPermissions,
		a.config.UI.BellOnReady,
		a.config.UI.TmuxPersistence,
		a.config.UI.TmuxServer,
		a.config.UI.TmuxConfigPath,
		a.config.UI.TmuxSyncInterval,
	)
	a.settingsDialog.SetSize(a.width, a.height)
	a.settingsDialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)

	// Set update state
	if a.updateAvailable != nil {
		a.settingsDialog.SetUpdateInfo(
			a.updateAvailable.CurrentVersion,
			a.updateAvailable.LatestVersion,
			a.updateAvailable.UpdateAvailable,
		)
	} else {
		a.settingsDialog.SetUpdateInfo(a.version, "", false)
	}

	a.settingsDialog.Show()
}

// handleShowThemeEditor opens the theme selection dialog.
func (a *App) handleShowThemeEditor() {
	currentTheme := common.ThemeID(a.config.UI.Theme)
	if currentTheme == "" {
		currentTheme = common.ThemeGruvbox
	}
	a.themeDialog = common.NewThemeDialog(currentTheme)
	a.themeDialog.SetSize(a.width, a.height)
	a.themeDialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.themeDialog.Show()
}

// handleThemeResult handles theme dialog completion.
func (a *App) handleThemeResult(msg common.ThemeResult) tea.Cmd {
	a.themeDialog = nil
	if msg.Confirmed {
		// Apply and save theme
		common.SetCurrentTheme(msg.Theme)
		a.config.UI.Theme = string(msg.Theme)
		a.styles = common.DefaultStyles()
		// Propagate styles to all components
		a.dashboard.SetStyles(a.styles)
		a.sidebar.SetStyles(a.styles)
		a.sidebarTerminal.SetStyles(a.styles)
		a.center.SetStyles(a.styles)
		a.toast.SetStyles(a.styles)
		a.helpOverlay.SetStyles(a.styles)
		if a.filePicker != nil {
			a.filePicker.SetStyles(a.styles)
		}
		// Update settings dialog with new theme and re-show it
		if a.settingsDialog != nil {
			a.settingsDialog.SetTheme(msg.Theme)
			a.settingsDialog.Show()
		}
		// Save settings
		if err := a.config.SaveUISettings(); err != nil {
			return a.toast.ShowWarning("Failed to save theme")
		}
		return nil
	}
	// Cancelled - re-show settings dialog
	if a.settingsDialog != nil {
		a.settingsDialog.Show()
	}
	return nil
}

// handleThemePreview handles live theme preview.
func (a *App) handleThemePreview(msg common.ThemePreview) {
	// Live preview - apply theme without saving
	common.SetCurrentTheme(msg.Theme)
	a.styles = common.DefaultStyles()
	// Propagate styles to all components
	a.dashboard.SetStyles(a.styles)
	a.sidebar.SetStyles(a.styles)
	a.sidebarTerminal.SetStyles(a.styles)
	a.center.SetStyles(a.styles)
	a.toast.SetStyles(a.styles)
	a.helpOverlay.SetStyles(a.styles)
	if a.filePicker != nil {
		a.filePicker.SetStyles(a.styles)
	}
}

// handleSettingsResult handles settings dialog result.
func (a *App) handleSettingsResult(msg common.SettingsResult) tea.Cmd {
	a.settingsDialog = nil
	if msg.Confirmed {
		// Apply theme
		common.SetCurrentTheme(msg.Theme)
		a.config.UI.Theme = string(msg.Theme)
		a.styles = common.DefaultStyles()
		// Propagate styles to all components
		a.dashboard.SetStyles(a.styles)
		a.sidebar.SetStyles(a.styles)
		a.sidebarTerminal.SetStyles(a.styles)
		a.center.SetStyles(a.styles)
		a.toast.SetStyles(a.styles)
		a.helpOverlay.SetStyles(a.styles)
		if a.filePicker != nil {
			a.filePicker.SetStyles(a.styles)
		}

		// Apply keymap hints
		a.setKeymapHintsEnabled(msg.ShowKeymapHints)

		// Apply auto-start agent setting
		a.config.UI.AutoStartAgent = msg.AutoStartAgent

		// Apply sync profile plugins setting
		oldSync := a.config.UI.SyncProfilePlugins
		a.config.UI.SyncProfilePlugins = msg.SyncProfilePlugins
		if msg.SyncProfilePlugins && !oldSync {
			_ = config.SyncAllProfiles(a.config.Paths.ProfilesRoot)
		} else if !msg.SyncProfilePlugins && oldSync {
			_ = config.UnsyncAllProfiles(a.config.Paths.ProfilesRoot)
		}

		// Apply bell on ready setting
		a.config.UI.BellOnReady = msg.BellOnReady

		// Apply global permissions settings
		oldGlobalPerms := a.config.UI.GlobalPermissions
		a.config.UI.GlobalPermissions = msg.GlobalPermissions
		a.config.UI.AutoAddPermissions = msg.AutoAddPermissions

		// Apply sidebar hidden setting
		wasHidden := a.config.UI.HideSidebar
		a.config.UI.HideSidebar = msg.HideSidebar
		a.layout.SetSidebarHidden(msg.HideSidebar)
		// If sidebar is now hidden and focus is on it, move focus to center
		// Note: Terminal is now below center pane, so hiding sidebar doesn't affect it
		if msg.HideSidebar && a.focusedPane == messages.PaneSidebar {
			a.focusPane(messages.PaneCenter)
		}
		a.layout.Resize(a.width, a.height)
		a.updateLayout()

		// Tear down or spin up sidebar resources when visibility changes
		var sidebarCmds []tea.Cmd
		if msg.HideSidebar && !wasHidden {
			// Sidebar just hidden — close terminals and stop file watching
			a.sidebarTerminal.CloseAll()
			if a.fileWatcher != nil {
				a.unwatchAllWorkspaces()
			}
		} else if !msg.HideSidebar && wasHidden {
			// Sidebar just shown — start terminal and file watcher for active workspace
			if a.activeWorkspace != nil {
				if termCmd := a.sidebarTerminal.SetWorkspace(a.activeWorkspace); termCmd != nil {
					sidebarCmds = append(sidebarCmds, termCmd)
				}
				sidebarCmds = append(sidebarCmds, a.requestGitStatus(a.activeWorkspace.Root))
				if a.fileWatcher != nil {
					_ = a.fileWatcher.Watch(a.activeWorkspace.Root)
				}
			}
		}

		// Apply tmux settings
		oldServerName := a.tmuxOptions.ServerName
		tmuxPersistenceChanged := a.config.UI.TmuxPersistence != msg.TmuxPersistence
		a.config.UI.TmuxServer = msg.TmuxServer
		a.config.UI.TmuxConfigPath = msg.TmuxConfigPath
		a.config.UI.TmuxSyncInterval = msg.TmuxSyncInterval
		a.config.UI.TmuxPersistence = msg.TmuxPersistence
		applyTmuxEnvFromConfig(a.config, true)
		a.tmuxOptions = tmux.DefaultOptions() // Refresh cached options
		a.center.SetTmuxConfig(a.tmuxOptions.ServerName, a.tmuxOptions.ConfigPath)
		_ = tmux.SetStatusOff(a.tmuxOptions)

		// Handle global permissions toggle
		if msg.GlobalPermissions && !oldGlobalPerms {
			// Toggled ON: start permission watcher, inject into all profiles
			if a.permissionWatcher == nil {
				a.initPermissionWatcher()
			}
			sidebarCmds = append(sidebarCmds, a.startPermissionWatcher())
			a.watchAllWorkspacePermissions()
			global, err := config.LoadGlobalPermissions(a.config.Paths.GlobalPermissionsPath)
			if err == nil {
				_ = config.InjectIntoAllProfiles(a.config.Paths.ProfilesRoot, global)
			}
		} else if !msg.GlobalPermissions && oldGlobalPerms {
			// Toggled OFF: stop permission watcher
			a.unwatchAllWorkspacePermissions()
		}

		// Save settings
		if err := a.config.SaveUISettings(); err != nil {
			return a.toast.ShowWarning("Failed to save settings")
		}
		cmds := append(sidebarCmds, a.startTmuxSyncTicker(), a.toast.ShowSuccess("Settings saved"))
		if tmuxPersistenceChanged {
			cmds = append(cmds, a.toast.ShowInfo("Restart Medusa to apply tmux persistence change"))
		}
		// Clean up sessions on the old server if the server name changed
		if oldServerName != a.tmuxOptions.ServerName {
			oldOpts := tmux.Options{ServerName: oldServerName, CommandTimeout: 2 * time.Second}
			cmds = append(cmds, func() tea.Msg {
				_, _ = tmux.KillSessionsMatchingTags(map[string]string{"@medusa": "1"}, oldOpts)
				_ = tmux.KillSessionsWithPrefix("medusa-", oldOpts)
				return nil
			})
			cmds = append(cmds, a.toast.ShowInfo(fmt.Sprintf("Cleaned up sessions on old server %q", oldServerName)))
			cmds = append(cmds, a.resetAllTabStatuses()...)
			_ = tmux.SetMonitorActivityOn(a.tmuxOptions)
			_ = tmux.SetStatusOff(a.tmuxOptions)
		}
		return a.safeBatch(cmds...)
	}
	return nil
}

// handleCreateWorkspace handles the CreateWorkspace message.
func (a *App) handleCreateWorkspace(msg messages.CreateWorkspace) []tea.Cmd {
	var cmds []tea.Cmd
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

// handleGitStatusResult handles the GitStatusResult message.
func (a *App) handleGitStatusResult(msg messages.GitStatusResult) tea.Cmd {
	newDashboard, cmd := a.dashboard.Update(msg)
	a.dashboard = newDashboard
	if a.activeWorkspace != nil && msg.Root == a.activeWorkspace.Root {
		a.sidebar.SetGitStatus(msg.Status)
	}
	return cmd
}

// handleOpenDiff handles the OpenDiff message.
func (a *App) handleOpenDiff(msg messages.OpenDiff) tea.Cmd {
	logging.Info("Opening diff: %s", msg.File)
	newCenter, cmd := a.center.Update(msg)
	a.center = newCenter
	return cmd
}

// handleLaunchAgent handles the LaunchAgent message.
func (a *App) handleLaunchAgent(msg messages.LaunchAgent) tea.Cmd {
	logging.Info("Launching agent: %s", msg.Assistant)
	newCenter, cmd := a.center.Update(msg)
	a.center = newCenter
	return cmd
}

// handleTabCreated handles the TabCreated message.
func (a *App) handleTabCreated(msg messages.TabCreated) tea.Cmd {
	logging.Info("Tab created: %s", msg.Name)
	cmd := a.center.StartPTYReaders()
	// Only auto-focus center pane if a workspace is already active.
	// During startup, tabs are restored but we want to keep focus on dashboard
	// so users can browse their projects first.
	if a.activeWorkspace != nil {
		if a.monitorMode {
			a.focusPane(messages.PaneMonitor)
		} else {
			a.focusPane(messages.PaneCenter)
		}
	}
	return cmd
}

// workspaceHasLiveTabs checks persisted OpenTabs for any non-stopped tabs.
// This catches running/detached agents that haven't been restored into the
// center's tabsByWorkspace yet (restore is async).
func workspaceHasLiveTabs(ws *data.Workspace) bool {
	if ws == nil {
		return false
	}
	for _, tab := range ws.OpenTabs {
		if tab.Assistant == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(tab.Status), "stopped") {
			continue
		}
		return true
	}
	return false
}

// handleActionBarCopyDir copies the workspace directory to clipboard.
func (a *App) handleActionBarCopyDir(msg messages.ActionBarCopyDir) tea.Cmd {
	if msg.WorkspaceRoot == "" {
		return nil
	}
	if err := common.CopyToClipboard(msg.WorkspaceRoot); err != nil {
		logging.Error("Failed to copy to clipboard: %v", err)
		return a.toast.ShowError("Failed to copy to clipboard")
	}
	return a.toast.ShowSuccess("Copied directory to clipboard")
}

// handleActionBarOpenIDE opens the workspace folder in the user's IDE.
func (a *App) handleActionBarOpenIDE(msg messages.ActionBarOpenIDE) tea.Cmd {
	if msg.WorkspaceRoot == "" {
		return nil
	}

	// Get configured IDE or auto-detect
	ideCLI := ide.GetOrDetect(a.config.UI.IDE)
	if ideCLI == "" {
		return a.toast.ShowError("No IDE found. Install VS Code, Cursor, or configure 'ide' in settings.")
	}

	if err := ide.Open(ideCLI, msg.WorkspaceRoot); err != nil {
		logging.Error("Failed to open IDE: %v", err)
		return a.toast.ShowError("Failed to open " + ideCLI)
	}
	return a.toast.ShowSuccess("Opening in " + ideCLI)
}

// handleActionBarMergeToMain runs the merge operation asynchronously.
func (a *App) handleActionBarMergeToMain(msg messages.ActionBarMergeToMain) tea.Cmd {
	if msg.RepoPath == "" || msg.BranchName == "" {
		return nil
	}
	repoPath := msg.RepoPath
	branch := msg.BranchName
	return func() tea.Msg {
		err := git.MergeBranchToMain(repoPath, branch)
		return messages.ActionBarMergeResult{
			Success: err == nil,
			Err:     err,
		}
	}
}

// handleActionBarCommitResult handles the result of a commit operation.
func (a *App) handleActionBarCommitResult(msg messages.ActionBarCommitResult) tea.Cmd {
	if !msg.Success {
		errMsg := "Commit failed"
		if msg.Err != nil {
			errMsg = "Commit failed: " + msg.Err.Error()
		}
		return a.toast.ShowError(errMsg)
	}
	return a.toast.ShowSuccess(fmt.Sprintf("Committed: %s", msg.CommitHash))
}

// handleActionBarMergeResult handles the result of a merge operation.
func (a *App) handleActionBarMergeResult(msg messages.ActionBarMergeResult) tea.Cmd {
	if !msg.Success {
		errMsg := "Merge failed"
		if msg.Err != nil {
			errMsg = "Merge failed: " + msg.Err.Error()
		}
		return a.toast.ShowError(errMsg)
	}
	return a.toast.ShowSuccess("Merged to main")
}

// handleActionBarOpenMR opens the browser to create a merge/pull request.
func (a *App) handleActionBarOpenMR(msg messages.ActionBarOpenMR) tea.Cmd {
	if msg.WorkspaceRoot == "" || msg.BranchName == "" {
		return nil
	}
	root := msg.WorkspaceRoot
	branch := msg.BranchName
	return func() tea.Msg {
		url, err := git.GetPRURL(root, branch)
		if err != nil {
			logging.Error("Failed to get PR URL: %v", err)
			return messages.Toast{
				Message: "Failed to get PR URL: " + err.Error(),
				Level:   messages.ToastError,
			}
		}
		if err := openBrowser(url); err != nil {
			logging.Error("Failed to open browser: %v", err)
			return messages.Toast{
				Message: "Failed to open browser",
				Level:   messages.ToastError,
			}
		}
		return messages.Toast{
			Message: "Opened PR page in browser",
			Level:   messages.ToastSuccess,
		}
	}
}

// handleShowCommitDialog shows the commit message dialog.
func (a *App) handleShowCommitDialog(msg messages.ShowCommitDialog) {
	a.dialogWorkspaceRoot = msg.WorkspaceRoot
	a.dialog = common.NewInputDialog(DialogCommit, "Commit Message", "")
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleShowCreateGroupDialog redirects to the unified add project flow.
func (a *App) handleShowCreateGroupDialog() {
	a.handleShowAddProjectDialog()
}

// groupHasActiveSessions checks if any workspace in the group has an active agent session.
func (a *App) groupHasActiveSessions(group *data.ProjectGroup) bool {
	activeIDs := make(map[string]bool)
	for _, wsID := range a.center.GetActiveWorkspaceIDs() {
		activeIDs[wsID] = true
	}
	for wsID := range a.tmuxActiveWorkspaceIDs {
		activeIDs[wsID] = true
	}
	for i := range group.Workspaces {
		gw := &group.Workspaces[i]
		if activeIDs[string(gw.ID())] {
			return true
		}
		if workspaceHasLiveTabs(&gw.Primary) {
			return true
		}
	}
	return false
}

// projectHasActiveSessions checks if any workspace in the project has an active agent session.
func (a *App) projectHasActiveSessions(project *data.Project) bool {
	if project == nil {
		return false
	}
	activeIDs := make(map[string]bool)
	for _, wsID := range a.center.GetActiveWorkspaceIDs() {
		activeIDs[wsID] = true
	}
	for wsID := range a.tmuxActiveWorkspaceIDs {
		activeIDs[wsID] = true
	}
	for i := range project.Workspaces {
		ws := &project.Workspaces[i]
		if activeIDs[string(ws.ID())] {
			return true
		}
		if a.center.HasRunningTabsInWorkspace(string(ws.ID())) {
			return true
		}
		if workspaceHasLiveTabs(ws) {
			return true
		}
	}
	return false
}

// handleShowEditGroupReposDialog shows the multi-select file picker for editing repos in a group.
// Pre-populates with existing repo paths.
func (a *App) handleShowEditGroupReposDialog(group *data.ProjectGroup) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/"
	}
	a.dialogGroup = group
	a.filePicker = common.NewFilePicker(DialogAddGroupRepo, home, true)
	a.filePicker.SetTitle("Edit Workspace Repos")
	a.filePicker.SetPrimaryActionLabel("Add repo")
	a.filePicker.SetMultiSelect(true)
	// Pre-populate with existing repos
	for _, repo := range group.Repos {
		a.filePicker.AddSelectedPath(repo.Path)
	}
	a.filePicker.SetValidatePath(func(path string, existing []string) string {
		if !git.IsGitRepository(path) {
			return "Not a git repository"
		}
		for _, p := range existing {
			if p == path {
				return "Already added"
			}
			if strings.HasPrefix(path, p+"/") {
				return "Nested inside " + filepath.Base(p)
			}
			if strings.HasPrefix(p, path+"/") {
				return "Contains already-added " + filepath.Base(p)
			}
		}
		return ""
	})
	a.filePicker.SetSize(a.width, a.height)
	a.filePicker.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.filePicker.Show()
}

// handleShowCreateGroupWorkspaceDialog shows the group workspace creation dialog.
func (a *App) handleShowCreateGroupWorkspaceDialog(msg messages.ShowCreateGroupWorkspaceDialog) {
	a.dialogGroup = msg.Group
	a.dialogDefaultName = generateGroupWorkspaceName(msg.Group)
	a.dialog = common.NewInputDialog(DialogCreateGroupWorkspace, "Create Worktree", a.dialogDefaultName)
	a.dialog.SetInputValidate(func(s string) string {
		s = validation.SanitizeInput(s)
		if s == "" {
			return ""
		}
		if err := validation.ValidateWorkspaceName(s); err != nil {
			return err.Error()
		}
		return ""
	})
	a.dialog.SetCheckbox("Immediately allow edits", a.config.UI.LastAllowEdits)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleShowDeleteGroupDialog shows the group delete confirmation.
func (a *App) handleShowDeleteGroupDialog(msg messages.ShowDeleteGroupDialog) {
	a.dialogGroupName = msg.GroupName
	a.dialog = common.NewConfirmDialog(
		DialogDeleteGroup,
		"Delete Workspace",
		fmt.Sprintf("Delete workspace '%s' and all its worktrees?", msg.GroupName),
	)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleShowRenameGroupDialog shows the rename dialog for a project group.
func (a *App) handleShowRenameGroupDialog(msg messages.ShowRenameGroupDialog) {
	a.dialogGroup = msg.Group
	a.dialog = common.NewInputDialog(DialogRenameGroup, "Rename Workspace", msg.Group.Name)
	a.dialog.SetInputValidate(func(s string) string {
		s = validation.SanitizeInput(s)
		if s == "" {
			return ""
		}
		if err := validation.ValidateWorkspaceName(s); err != nil {
			return err.Error()
		}
		return ""
	})
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	a.dialog.SetValue(msg.Group.Name)
}

// handleShowRenameGroupWorkspaceDialog shows the rename dialog for a group workspace.
func (a *App) handleShowRenameGroupWorkspaceDialog(msg messages.ShowRenameGroupWorkspaceDialog) {
	a.dialogGroup = msg.Group
	a.dialogGroupWs = msg.Workspace
	a.dialog = common.NewInputDialog(DialogRenameGroupWorkspace, "Rename Worktree", msg.Workspace.Name)
	a.dialog.SetInputValidate(func(s string) string {
		s = validation.SanitizeInput(s)
		if s == "" {
			return ""
		}
		if err := validation.ValidateWorkspaceName(s); err != nil {
			return err.Error()
		}
		return ""
	})
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	a.dialog.SetValue(msg.Workspace.Name)
}

// handleShowDeleteGroupWorkspaceDialog shows the group workspace delete confirmation.
func (a *App) handleShowDeleteGroupWorkspaceDialog(msg messages.ShowDeleteGroupWorkspaceDialog) {
	a.dialogGroup = msg.Group
	a.dialogGroupWs = msg.Workspace
	a.dialog = common.NewConfirmDialog(
		DialogDeleteGroupWorkspace,
		"Delete Worktree",
		fmt.Sprintf("Delete worktree '%s' and its branches?", msg.Workspace.Name),
	)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleShowSetGroupProfileDialog shows the group profile picker.
func (a *App) handleShowSetGroupProfileDialog(msg messages.ShowSetGroupProfileDialog) {
	a.dialogGroup = msg.Group
	currentProfile := ""
	if msg.Group != nil {
		currentProfile = msg.Group.Profile
	}

	profiles := a.listProfiles()
	if len(profiles) > 0 {
		a.dialog = common.NewProfilePicker(DialogSetGroupProfile, profiles, currentProfile)
	} else {
		a.dialogDefaultName = "Default"
		a.dialog = common.NewInputDialog(DialogSetGroupProfile, "Set Profile", "Default")
		a.dialog.SetMessage("Profile isolates Claude settings for this group.")
	}
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleGroupWorkspaceActivated processes the GroupWorkspaceActivated message.
func (a *App) handleGroupWorkspaceActivated(msg messages.GroupWorkspaceActivated) []tea.Cmd {
	var cmds []tea.Cmd
	a.activeGroup = msg.Group
	a.activeGroupWs = msg.Workspace
	a.activeProject = nil
	a.showWelcome = false
	a.centerBtnFocused = false
	a.centerBtnIndex = 0

	// Ensure profile is propagated to inner workspace
	if msg.Workspace.Primary.Profile == "" && msg.Workspace.Profile != "" {
		msg.Workspace.Primary.Profile = msg.Workspace.Profile
	}

	// Pass primary workspace to center and sidebar
	a.activeWorkspace = &msg.Workspace.Primary
	a.center.SetWorkspace(&msg.Workspace.Primary)
	a.sidebar.SetWorkspace(&msg.Workspace.Primary)
	a.dashboard.MarkRead(string(msg.Workspace.Primary.ID()))

	// Set up file watching and git status for each repo worktree
	if !a.layout.SidebarHidden() {
		for _, root := range msg.Workspace.AllRoots() {
			if a.fileWatcher != nil {
				_ = a.fileWatcher.Watch(root)
			}
			cmds = append(cmds, a.requestGitStatus(root))
		}
	}

	// Restore tabs
	if restoreCmd := a.center.RestoreTabsFromWorkspace(&msg.Workspace.Primary); restoreCmd != nil {
		cmds = append(cmds, restoreCmd)
	}

	// Set up sidebar terminal
	if !a.layout.SidebarHidden() {
		if termCmd := a.sidebarTerminal.SetWorkspace(&msg.Workspace.Primary); termCmd != nil {
			cmds = append(cmds, termCmd)
		}
	}

	// Auto-start agent for newly created group workspaces
	ws := &msg.Workspace.Primary
	wsID := string(ws.ID())
	if !a.center.HasTabsForWorkspace(wsID) && !workspaceHasLiveTabs(ws) {
		if a.config.UI.DefaultAgent != "" {
			agent := a.config.UI.DefaultAgent
			cmds = append(cmds, func() tea.Msg {
				return messages.LaunchAgent{Assistant: agent, Workspace: ws}
			})
		} else {
			cmds = append(cmds, func() tea.Msg {
				return messages.ShowSelectAssistantDialog{}
			})
		}
	}

	// Focus center pane when workspace has active tabs
	if a.center.HasTabsForWorkspace(wsID) || workspaceHasLiveTabs(ws) {
		a.focusPane(messages.PaneCenter)
	}

	return cmds
}

// handleGroupWorkspacePreviewed processes the GroupWorkspacePreviewed message.
func (a *App) handleGroupWorkspacePreviewed(msg messages.GroupWorkspacePreviewed) []tea.Cmd {
	var cmds []tea.Cmd
	a.activeGroup = msg.Group
	a.activeGroupWs = msg.Workspace
	a.activeProject = nil
	a.showWelcome = false
	a.centerBtnFocused = false
	a.centerBtnIndex = 0

	// Ensure profile is propagated to inner workspace
	if msg.Workspace.Primary.Profile == "" && msg.Workspace.Profile != "" {
		msg.Workspace.Primary.Profile = msg.Workspace.Profile
	}

	a.activeWorkspace = &msg.Workspace.Primary
	a.center.SetWorkspace(&msg.Workspace.Primary)
	a.sidebar.SetWorkspace(&msg.Workspace.Primary)
	a.sidebarTerminal.SetWorkspacePreview(&msg.Workspace.Primary)
	a.dashboard.MarkRead(string(msg.Workspace.Primary.ID()))

	if msg.Workspace.Primary.Root != "" && a.statusManager != nil {
		if cached := a.statusManager.GetCached(msg.Workspace.Primary.Root); cached != nil {
			a.sidebar.SetGitStatus(cached)
		} else {
			a.sidebar.SetGitStatus(nil)
		}
	} else {
		a.sidebar.SetGitStatus(nil)
	}

	return cmds
}

// handleSetGroupProfile persists a profile for a group and reloads.
func (a *App) handleSetGroupProfile(msg messages.SetGroupProfile) tea.Cmd {
	profile := strings.TrimSpace(msg.Profile)

	if err := a.registry.SetGroupProfile(msg.GroupName, profile); err != nil {
		logging.Error("Failed to set group profile: %v", err)
		return a.toast.ShowError("Failed to set profile: " + err.Error())
	}

	if profile != "" {
		profileDir := filepath.Join(a.config.Paths.ProfilesRoot, profile)
		if err := os.MkdirAll(profileDir, 0755); err != nil {
			logging.Warn("Failed to create profile directory: %v", err)
		}
		a.config.UI.LastProfile = profile
		_ = a.config.SaveUISettings()
	}

	// Update in-memory state
	for i := range a.groups {
		if a.groups[i].Name == msg.GroupName {
			a.groups[i].Profile = profile
			for j := range a.groups[i].Workspaces {
				a.groups[i].Workspaces[j].Profile = profile
			}
			break
		}
	}

	var cmds []tea.Cmd
	if profile != "" {
		cmds = append(cmds, a.toast.ShowSuccess(fmt.Sprintf("Group profile set to '%s'", profile)))
	} else {
		cmds = append(cmds, a.toast.ShowSuccess("Group profile cleared"))
	}
	cmds = append(cmds, a.loadGroups())
	return a.safeBatch(cmds...)
}

// handleGroupPreviewed processes the GroupPreviewed message.
// Shows group info in the center pane when the group header is highlighted.
func (a *App) handleGroupPreviewed(msg messages.GroupPreviewed) []tea.Cmd {
	a.activeGroup = msg.Group
	a.activeGroupWs = nil
	a.activeProject = nil
	a.activeWorkspace = nil
	a.showWelcome = false
	a.centerBtnFocused = false
	a.centerBtnIndex = 0
	a.center.SetWorkspace(nil)
	a.sidebar.SetWorkspace(nil)
	a.sidebar.SetGitStatus(nil)
	return nil
}

// handleUpdateGroupRepos handles the UpdateGroupRepos message.
func (a *App) handleUpdateGroupRepos(msg messages.UpdateGroupRepos) tea.Cmd {
	if msg.Group == nil || len(msg.RepoPaths) < 2 {
		return a.toast.ShowError("A group needs at least 2 repos")
	}
	repos := make([]data.GroupRepo, len(msg.RepoPaths))
	for i, p := range msg.RepoPaths {
		repos[i] = data.GroupRepo{
			Path: p,
			Name: filepath.Base(p),
		}
	}
	groupName := msg.Group.Name
	return func() tea.Msg {
		if err := a.registry.UpdateGroupRepos(groupName, repos); err != nil {
			return messages.Error{Err: err, Context: "updating group repos"}
		}
		return messages.GroupReposUpdated{GroupName: groupName}
	}
}

// handleLaunchGroupAgent launches an agent for a group workspace.
func (a *App) handleLaunchGroupAgent(msg messages.LaunchGroupAgent) tea.Cmd {
	if msg.Workspace == nil {
		return nil
	}
	// Pass as regular LaunchAgent using the primary workspace
	ws := &msg.Workspace.Primary
	newCenter, cmd := a.center.Update(messages.LaunchAgent{
		Assistant: msg.Assistant,
		Workspace: ws,
	})
	a.center = newCenter
	return cmd
}

// openBrowser opens a URL in the default browser.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported platform")
	}
	return cmd.Start()
}
