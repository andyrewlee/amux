package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/update"
	"github.com/andyrewlee/amux/internal/validation"
)

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

// handleOverlayInput updates a modal overlay and reports whether the message was consumed.
// When consumePaste is true, tea.PasteMsg is treated as consumed input.
func handleOverlayInput[T interface {
	Visible() bool
	Update(tea.Msg) (T, tea.Cmd)
}](overlay T, msg tea.Msg, cmds *[]tea.Cmd, consumePaste bool) (T, bool) {
	if isNilOverlay(overlay) || !overlay.Visible() {
		return overlay, false
	}
	updated, cmd := overlay.Update(msg)
	if cmd != nil {
		*cmds = append(*cmds, cmd)
	}
	switch msg.(type) {
	case tea.KeyPressMsg, tea.MouseClickMsg, tea.MouseWheelMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg:
		return updated, true
	case tea.PasteMsg:
		return updated, consumePaste
	}
	return updated, false
}

func isNilOverlay[T any](overlay T) bool {
	v := reflect.ValueOf(overlay)
	if !v.IsValid() {
		return true
	}
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func (a *App) handleDialogInput(msg tea.Msg, cmds *[]tea.Cmd) bool {
	// Collect the cmds the open dialog produces this round separately so each
	// can be bound to the producing instance: the cmd executes asynchronously,
	// and by the time its DialogResult lands a newer dialog may have replaced
	// the producer (async Show* messages).
	var dialogCmds []tea.Cmd
	var consumed bool
	a.dialog, consumed = handleOverlayInput(a.dialog, msg, &dialogCmds, true)
	seq, dlg := a.dialogOpenSeq, a.dlg
	for _, cmd := range dialogCmds {
		*cmds = append(*cmds, bindDialogResultCmd(cmd, seq, dlg))
	}
	return consumed
}

func (a *App) handleFilePickerInput(msg tea.Msg, cmds *[]tea.Cmd) bool {
	var consumed bool
	a.filePicker, consumed = handleOverlayInput(a.filePicker, msg, cmds, true)
	return consumed
}

// handleDialogResult handles dialog completion. dlg is the context bound to
// the producing dialog instance at emit time (boundDialogResultMsg) — never
// re-read from a.dlg here, since a.dlg may belong to a newer dialog by the
// time the result lands. Confirmed results dispatch through appDialogHandlers
// (the single registration table in app_dialogs_registry.go) — adding a
// dialog never edits this function.
func (a *App) handleDialogResult(result common.DialogResult, dlg dialogContext) tea.Cmd {
	logging.Debug("Dialog result: id=%s confirmed=%v value_len=%d", result.ID, result.Confirmed, len(result.Value))

	// Defensive: handleDialogResult only knows how to act on IDs in the shared
	// registry. Routing already gates on isAppDialogID, so an unknown ID here
	// signals the handler table and the routing allow-list have drifted.
	if !isAppDialogID(result.ID) {
		logging.Warn("handleDialogResult called with non-App dialog ID: %s", result.ID)
		return nil
	}

	if !result.Confirmed {
		if result.ID == common.AgentPickerDialogID {
			a.pendingWorkspaceCreate.project = nil
			a.pendingWorkspaceCreate.name = ""
			a.pendingWorkspaceCreate.base = ""
		}
		logging.Debug("Dialog canceled")
		return nil
	}

	handler, ok := appDialogHandlers[result.ID]
	if !ok {
		logging.Warn("no handler registered for App dialog ID: %s", result.ID)
		return nil
	}
	return handler(a, result, dlg)
}

func dialogResultAddProject(a *App, result common.DialogResult, _ dialogContext) tea.Cmd {
	if result.Value == "" {
		// Confirming an empty path must not silently close with no feedback.
		return a.toast.ShowWarning("Project path is required")
	}
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

func dialogResultCreateWorkspace(a *App, result common.DialogResult, dlg dialogContext) tea.Cmd {
	if result.Value == "" || dlg.project == nil {
		return nil
	}
	name := validation.SanitizeInput(result.Value)
	if err := validation.ValidateWorkspaceName(name); err != nil {
		return func() tea.Msg {
			return messages.Error{Err: err, Context: errorContext(errorServiceDialog, "validating workspace name")}
		}
	}
	a.pendingWorkspaceCreate.project = dlg.project
	a.pendingWorkspaceCreate.name = name
	a.pendingWorkspaceCreate.base = ""
	return func() tea.Msg {
		return messages.ShowSelectAssistantDialog{}
	}
}

func dialogResultDeleteWorkspace(_ *App, _ common.DialogResult, dlg dialogContext) tea.Cmd {
	if dlg.project == nil || dlg.workspace == nil {
		return nil
	}
	ws := dlg.workspace
	return func() tea.Msg {
		return messages.DeleteWorkspace{
			Project:   dlg.project,
			Workspace: ws,
		}
	}
}

func dialogResultShelveWorkspace(_ *App, _ common.DialogResult, dlg dialogContext) tea.Cmd {
	if dlg.project == nil || dlg.workspace == nil {
		return nil
	}
	ws := dlg.workspace
	return func() tea.Msg {
		return messages.ShelveWorkspace{
			Project:   dlg.project,
			Workspace: ws,
		}
	}
}

func dialogResultRenameWorkspace(_ *App, result common.DialogResult, dlg dialogContext) tea.Cmd {
	if dlg.workspace == nil || result.Value == "" {
		return nil
	}
	name := validation.SanitizeInput(result.Value)
	if err := validation.ValidateWorkspaceName(name); err != nil {
		return func() tea.Msg {
			return messages.Error{Err: err, Context: errorContext(errorServiceDialog, "validating workspace name")}
		}
	}
	ws := dlg.workspace
	proj := dlg.project
	return func() tea.Msg {
		return messages.RenameWorkspace{
			Project:   proj,
			Workspace: ws,
			NewName:   name,
		}
	}
}

func dialogResultCommitWorkspace(a *App, result common.DialogResult, dlg dialogContext) tea.Cmd {
	if dlg.workspace == nil {
		return nil
	}
	// Message is the argv value of -m; sanitize control chars but never
	// shell-interpolate. CommitAll rejects an empty message.
	return a.commitWorkspaceAsync(dlg.workspace, validation.SanitizeInput(result.Value))
}

func dialogResultMergeWorkspace(a *App, _ common.DialogResult, dlg dialogContext) tea.Cmd {
	if dlg.workspace == nil {
		return nil
	}
	// mergeBase was resolved and verified against the primary checkout's
	// HEAD before the dialog was shown; reuse it rather than re-deriving,
	// so the merge reports the branch the user actually agreed to.
	return a.mergeWorkspaceAsync(dlg.workspace, dlg.mergeBase)
}

func dialogResultMergeConflict(a *App, _ common.DialogResult, dlg dialogContext) tea.Cmd {
	// Confirmed means "Yes, abort"; declining leaves the merge in progress
	// for the user to resolve themselves, which is handled by the early
	// !result.Confirmed return above.
	if dlg.workspace == nil {
		return nil
	}
	return a.abortWorkspaceMergeAsync(dlg.workspace)
}

func dialogResultTrustScripts(a *App, _ common.DialogResult, dlg dialogContext) tea.Cmd {
	if dlg.workspace == nil {
		return nil
	}
	return a.trustRepoScriptsAndRunSetupAsync(dlg.workspace, dlg.trustScriptsHash)
}

// dialogResultSaveTranscript writes the transcript captured at dialog-open
// (dlg.transcript) to the user-chosen path. No truncation — a file sink holds
// the full scrollback the 1 MiB clipboard cap drops.
func dialogResultSaveTranscript(a *App, result common.DialogResult, dlg dialogContext) tea.Cmd {
	if dlg.transcript == "" {
		return nil
	}
	raw := strings.TrimSpace(result.Value)
	if raw == "" {
		return a.toast.ShowError("Save transcript: path cannot be empty")
	}
	path, err := config.ExpandHomePath(raw)
	if err != nil {
		return a.toast.ShowError(fmt.Sprintf("Save transcript: %v", err))
	}
	text := dlg.transcript
	return func() tea.Msg {
		if dir := filepath.Dir(path); dir != "" {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return messages.Toast{Level: messages.ToastError, Message: fmt.Sprintf("Save transcript: %v", err)}
			}
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			return messages.Toast{Level: messages.ToastError, Message: fmt.Sprintf("Save transcript: %v", err)}
		}
		return messages.Toast{Level: messages.ToastSuccess, Message: fmt.Sprintf("Transcript saved to %s (%d bytes)", path, len(text))}
	}
}

// dialogResultBrowseTranscripts opens the picked transcript in the
// configured file-viewer tab, hosted on the active workspace (the gate in
// browseTranscriptsCommand guarantees one was selected at open time; the
// nil-check covers the workspace being deleted while the picker was up).
func dialogResultBrowseTranscripts(a *App, result common.DialogResult, _ dialogContext) tea.Cmd {
	path := strings.TrimSpace(result.Value)
	if path == "" {
		return nil
	}
	ws := a.activeWorkspace
	if ws == nil {
		return a.toast.ShowWarning("Select a workspace to host the transcript viewer")
	}
	return func() tea.Msg {
		return messages.OpenFileInVim{Path: path, Workspace: ws}
	}
}

func dialogResultRemoveProject(_ *App, _ common.DialogResult, dlg dialogContext) tea.Cmd {
	if dlg.project == nil {
		return nil
	}
	proj := dlg.project
	return func() tea.Msg {
		return messages.RemoveProject{
			Project: proj,
		}
	}
}

func dialogResultAgentPicker(a *App, result common.DialogResult, _ dialogContext) tea.Cmd {
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
	if a.pendingWorkspaceCreate.project != nil && a.pendingWorkspaceCreate.name != "" {
		pendingProject := a.pendingWorkspaceCreate.project
		pendingName := a.pendingWorkspaceCreate.name
		pendingBase := a.pendingWorkspaceCreate.base
		a.pendingWorkspaceCreate.project = nil
		a.pendingWorkspaceCreate.name = ""
		a.pendingWorkspaceCreate.base = ""
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
	return nil
}

func dialogResultQuit(a *App, _ common.DialogResult, _ dialogContext) tea.Cmd {
	// Persist workspace tabs synchronously before shutdown.
	// Shutdown() closes tabs (sets Running=false), so we must
	// capture current state first to avoid saving "stopped" status.
	a.persistAllWorkspacesNow()
	a.Shutdown()
	a.quitting = true
	return tea.Quit
}

func dialogResultCleanupTmux(_ *App, _ common.DialogResult, _ dialogContext) tea.Cmd {
	return func() tea.Msg { return messages.CleanupTmuxSessions{} }
}

func (a *App) showQuitDialog() {
	if a.dialogOpen() {
		return
	}
	a.requestOverlayOpen(func() {
		a.clearPendingWorkspaceCreate()
		a.dialog = common.NewConfirmDialog(
			DialogQuit,
			"Quit AMUX",
			"Are you sure you want to quit?",
		)
		a.presentDialog(a.dialog)
	})
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
	if a.overlays.settings != nil && a.overlays.settings.Visible() {
		a.overlays.settings.SetUpdateInfo(msg.CurrentVersion, msg.LatestVersion, true)
	}
	return nil
}

// handleTriggerUpgrade handles the TriggerUpgrade message.
func (a *App) handleTriggerUpgrade() tea.Cmd {
	if a.overlays.settings != nil {
		a.applyTheme(a.overlays.settings.SelectedTheme())
		a.overlays.settings = nil
		a.overlays.settingsSession++
	}
	persistCmd := a.persistSettingsThemeIfDirty()
	if a.updateAvailable == nil || a.upgradeRunning {
		return persistCmd
	}
	a.upgradeRunning = true
	svc := a.updateService
	upgradeCmd := func() tea.Msg {
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
	return common.SafeBatch(persistCmd, upgradeCmd)
}

// handleUpgradeComplete handles the UpgradeComplete message.
func (a *App) handleUpgradeComplete(msg messages.UpgradeComplete) tea.Cmd {
	a.upgradeRunning = false
	if msg.Err != nil {
		return common.ReportError("upgrading amux", msg.Err, "Upgrade failed: "+msg.Err.Error())
	}
	a.updateAvailable = nil
	// Update settings dialog if visible
	if a.overlays.settings != nil && a.overlays.settings.Visible() {
		a.overlays.settings.SetUpdateInfo(msg.NewVersion, "", false)
	}
	logging.Info("Upgrade complete: %s", msg.NewVersion)
	return a.toast.ShowSuccess("Upgraded to " + msg.NewVersion + " - restart amux to use new version")
}

// handleOpenFileInVim handles the OpenFileInVim message from the project tree.
// This opens the file in vim in the center pane.
func (a *App) handleOpenFileInVim(msg messages.OpenFileInVim) tea.Cmd {
	if msg.Workspace == nil || msg.Path == "" {
		return nil
	}
	logging.Info("Opening file in editor: %s", msg.Path)
	newCenter, cmd := a.center.Update(msg)
	a.center = newCenter
	return cmd
}

// dialogResultRunSessionPicker opens the run-output viewer pinned to the
// picked session. dlg.runSessions is the enumeration the picker's Index
// addresses; a vanished or never-recorded selection reports rather than
// panicking.
func dialogResultRunSessionPicker(a *App, result common.DialogResult, dlg dialogContext) tea.Cmd {
	if !result.Confirmed {
		return nil
	}
	if result.Index < 0 || result.Index >= len(dlg.runSessions) || dlg.workspace == nil {
		return func() tea.Msg {
			return messages.Error{Err: errors.New("run session selection out of range"), Context: errorContext(errorServiceDialog, "selecting run session")}
		}
	}
	return a.fetchRunOutputCmd(a.overlays.runOutputToken, dlg.workspace, dlg.runSessions[result.Index])
}
