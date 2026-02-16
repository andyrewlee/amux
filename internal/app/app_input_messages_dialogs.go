package app

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/validation"
)

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
	if a.activeWorkspace == nil && a.pendingWorkspaceProject == nil {
		return
	}
	a.dialog = common.NewAgentPicker(a.assistantNames())
	a.dialog.SetSize(a.width, a.height)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
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
		return common.SafeBatch(cmds...)
	}
	return nil
}
