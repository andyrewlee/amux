package app

import (
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
	"github.com/andyrewlee/amux/internal/update"
	"github.com/andyrewlee/amux/internal/validation"
)

// handleProjectsLoaded processes the ProjectsLoaded message.
func (a *App) handleProjectsLoaded(msg messages.ProjectsLoaded) []tea.Cmd {
	a.projects = msg.Projects
	a.dashboard.SetProjects(a.projects)
	// Request git status for all worktrees
	var cmds []tea.Cmd
	for i := range a.projects {
		for j := range a.projects[i].Worktrees {
			wt := &a.projects[i].Worktrees[j]
			cmds = append(cmds, a.requestGitStatus(wt.Root))
		}
	}
	return cmds
}

// handleWorktreeActivated processes the WorktreeActivated message.
func (a *App) handleWorktreeActivated(msg messages.WorktreeActivated) []tea.Cmd {
	var cmds []tea.Cmd
	// Tabs now persist in memory per-worktree, no need to save/restore from disk
	a.activeProject = msg.Project
	a.activeWorktree = msg.Worktree
	a.showWelcome = false
	a.centerBtnFocused = false
	a.centerBtnIndex = 0
	a.center.SetWorktree(msg.Worktree)
	a.sidebar.SetWorktree(msg.Worktree)
	// Set up sidebar terminal for the worktree
	if termCmd := a.sidebarTerminal.SetWorktree(msg.Worktree); termCmd != nil {
		cmds = append(cmds, termCmd)
	}
	newDashboard, cmd := a.dashboard.Update(msg)
	a.dashboard = newDashboard
	cmds = append(cmds, cmd)

	// Refresh git status for sidebar
	if msg.Worktree != nil {
		cmds = append(cmds, a.requestGitStatus(msg.Worktree.Root))
		// Set up file watching for this worktree
		if a.fileWatcher != nil {
			_ = a.fileWatcher.Watch(msg.Worktree.Root)
		}
	}
	return cmds
}

