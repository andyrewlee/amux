package app

import (
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
	"github.com/andyrewlee/amux/internal/update"
	"github.com/andyrewlee/amux/internal/validation"
)

func (a *App) handleDialogResultMsg(msg tea.Msg) (bool, tea.Cmd) {
	result, ok := msg.(common.DialogResult)
	if !ok {
		return false, nil
	}
	logging.Info("Received DialogResult: id=%s confirmed=%v", result.ID, result.Confirmed)
	switch result.ID {
	case DialogAddProject, DialogCreateWorkspace, DialogDeleteWorkspace, DialogRemoveProject, DialogSelectAssistant, "agent-picker", DialogQuit, DialogCleanupTmux:
		return true, common.SafeCmd(a.handleDialogResult(result))
	}
	// If not an App-level dialog, let it fall through to components.
	newCenter, cmd := a.center.Update(msg)
	a.center = newCenter
	return true, common.SafeCmd(cmd)
}

func (a *App) handleHelpOverlayInput(msg tea.Msg) (bool, tea.Cmd) {
	if !a.helpOverlay.Visible() {
		return false, nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		var cmd tea.Cmd
		a.helpOverlay, _, cmd = a.helpOverlay.Update(msg)
		return true, common.SafeCmd(cmd)
	case tea.MouseWheelMsg:
		a.helpOverlay, _, _ = a.helpOverlay.Update(msg)
		return true, nil
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			// First check if clicking on a link inside the dialog
			var cmd tea.Cmd
			a.helpOverlay, _, cmd = a.helpOverlay.Update(msg)
			if cmd != nil {
				return true, common.SafeCmd(cmd)
			}
			// Close if clicking outside the dialog
			if !a.helpOverlay.ContainsClick(msg.X, msg.Y) {
				a.helpOverlay.Hide()
			}
			return true, nil
		}
	}
	return false, nil
}

func (a *App) handleErrorOverlayDismiss(msg tea.Msg) bool {
	mouseMsg, ok := msg.(tea.MouseClickMsg)
	if !ok || mouseMsg.Button != tea.MouseLeft {
		return false
	}
	if a.err == nil {
		return false
	}
	a.err = nil
	return true
}

func (a *App) handleDialogInput(msg tea.Msg, cmds *[]tea.Cmd) bool {
	if a.dialog == nil || !a.dialog.Visible() {
		return false
	}
	newDialog, cmd := a.dialog.Update(msg)
	a.dialog = newDialog
	if cmd != nil {
		*cmds = append(*cmds, cmd)
	}
	// Don't process other input while dialog is open.
	switch msg.(type) {
	case tea.KeyPressMsg, tea.PasteMsg, tea.MouseClickMsg:
		return true
	}
	return false
}

func (a *App) handleFilePickerInput(msg tea.Msg, cmds *[]tea.Cmd) bool {
	if a.filePicker == nil || !a.filePicker.Visible() {
		return false
	}
	newPicker, cmd := a.filePicker.Update(msg)
	a.filePicker = newPicker
	if cmd != nil {
		*cmds = append(*cmds, cmd)
	}
	// Don't process other input while file picker is open.
	switch msg.(type) {
	case tea.KeyPressMsg, tea.PasteMsg, tea.MouseClickMsg:
		return true
	}
	return false
}

func (a *App) handleSettingsDialogInput(msg tea.Msg, cmds *[]tea.Cmd) bool {
	if a.settingsDialog == nil || !a.settingsDialog.Visible() {
		return false
	}
	newSettings, cmd := a.settingsDialog.Update(msg)
	a.settingsDialog = newSettings
	if cmd != nil {
		*cmds = append(*cmds, cmd)
	}
	// Don't process other input while settings dialog is open.
	switch msg.(type) {
	case tea.KeyPressMsg, tea.MouseClickMsg:
		return true
	}
	return false
}

