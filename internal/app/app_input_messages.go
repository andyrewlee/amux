package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/validation"
)

// handleProjectsLoaded processes the ProjectsLoaded message.
func (a *App) handleProjectsLoaded(msg messages.ProjectsLoaded) []tea.Cmd {
	a.projects = msg.Projects
	a.projectsLoaded = true
	a.dashboard.SetProjects(a.projects)
	// Request git status for all workspaces
	var cmds []tea.Cmd
	cmds = append(cmds, a.scanTmuxActivityNow())
	if gcCmd := a.gcOrphanedTmuxSessions(); gcCmd != nil {
		cmds = append(cmds, gcCmd)
	}
	if gcCmd := a.gcStaleTerminalSessions(); gcCmd != nil {
		cmds = append(cmds, gcCmd)
	}
	for i := range a.projects {
		for j := range a.projects[i].Workspaces {
			ws := &a.projects[i].Workspaces[j]
			cmds = append(cmds, a.requestGitStatus(ws.Root))
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
	a.sidebarTerminal.SetWorkspacePreview(msg.Workspace)
	// Discover shared tmux tabs first; restore/sync happens below.
	if discoverCmd := a.discoverWorkspaceTabsFromTmux(msg.Workspace); discoverCmd != nil {
		cmds = append(cmds, discoverCmd)
	}
	if discoverTermCmd := a.discoverSidebarTerminalsFromTmux(msg.Workspace); discoverTermCmd != nil {
		cmds = append(cmds, discoverTermCmd)
	}
	if syncCmd := a.syncWorkspaceTabsFromTmux(msg.Workspace); syncCmd != nil {
		cmds = append(cmds, syncCmd)
	}
	if restoreCmd := a.center.RestoreTabsFromWorkspace(msg.Workspace); restoreCmd != nil {
		cmds = append(cmds, restoreCmd)
	}
	// Sync active workspaces to dashboard (fixes spinner race condition)
	a.syncActiveWorkspacesToDashboard()
	newDashboard, cmd := a.dashboard.Update(msg)
	a.dashboard = newDashboard
	cmds = append(cmds, cmd)

	// Refresh git status for sidebar
	if msg.Workspace != nil {
		cmds = append(cmds, a.requestGitStatus(msg.Workspace.Root))
		// Set up file watching for this workspace
		if a.fileWatcher != nil {
			_ = a.fileWatcher.Watch(msg.Workspace.Root)
		}
	}
	// Ensure spinner starts if needed after sync
	if startCmd := a.dashboard.StartSpinnerIfNeeded(); startCmd != nil {
		cmds = append(cmds, startCmd)
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
	svc := a.tmuxService
	return func() tea.Msg {
		if svc == nil {
			return tmuxTabsSyncResult{WorkspaceID: wsID}
		}

		var updates []tmuxTabStatusUpdate
		for _, tab := range tabsSnapshot {
			if tab.SessionName == "" {
				continue
			}
			state, err := svc.SessionStateFor(tab.SessionName, opts)
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
	a.syncActiveWorkspacesToDashboard()
	if msg.Workspace != nil && a.gitStatus != nil {
		if cached := a.gitStatus.GetCached(msg.Workspace.Root); cached != nil {
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

// handleShowCreateWorkspaceDialog shows the create workspace dialog.
func (a *App) handleShowCreateWorkspaceDialog(msg messages.ShowCreateWorkspaceDialog) {
	a.dialogProject = msg.Project
	a.dialog = common.NewInputDialog(DialogCreateWorkspace, "Create Workspace", "Enter workspace name...")
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
func (a *App) handleShowSelectAssistantDialog() {
	if a.activeWorkspace != nil {
		a.dialog = common.NewAgentPicker()
		a.dialog.SetSize(a.width, a.height)
		a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
		a.dialog.Show()
	}
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
	if a.updateService != nil && a.updateService.IsHomebrewBuild() {
		a.settingsDialog.SetUpdateHint("Installed via Homebrew - update with brew upgrade amux")
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

		// Apply tmux settings
		oldServerName := a.tmuxOptions.ServerName
		a.config.UI.TmuxServer = msg.TmuxServer
		a.config.UI.TmuxConfigPath = msg.TmuxConfigPath
		a.config.UI.TmuxSyncInterval = msg.TmuxSyncInterval
		applyTmuxEnvFromConfig(a.config, true)
		a.tmuxOptions = tmux.DefaultOptions() // Refresh cached options
		a.center.SetTmuxConfig(a.tmuxOptions.ServerName, a.tmuxOptions.ConfigPath)
		a.sidebarTerminal.SetTmuxConfig(a.tmuxOptions.ServerName, a.tmuxOptions.ConfigPath)

		// Save settings
		if err := a.config.SaveUISettings(); err != nil {
			return a.toast.ShowWarning("Failed to save settings")
		}
		cmds := []tea.Cmd{a.startTmuxSyncTicker(), a.toast.ShowSuccess("Settings saved")}
		if a.tmuxService != nil {
			cmds = append(cmds, func() tea.Msg {
				_ = a.tmuxService.SetStatusOff(a.tmuxOptions)
				return nil
			})
		}
		// Clean up sessions on the old server if the server name changed
		if oldServerName != a.tmuxOptions.ServerName {
			oldOpts := tmux.Options{ServerName: oldServerName, CommandTimeout: tmuxCommandTimeout}
			if a.tmuxService != nil {
				cmds = append(cmds, func() tea.Msg {
					_, _ = a.tmuxService.KillSessionsMatchingTags(map[string]string{"@amux": "1"}, oldOpts)
					_ = a.tmuxService.KillSessionsWithPrefix("amux-", oldOpts)
					return nil
				})
				cmds = append(cmds, func() tea.Msg {
					_ = a.tmuxService.SetMonitorActivityOn(a.tmuxOptions)
					_ = a.tmuxService.SetStatusOff(a.tmuxOptions)
					return nil
				})
			}
			cmds = append(cmds, a.toast.ShowInfo(fmt.Sprintf("Cleaned up sessions on old server %q", oldServerName)))
			cmds = append(cmds, a.resetAllTabStatuses()...)
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
		if pending != nil {
			a.creatingWorkspaceIDs[string(pending.ID())] = true
		}
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
	a.focusPane(messages.PaneCenter)
	return cmd
}
