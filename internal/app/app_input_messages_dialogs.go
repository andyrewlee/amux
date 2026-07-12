package app

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
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

// handleShowRenameWorkspaceDialog shows the rename workspace input dialog,
// prefilled with the workspace's current name for editing.
func (a *App) handleShowRenameWorkspaceDialog(msg messages.ShowRenameWorkspaceDialog) {
	if msg.Workspace == nil {
		return
	}
	a.dialogProject = msg.Project
	a.dialogWorkspace = msg.Workspace
	a.dialog = common.NewInputDialog(DialogRenameWorkspace, "Rename Workspace", "Enter new workspace name...")
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
	// Prefill after presentDialog: Show() resets the input to empty, so the
	// current name must be set afterward to render ready-to-edit.
	a.dialog.SetInputValue(msg.Workspace.Name)
}

// handleShowTrustScriptsDialog shows the repo script trust confirmation dialog.
func (a *App) handleShowTrustScriptsDialog(msg messages.ShowTrustScriptsDialog) {
	a.dialogWorkspace = msg.Workspace
	a.dialogTrustScriptsHash = msg.ConfigHash
	workspaceName := ""
	repoRoot := ""
	if msg.Workspace != nil {
		workspaceName = msg.Workspace.Name
		repoRoot = msg.Workspace.Repo
	}
	a.dialog = common.NewConfirmDialog(
		DialogTrustScripts,
		"Trust Project Scripts",
		fmt.Sprintf("Trust .amux/workspaces.json scripts for '%s' and run setup now?", workspaceName),
	)
	a.dialog.SetDefaultOption(1)
	// Informational only: surface in-repo scripts the approved commands reach
	// into, which the trust gate's manifest hash cannot cover. This changes no
	// gating; an empty warning is NOT a safety guarantee (see
	// scriptIndirectionWarning / process.ReferencesInRepoFiles).
	if warning := scriptIndirectionWarning(a.repoScriptCommandsForTrust(repoRoot), repoRoot); warning != "" {
		a.dialog.SetWarning(warning)
	}
	a.presentDialog(a.dialog)
}

// repoScriptCommandsForTrust returns the repo-supplied commands from repo's
// .amux/workspaces.json (setup-workspace/run/archive — the same commands the
// trust gate hashes), best-effort and read-only, for the trust dialog's
// indirection warning. It never gates: a nil service, empty repo, or load error
// simply yields no commands (and therefore no warning). It does not hash and so
// cannot disagree with what the gate hashed.
func (a *App) repoScriptCommandsForTrust(repo string) []string {
	if a.workspaceService == nil || a.workspaceService.scripts == nil || repo == "" {
		return nil
	}
	config, err := a.workspaceService.scripts.LoadConfig(repo)
	if err != nil || config == nil {
		return nil
	}
	commands := append([]string(nil), config.SetupWorkspace...)
	if config.RunScript != "" {
		commands = append(commands, config.RunScript)
	}
	if config.ArchiveScript != "" {
		commands = append(commands, config.ArchiveScript)
	}
	return commands
}

// scriptIndirectionWarning builds the trust dialog's advisory text about
// commands that reach into in-repo files the manifest hash cannot pin. It runs
// the shipped, already-tested detector (process.ReferencesInRepoFiles /
// CommandIsUnresolvable) over the repo-supplied commands and reports what it
// found. It returns "" only when the detector found neither a referenced file
// nor an unresolvable construct — which is explicitly NOT a guarantee the
// commands run no repo code (the detector's contract), so the empty case renders
// nothing rather than any reassurance. It never authorizes or blocks anything.
func scriptIndirectionWarning(commands []string, repoRoot string) string {
	var refs []string
	seen := make(map[string]struct{})
	unresolvable := false
	for _, cmd := range commands {
		if process.CommandIsUnresolvable(cmd) {
			unresolvable = true
		}
		for _, ref := range process.ReferencesInRepoFiles(cmd, repoRoot) {
			if _, dup := seen[ref]; dup {
				continue
			}
			seen[ref] = struct{}{}
			refs = append(refs, ref)
		}
	}

	var lines []string
	if len(refs) > 0 {
		lines = append(lines, "Runs in-repo scripts amux can't re-verify after approval: "+strings.Join(refs, ", "))
	}
	if unresolvable {
		lines = append(lines, "One or more commands use variables/globs — amux can't list every file they run.")
	}
	return strings.Join(lines, "\n")
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
		a.config.UI.TmuxServer,
		a.config.UI.TmuxConfigPath,
		a.config.UI.TmuxSyncInterval,
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
		return common.ReportError("saving theme setting", err, "Failed to save theme setting")
	}
	a.settingsThemePersistedTheme = common.ThemeID(a.config.UI.Theme)
	a.settingsThemeDirty = false
	return nil
}

// applySettingsTmux copies the dialog's (possibly edited) tmux values into the
// in-memory config and reports whether any changed. The values are read as
// AMUX_TMUX_* env vars at launch, so persisting them here takes effect on the
// next start (the dialog surfaces a "restart to apply" hint).
func (a *App) applySettingsTmux(d *common.SettingsDialog) bool {
	changed := false
	if v := d.TmuxServer(); v != a.config.UI.TmuxServer {
		a.config.UI.TmuxServer = v
		changed = true
	}
	if v := d.TmuxConfigPath(); v != a.config.UI.TmuxConfigPath {
		a.config.UI.TmuxConfigPath = v
		changed = true
	}
	if v := d.TmuxSyncInterval(); v != a.config.UI.TmuxSyncInterval {
		a.config.UI.TmuxSyncInterval = v
		changed = true
	}
	return changed
}

// handleSettingsResult handles settings dialog close.
func (a *App) handleSettingsResult(res common.SettingsResult) tea.Cmd {
	if res.Canceled {
		// Esc cancels: revert any live theme preview to what was active when the
		// dialog opened and do not persist. Tmux edits are dropped with it.
		a.applyTheme(a.settingsThemeOriginal)
		a.settingsThemeDirty = false
		a.settingsDialog = nil
		a.settingsDialogSession++
		return nil
	}
	tmuxChanged := false
	if a.settingsDialog != nil {
		a.applyTheme(a.settingsDialog.SelectedTheme())
		tmuxChanged = a.applySettingsTmux(a.settingsDialog)
	}
	a.settingsDialog = nil
	a.settingsDialogSession++
	// A dirty theme save already persists the whole UI struct (tmux fields
	// included, since applySettingsTmux wrote them). Only persist separately
	// when tmux changed but the theme did not.
	if a.settingsThemeDirty {
		return a.persistSettingsThemeIfDirty()
	}
	if tmuxChanged {
		if err := a.config.SaveUISettings(); err != nil {
			return common.ReportError("saving tmux settings", err, "Failed to save tmux settings")
		}
	}
	return nil
}
