package app

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

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/ide"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/validation"
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

// handleShowAddProjectDialog shows the add project file picker.
func (a *App) handleShowAddProjectDialog() {
	logging.Info("Showing Add Project file picker")
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/"
	}
	a.filePicker = common.NewFilePicker(DialogAddProject, home, true)
	a.filePicker.SetTitle("Add Project")
	a.filePicker.SetPrimaryActionLabel("Add as project")
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
func (a *App) handleShowRenameWorkspaceDialog(msg messages.ShowRenameWorkspaceDialog) {
	a.dialogProject = msg.Project
	a.dialogWorkspace = msg.Workspace
	a.dialog = common.NewInputDialog(DialogRenameWorkspace, "Rename Session", msg.Workspace.Name)
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

// handleShowCreateWorkspaceDialog shows the create workspace dialog.
func (a *App) handleShowCreateWorkspaceDialog(msg messages.ShowCreateWorkspaceDialog) {
	a.dialogProject = msg.Project
	a.dialogDefaultName = generateWorkspaceName(msg.Project)
	a.dialog = common.NewInputDialog(DialogCreateWorkspace, "Create Session", a.dialogDefaultName)
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
		"Delete Workspace",
		fmt.Sprintf("Delete workspace '%s' and its branch?", msg.Workspace.Name),
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
		"Remove Project",
		fmt.Sprintf("Remove project '%s' from AMUX? This won't delete any files.", projectName),
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
		fmt.Sprintf("Kill all amux-* tmux sessions on server %q?", a.tmuxOptions.ServerName),
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

		// Apply global permissions settings
		oldGlobalPerms := a.config.UI.GlobalPermissions
		a.config.UI.GlobalPermissions = msg.GlobalPermissions
		a.config.UI.AutoAddPermissions = msg.AutoAddPermissions

		// Apply sidebar hidden setting
		wasHidden := a.config.UI.HideSidebar
		a.config.UI.HideSidebar = msg.HideSidebar
		a.layout.SetSidebarHidden(msg.HideSidebar)
		// If sidebar is now hidden and focus is on it, move focus to center
		if msg.HideSidebar && (a.focusedPane == messages.PaneSidebar || a.focusedPane == messages.PaneSidebarTerminal) {
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
			cmds = append(cmds, a.toast.ShowInfo("Restart amux to apply tmux persistence change"))
		}
		// Clean up sessions on the old server if the server name changed
		if oldServerName != a.tmuxOptions.ServerName {
			oldOpts := tmux.Options{ServerName: oldServerName, CommandTimeout: 2 * time.Second}
			cmds = append(cmds, func() tea.Msg {
				_, _ = tmux.KillSessionsMatchingTags(map[string]string{"@amux": "1"}, oldOpts)
				_ = tmux.KillSessionsWithPrefix("amux-", oldOpts)
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
	cmds = append(cmds, a.createWorkspace(msg.Project, msg.Name, msg.Base))
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
	if a.monitorMode {
		a.focusPane(messages.PaneMonitor)
	} else {
		a.focusPane(messages.PaneCenter)
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