// handleDialogResult handles dialog completion
func (a *App) handleDialogResult(result common.DialogResult) tea.Cmd {
	project := a.dialogProject
	workspace := a.dialogWorkspace
	a.dialog = nil
	a.dialogProject = nil
	a.dialogWorkspace = nil
	logging.Debug("Dialog result: id=%s confirmed=%v value=%s", result.ID, result.Confirmed, result.Value)

	if !result.Confirmed {
		if result.ID == DialogSelectAssistant || result.ID == "agent-picker" {
			a.pendingWorkspaceProject = nil
			a.pendingWorkspaceName = ""
			a.pendingWorkspaceBase = ""
		}
		logging.Debug("Dialog canceled")
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
					return messages.Error{Err: err, Context: errorContext(errorServiceDialog, "validating project path")}
				}
			}
			return func() tea.Msg {
				return messages.AddProject{Path: path}
			}
		}

	case DialogCreateWorkspace:
		if result.Value != "" && project != nil {
			name := validation.SanitizeInput(result.Value)
			if err := validation.ValidateWorkspaceName(name); err != nil {
				return func() tea.Msg {
					return messages.Error{Err: err, Context: errorContext(errorServiceDialog, "validating workspace name")}
				}
			}
			a.pendingWorkspaceProject = project
			a.pendingWorkspaceName = name
			a.pendingWorkspaceBase = "HEAD"
			return func() tea.Msg {
				return messages.ShowSelectAssistantDialog{}
			}
		}

	case DialogDeleteWorkspace:
		if project != nil && workspace != nil {
			ws := workspace
			return func() tea.Msg {
				return messages.DeleteWorkspace{
					Project:   project,
					Workspace: ws,
				}
			}
		}

	case DialogRemoveProject:
		if project != nil {
			proj := project
			return func() tea.Msg {
				return messages.RemoveProject{
					Project: proj,
				}
			}
		}

	case DialogSelectAssistant, "agent-picker":
		assistant := result.Value
		if err := validation.ValidateAssistant(assistant); err != nil {
			return func() tea.Msg {
				return messages.Error{Err: err, Context: errorContext(errorServiceDialog, "validating assistant")}
			}
		}
		if !a.isKnownAssistant(assistant) {
			return func() tea.Msg {
				return messages.Error{Err: errors.New("unknown assistant: " + assistant), Context: errorContext(errorServiceDialog, "validating assistant")}
			}
		}
		if a.pendingWorkspaceProject != nil && a.pendingWorkspaceName != "" {
			pendingProject := a.pendingWorkspaceProject
			pendingName := a.pendingWorkspaceName
			pendingBase := a.pendingWorkspaceBase
			a.pendingWorkspaceProject = nil
			a.pendingWorkspaceName = ""
			a.pendingWorkspaceBase = ""
			return func() tea.Msg {
				return messages.CreateWorkspace{
					Project:   pendingProject,
					Name:      pendingName,
					Base:      pendingBase,
					Assistant: assistant,
				}
			}
		}
		if a.activeWorkspace != nil {
			ws := a.activeWorkspace
			return func() tea.Msg {
				return messages.LaunchAgent{
					Assistant: assistant,
					Workspace: ws,
				}
			}
		}

	case DialogQuit:
		// Persist workspace tabs synchronously before shutdown.
		// Shutdown() closes tabs (sets Running=false), so we must
		// capture current state first to avoid saving "stopped" status.
		a.persistAllWorkspacesNow()
		a.Shutdown()
		a.quitting = true
		return tea.Quit

	case DialogCleanupTmux:
		return func() tea.Msg { return messages.CleanupTmuxSessions{} }
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
	svc := a.updateService
	return func() tea.Msg {
		if svc == nil {
			return messages.UpgradeComplete{Err: errors.New("update service unavailable")}
		}
		// Get the latest release
		result, err := svc.Check()
		if err != nil {
			return messages.UpgradeComplete{Err: err}
		}
		if result.Release == nil {
			return messages.UpgradeComplete{Err: errors.New("no release found")}
		}
		// Perform the upgrade
		if err := svc.Upgrade(result.Release); err != nil {
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
	if msg.Workspace == nil || msg.Path == "" {
		return nil
	}
	logging.Info("Opening file in editor: %s", msg.Path)
	newCenter, cmd := a.center.Update(messages.OpenFileInVim{
		Path:      msg.Path,
		Workspace: msg.Workspace,
	})
	a.center = newCenter
	return cmd
}
