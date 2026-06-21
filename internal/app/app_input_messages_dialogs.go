package app

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/validation"
)

// presentDialog applies the common show-time setup (size + keymap hints) and
// makes the dialog visible. Centralizing this keeps every Show*Dialog handler
// from repeating the SetSize/SetShowKeymapHints/Show trailer.
func (a *App) presentDialog(d *common.Dialog) {
	d.SetSize(a.width, a.height)
	d.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	d.Show()
}

// presentFilePicker is the *common.FilePicker sibling of presentDialog.
func (a *App) presentFilePicker(fp *common.FilePicker) {
	fp.SetSize(a.width, a.height)
	fp.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	fp.Show()
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
	a.presentFilePicker(a.filePicker)
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
	a.presentDialog(a.dialog)
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
	a.presentDialog(a.dialog)
}

// handleShowTrustScriptsDialog shows the repo script trust confirmation dialog.
func (a *App) handleShowTrustScriptsDialog(msg messages.ShowTrustScriptsDialog) {
	a.dialogWorkspace = msg.Workspace
	a.dialogTrustScriptsHash = msg.ConfigHash
	workspaceName := ""
	if msg.Workspace != nil {
		workspaceName = msg.Workspace.Name
	}
	a.dialog = common.NewConfirmDialog(
		DialogTrustScripts,
		"Trust Project Scripts",
		fmt.Sprintf("Trust .amux/workspaces.json scripts for '%s' and run setup now?", workspaceName),
	)
	a.dialog.SetDefaultOption(1)
	a.presentDialog(a.dialog)
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
	a.presentDialog(a.dialog)
}

// handleShowSelectAssistantDialog shows the select assistant dialog.
func (a *App) handleShowSelectAssistantDialog() {
	if a.activeWorkspace == nil && a.pendingWorkspaceProject == nil {
		return
	}
	a.dialog = common.NewAgentPicker(a.assistantNames())
	a.presentDialog(a.dialog)
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
	a.presentDialog(a.dialog)
}

// handleShowSettingsDialog shows the settings dialog.
func (a *App) handleShowSettingsDialog() {
	persistedUI := a.config.PersistedUISettings()
	a.settingsThemePersistedTheme = common.ThemeID(persistedUI.Theme)
	a.settingsThemeOriginal = common.ThemeID(a.config.UI.Theme)
	a.settingsThemeDirty = common.ThemeID(a.config.UI.Theme) != a.settingsThemePersistedTheme
	a.settingsDialogSession++
	a.settingsDialog = common.NewSettingsDialog(
		common.ThemeID(a.config.UI.Theme),
	)
	a.settingsDialog.SetSession(a.settingsDialogSession)
	a.settingsDialog.SetSize(a.width, a.height)

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

func (a *App) applyTheme(theme common.ThemeID) {
	common.SetCurrentTheme(theme)
	a.config.UI.Theme = string(theme)
	a.settingsThemeDirty = theme != a.settingsThemePersistedTheme
	a.styles = common.DefaultStyles()
	// Propagate styles to all components.
	a.propagateStyles()
}

// handleThemePreview handles live theme preview.
func (a *App) handleThemePreview(msg common.ThemePreview) tea.Cmd {
	if msg.Session != a.settingsDialogSession {
		return nil
	}
	if a.settingsDialog != nil {
		a.settingsDialog.SetSelectedTheme(msg.Theme)
	}
	a.applyTheme(msg.Theme)
	return nil
}

func (a *App) persistSettingsThemeIfDirty() tea.Cmd {
	if !a.settingsThemeDirty {
		return nil
	}
	if err := a.config.SaveUISettings(); err != nil {
		logging.Warn("Failed to save theme setting: %v", err)
		return a.toast.ShowWarning("Failed to save theme setting")
	}
	a.settingsThemePersistedTheme = common.ThemeID(a.config.UI.Theme)
	a.settingsThemeDirty = false
	return nil
}

// handleSettingsResult handles settings dialog close.
func (a *App) handleSettingsResult(res common.SettingsResult) tea.Cmd {
	if res.Canceled {
		// Esc cancels: revert any live theme preview to what was active when the
		// dialog opened and do not persist.
		a.applyTheme(a.settingsThemeOriginal)
		a.settingsThemeDirty = false
		a.settingsDialog = nil
		a.settingsDialogSession++
		return nil
	}
	if a.settingsDialog != nil {
		a.applyTheme(a.settingsDialog.SelectedTheme())
	}
	a.settingsDialog = nil
	a.settingsDialogSession++
	return a.persistSettingsThemeIfDirty()
}