// handleWorktreePreviewed processes the WorktreePreviewed message.
func (a *App) handleWorktreePreviewed(msg messages.WorktreePreviewed) []tea.Cmd {
	var cmds []tea.Cmd
	a.activeProject = msg.Project
	a.activeWorktree = msg.Worktree
	a.showWelcome = false
	a.centerBtnFocused = false
	a.centerBtnIndex = 0
	a.center.SetWorktree(msg.Worktree)
	a.sidebar.SetWorktree(msg.Worktree)
	a.sidebarTerminal.SetWorktreePreview(msg.Worktree)
	if msg.Worktree != nil && a.statusManager != nil {
		if cached := a.statusManager.GetCached(msg.Worktree.Root); cached != nil {
			a.sidebar.SetGitStatus(cached)
		} else {
			a.sidebar.SetGitStatus(nil)
		}
	} else {
		a.sidebar.SetGitStatus(nil)
	}

	newDashboard, cmd := a.dashboard.Update(msg)
	a.dashboard = newDashboard
	if cmd != nil {
		cmds = append(cmds, cmd)
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

// handleShowCreateWorktreeDialog shows the create worktree dialog.
func (a *App) handleShowCreateWorktreeDialog(msg messages.ShowCreateWorktreeDialog) {
	a.dialogProject = msg.Project
	a.dialog = common.NewInputDialog(DialogCreateWorktree, "Create Worktree", "Enter worktree name...")
	a.dialog.SetInputValidate(func(s string) string {
		s = validation.SanitizeInput(s)
		if s == "" {
			return "" // Don't show error for empty input
		}
		if err := validation.ValidateWorktreeName(s); err != nil {
			return err.Error()
		}
		return ""
	})
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleShowDeleteWorktreeDialog shows the delete worktree dialog.
func (a *App) handleShowDeleteWorktreeDialog(msg messages.ShowDeleteWorktreeDialog) {
	a.dialogProject = msg.Project
	a.dialogWorktree = msg.Worktree
	a.dialog = common.NewConfirmDialog(
		DialogDeleteWorktree,
		"Delete Worktree",
		fmt.Sprintf("Delete worktree '%s' and its branch?", msg.Worktree.Name),
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
	if a.activeWorktree != nil {
		a.dialog = common.NewAgentPicker()
		a.dialog.SetSize(a.width, a.height)
		a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
		a.dialog.Show()
	}
}

// handleShowSettingsDialog shows the settings dialog.
func (a *App) handleShowSettingsDialog() {
	a.settingsDialog = common.NewSettingsDialog(
		common.ThemeID(a.config.UI.Theme),
		a.config.UI.ShowKeymapHints,
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

		// Save settings
		if err := a.config.SaveUISettings(); err != nil {
			return a.toast.ShowWarning("Failed to save settings")
		}
	}
	return nil
}

// handleCreateWorktree handles the CreateWorktree message.
func (a *App) handleCreateWorktree(msg messages.CreateWorktree) []tea.Cmd {
	var cmds []tea.Cmd
	if msg.Project != nil && msg.Name != "" {
		worktreePath := filepath.Join(
			a.config.Paths.WorktreesRoot,
			msg.Project.Name,
			msg.Name,
		)
		pending := data.NewWorktree(msg.Name, msg.Name, msg.Base, msg.Project.Path, worktreePath)
		if cmd := a.dashboard.SetWorktreeCreating(pending, true); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	cmds = append(cmds, a.createWorktree(msg.Project, msg.Name, msg.Base))
	return cmds
}

// handleDeleteWorktree handles the DeleteWorktree message.
func (a *App) handleDeleteWorktree(msg messages.DeleteWorktree) []tea.Cmd {
	var cmds []tea.Cmd
	if msg.Worktree != nil {
		if cmd := a.dashboard.SetWorktreeDeleting(msg.Worktree.Root, true); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	cmds = append(cmds, a.deleteWorktree(msg.Project, msg.Worktree))
	return cmds
}

// handleWorktreeCreatedWithWarning handles the WorktreeCreatedWithWarning message.
func (a *App) handleWorktreeCreatedWithWarning(msg messages.WorktreeCreatedWithWarning) []tea.Cmd {
	var cmds []tea.Cmd
	// Worktree was created but setup had issues - still refresh and show warning
	a.err = fmt.Errorf("worktree created with warning: %s", msg.Warning)
	if msg.Worktree != nil {
		if cmd := a.dashboard.SetWorktreeCreating(msg.Worktree, false); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	cmds = append(cmds, a.loadProjects())
	return cmds
}

// handleWorktreeCreated handles the WorktreeCreated message.
func (a *App) handleWorktreeCreated(msg messages.WorktreeCreated) []tea.Cmd {
	var cmds []tea.Cmd
	if msg.Worktree != nil {
		if cmd := a.dashboard.SetWorktreeCreating(msg.Worktree, false); cmd != nil {
			cmds = append(cmds, cmd)
		}
		// Run setup scripts asynchronously
		if msg.Meta != nil {
			cmds = append(cmds, a.runSetupAsync(msg.Worktree, msg.Meta))
		}
	}
	cmds = append(cmds, a.loadProjects())
	return cmds
}

// handleWorktreeSetupComplete handles the WorktreeSetupComplete message.
func (a *App) handleWorktreeSetupComplete(msg messages.WorktreeSetupComplete) tea.Cmd {
	if msg.Err != nil {
		return a.toast.ShowWarning(fmt.Sprintf("Setup failed for %s: %v", msg.Worktree.Name, msg.Err))
	}
	return nil
}

// handleWorktreeCreateFailed handles the WorktreeCreateFailed message.
func (a *App) handleWorktreeCreateFailed(msg messages.WorktreeCreateFailed) tea.Cmd {
	if msg.Worktree != nil {
		if cmd := a.dashboard.SetWorktreeCreating(msg.Worktree, false); cmd != nil {
			return cmd
		}
	}
	a.err = msg.Err
	logging.Error("Error in creating worktree: %v", msg.Err)
	return nil
}

// handleWorktreeDeleted handles the WorktreeDeleted message.
func (a *App) handleWorktreeDeleted(msg messages.WorktreeDeleted) []tea.Cmd {
	var cmds []tea.Cmd
	if msg.Worktree != nil {
		if cmd := a.dashboard.SetWorktreeDeleting(msg.Worktree.Root, false); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if a.statusManager != nil {
			a.statusManager.Invalidate(msg.Worktree.Root)
		}
	}
	cmds = append(cmds, a.loadProjects())
	return cmds
}

// handleWorktreeDeleteFailed handles the WorktreeDeleteFailed message.
func (a *App) handleWorktreeDeleteFailed(msg messages.WorktreeDeleteFailed) tea.Cmd {
	if msg.Worktree != nil {
		if cmd := a.dashboard.SetWorktreeDeleting(msg.Worktree.Root, false); cmd != nil {
			return cmd
		}
	}
	a.err = msg.Err
	logging.Error("Error in removing worktree: %v", msg.Err)
	return nil
}

// handleGitStatusResult handles the GitStatusResult message.
func (a *App) handleGitStatusResult(msg messages.GitStatusResult) tea.Cmd {
	newDashboard, cmd := a.dashboard.Update(msg)
	a.dashboard = newDashboard
	// Update sidebar if this is for the active worktree
	if a.activeWorktree != nil && msg.Root == a.activeWorktree.Root {
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
	// Start reading from the new PTY
	cmd := a.center.StartPTYReaders()
	// NOW switch focus to center - tab is ready
	if a.monitorMode {
		a.focusPane(messages.PaneMonitor)
	} else {
		a.focusPane(messages.PaneCenter)
	}
	return cmd
}

// handlePTYMessages handles PTY-related messages for center pane.
func (a *App) handlePTYMessages(msg tea.Msg) tea.Cmd {
	newCenter, cmd := a.center.Update(msg)
	a.center = newCenter
	return cmd
}

// handleSidebarPTYMessages handles PTY-related messages for sidebar terminal.
func (a *App) handleSidebarPTYMessages(msg tea.Msg) tea.Cmd {
	newSidebarTerminal, cmd := a.sidebarTerminal.Update(msg)
	a.sidebarTerminal = newSidebarTerminal
	return cmd
}

// handleGitStatusTick handles the GitStatusTick message.
func (a *App) handleGitStatusTick() []tea.Cmd {
	var cmds []tea.Cmd
	// Refresh git status for active worktree
	if a.activeWorktree != nil {
		cmds = append(cmds, a.requestGitStatusCached(a.activeWorktree.Root))
	}
	// Continue the ticker
	cmds = append(cmds, a.startGitStatusTicker())
	return cmds
}

// handleFileWatcherEvent handles the FileWatcherEvent message.
func (a *App) handleFileWatcherEvent(msg messages.FileWatcherEvent) []tea.Cmd {
	// File changed, invalidate cache and refresh
	a.statusManager.Invalidate(msg.Root)
	return []tea.Cmd{
		a.requestGitStatus(msg.Root),
		a.startFileWatcher(),
	}
}

// handleDialogResult handles dialog completion
func (a *App) handleDialogResult(result common.DialogResult) tea.Cmd {
	project := a.dialogProject
	worktree := a.dialogWorktree
	a.dialog = nil
	a.dialogProject = nil
	a.dialogWorktree = nil
	logging.Debug("Dialog result: id=%s confirmed=%v value=%s", result.ID, result.Confirmed, result.Value)

	if !result.Confirmed {
		logging.Debug("Dialog cancelled")
		return nil
	}

	switch result.ID {
	case DialogAddProject:
		if result.Value != "" {
			path := validation.SanitizeInput(result.Value)
			logging.Info("Adding project from dialog: %s", path)
			if err := validation.ValidateProjectPath(path); err != nil {
				logging.Warn("Project path validation failed: %v", err)
				return func() tea.Msg {
					return messages.Error{Err: err, Context: "validating project path"}
				}
			}
			return func() tea.Msg {
				return messages.AddProject{Path: path}
			}
		}

	case DialogCreateWorktree:
		if result.Value != "" && project != nil {
			name := validation.SanitizeInput(result.Value)
			if err := validation.ValidateWorktreeName(name); err != nil {
				return func() tea.Msg {
					return messages.Error{Err: err, Context: "validating worktree name"}
				}
			}
			return func() tea.Msg {
				return messages.CreateWorktree{
					Project: project,
					Name:    name,
					Base:    "HEAD",
				}
			}
		}

	case DialogDeleteWorktree:
		if project != nil && worktree != nil {
			wt := worktree
			return func() tea.Msg {
				return messages.DeleteWorktree{
					Project:  project,
					Worktree: wt,
				}
			}
		}

	case DialogRemoveProject:
		if a.dialogProject != nil {
			proj := a.dialogProject
			return func() tea.Msg {
				return messages.RemoveProject{
					Project: proj,
				}
			}
		}

	case DialogSelectAssistant, "agent-picker":
		if a.activeWorktree != nil {
			assistant := result.Value
			if err := validation.ValidateAssistant(assistant); err != nil {
				return func() tea.Msg {
					return messages.Error{Err: err, Context: "validating assistant"}
				}
			}
			wt := a.activeWorktree
			return func() tea.Msg {
				return messages.LaunchAgent{
					Assistant: assistant,
					Worktree:  wt,
				}
			}
		}

	case DialogQuit:
		a.center.Close()
		a.sidebarTerminal.CloseAll()
		a.quitting = true
		return tea.Quit
	}

	return nil
}

func (a *App) showQuitDialog() {
	if a.dialog != nil && a.dialog.Visible() {
		return
	}
	a.dialog = common.NewConfirmDialog(
		DialogQuit,
		"Quit AMUX",
		"Are you sure you want to quit?",
	)
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
}

// handleUpdateCheckComplete handles the UpdateCheckComplete message.
func (a *App) handleUpdateCheckComplete(msg messages.UpdateCheckComplete) tea.Cmd {
	if msg.Err != nil {
		logging.Debug("Update check error: %v", msg.Err)
		return nil
	}
	if !msg.UpdateAvailable {
		logging.Debug("No update available (current=%s, latest=%s)", msg.CurrentVersion, msg.LatestVersion)
		return nil
	}
	// Store update info
	a.updateAvailable = &update.CheckResult{
		CurrentVersion:  msg.CurrentVersion,
		LatestVersion:   msg.LatestVersion,
		UpdateAvailable: msg.UpdateAvailable,
		ReleaseNotes:    msg.ReleaseNotes,
	}
	logging.Info("Update available: %s -> %s", msg.CurrentVersion, msg.LatestVersion)
	// Update settings dialog if visible
	if a.settingsDialog != nil && a.settingsDialog.Visible() {
		a.settingsDialog.SetUpdateInfo(msg.CurrentVersion, msg.LatestVersion, true)
	}
	return nil
}

// handleTriggerUpgrade handles the TriggerUpgrade message.
func (a *App) handleTriggerUpgrade() tea.Cmd {
	if a.updateAvailable == nil || a.upgradeRunning {
		return nil
	}
	a.upgradeRunning = true
	return func() tea.Msg {
		updater := update.NewUpdater(a.version, a.commit, a.buildDate)
		// Get the latest release
		result, err := updater.Check()
		if err != nil {
			return messages.UpgradeComplete{Err: err}
		}
		if result.Release == nil {
			return messages.UpgradeComplete{Err: fmt.Errorf("no release found")}
		}
		// Perform the upgrade
		if err := updater.Upgrade(result.Release); err != nil {
			return messages.UpgradeComplete{Err: err}
		}
		return messages.UpgradeComplete{NewVersion: result.Release.TagName}
	}
}

// handleUpgradeComplete handles the UpgradeComplete message.
func (a *App) handleUpgradeComplete(msg messages.UpgradeComplete) tea.Cmd {
	a.upgradeRunning = false
	if msg.Err != nil {
		logging.Error("Upgrade failed: %v", msg.Err)
		return a.toast.ShowError("Upgrade failed: " + msg.Err.Error())
	}
	a.updateAvailable = nil
	// Update settings dialog if visible
	if a.settingsDialog != nil && a.settingsDialog.Visible() {
		a.settingsDialog.SetUpdateInfo(msg.NewVersion, "", false)
	}
	logging.Info("Upgrade complete: %s", msg.NewVersion)
	return a.toast.ShowSuccess("Upgraded to " + msg.NewVersion + " - restart amux to use new version")
}

// handleOpenFileInEditor handles the OpenFileInEditor message from the project tree.
// This opens the file in vim in the center pane.
func (a *App) handleOpenFileInEditor(msg sidebar.OpenFileInEditor) tea.Cmd {
	if msg.Worktree == nil || msg.Path == "" {
		return nil
	}
	logging.Info("Opening file in editor: %s", msg.Path)
	newCenter, cmd := a.center.Update(messages.OpenFileInVim{
		Path:     msg.Path,
		Worktree: msg.Worktree,
	})
	a.center = newCenter
	return cmd
}
