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
//
// Each presentation is a new dialog instance: the seq stamp is what lets a
// DialogResult emitted through handleDialogInput be bound to the instance
// that produced it (and dropped when that instance has been replaced).
func (a *App) presentDialog(d *common.Dialog) {
	d.SetSize(a.width, a.height)
	d.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialogSeq++
	a.dialogOpenSeq = a.dialogSeq
	d.Show()
}

// presentFilePicker is the *common.FilePicker sibling of presentDialog.
func (a *App) presentFilePicker(fp *common.FilePicker) {
	fp.SetSize(a.width, a.height)
	fp.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	fp.Show()
}

// handleShowAddProjectDialog shows the add project file picker. When the open
// runs synchronously the picker's first directory read is issued right away;
// a deferred open picks the load up through the picker's own Update self-issue.
func (a *App) handleShowAddProjectDialog() tea.Cmd {
	if a.dialogOpen() {
		return nil
	}
	a.requestOverlayOpen(func() {
		a.clearPendingWorkspaceCreate()
		logging.Info("Showing Add Project file picker")
		home, err := os.UserHomeDir()
		if err != nil {
			home = "/"
		}
		a.filePicker = common.NewFilePicker(DialogAddProject, home, true)
		a.filePicker.SetTitle("Add Project")
		a.filePicker.SetPrimaryActionLabel("Add as project")
		a.presentFilePicker(a.filePicker)
	})
	if a.filePicker != nil && a.filePicker.Visible() {
		return a.filePicker.LoadCmd()
	}
	return nil
}

// handleShowCreateWorkspaceDialog shows the create workspace dialog.
func (a *App) handleShowCreateWorkspaceDialog(msg messages.ShowCreateWorkspaceDialog) {
	if a.dialogOpen() {
		return
	}
	a.requestOverlayOpen(func() {
		a.clearPendingWorkspaceCreate()
		a.dlg.project = msg.Project
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
	})
}

// handleShowDeleteWorkspaceDialog shows the delete workspace dialog.
func (a *App) handleShowDeleteWorkspaceDialog(msg messages.ShowDeleteWorkspaceDialog) {
	if a.dialogOpen() {
		return
	}
	a.requestOverlayOpen(func() {
		a.clearPendingWorkspaceCreate()
		a.dlg.project = msg.Project
		a.dlg.workspace = msg.Workspace
		a.dialog = common.NewConfirmDialog(
			DialogDeleteWorkspace,
			"Delete Workspace",
			fmt.Sprintf("Delete workspace '%s' and its branch?", msg.Workspace.Name),
		)
		a.presentDialog(a.dialog)
	})
}

// handleShowShelveWorkspaceDialog shows the shelve confirmation: the copy
// states the kept parts up front because shelve reads like delete to a user
// meeting it for the first time.
func (a *App) handleShowShelveWorkspaceDialog(msg messages.ShowShelveWorkspaceDialog) {
	if msg.Workspace == nil || msg.Project == nil {
		return
	}
	if a.dialogOpen() {
		return
	}
	a.requestOverlayOpen(func() {
		a.clearPendingWorkspaceCreate()
		a.dlg.project = msg.Project
		a.dlg.workspace = msg.Workspace
		a.dialog = common.NewConfirmDialog(
			DialogShelveWorkspace,
			"Shelve Workspace",
			fmt.Sprintf("Shelve workspace '%s'? Its worktree is removed; the branch and settings are kept for restore.", msg.Workspace.Name),
		)
		a.presentDialog(a.dialog)
	})
}

// handleShowRenameWorkspaceDialog shows the rename workspace input dialog,
// prefilled with the workspace's current name for editing.
func (a *App) handleShowRenameWorkspaceDialog(msg messages.ShowRenameWorkspaceDialog) {
	if msg.Workspace == nil {
		return
	}
	if a.dialogOpen() {
		return
	}
	a.requestOverlayOpen(func() {
		a.clearPendingWorkspaceCreate()
		a.dlg.project = msg.Project
		a.dlg.workspace = msg.Workspace
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
	})
}

// handleShowWorkspaceEnvDialog shows the workspace environment-variable
// editor, seeded from a copy of the workspace's current Env with reserved
// keys (process.IsReservedScriptEnvKey -- the AMUX_*/ROOT_* names env.go
// injects) excluded up front, so they can never appear as an editable or
// removable row. Mirrors handleShowRenameWorkspaceDialog's show-time setup.
func (a *App) handleShowWorkspaceEnvDialog(msg messages.ShowWorkspaceEnvDialog) {
	if msg.Workspace == nil {
		return
	}
	a.requestOverlayOpen(func() {
		a.overlays.envWorkspace = msg.Workspace
		a.overlays.env = common.NewEnvDialog(filterReservedEnv(msg.Workspace.Env))
		a.overlays.env.SetKeyValidator(envAddKeyValidator)
		a.overlays.env.SetSize(a.width, a.height)
		a.overlays.env.Show()
	})
}

// handleShowCommitWorkspaceDialog shows the commit-message input dialog for a
// workspace's changes. The message the user types is the confirmation gesture;
// on confirm handleDialogResult stages and commits via git.CommitAll. Esc
// cancels. Live validation mirrors the create-workspace dialog (sanitize, then
// only flag a non-empty value); an empty message is refused by CommitAll.
func (a *App) handleShowCommitWorkspaceDialog(msg messages.ShowCommitWorkspaceDialog) {
	if a.dialogOpen() {
		return
	}
	a.requestOverlayOpen(func() {
		a.clearPendingWorkspaceCreate()
		a.dlg.workspace = msg.Workspace
		a.dialog = common.NewInputDialog(DialogCommitWorkspace, "Commit changes", "Commit message...")
		a.dialog.SetInputValidate(func(s string) string {
			s = validation.SanitizeInput(s)
			if s == "" {
				return "" // Don't show an error for empty input; block on confirm.
			}
			// Defense-in-depth: the message is the argv value of -m so a leading '-'
			// is never parsed as a flag, but keep the value shape consistent with
			// ValidateBaseRef and warn the user before they commit.
			if strings.HasPrefix(s, "-") {
				return "commit message cannot start with '-'"
			}
			return ""
		})
		a.presentDialog(a.dialog)
	})
}

// handleShowTrustScriptsDialog shows the repo script trust confirmation dialog.
func (a *App) handleShowTrustScriptsDialog(msg messages.ShowTrustScriptsDialog) {
	if a.dialogOpen() {
		return
	}
	a.requestOverlayOpen(func() {
		a.clearPendingWorkspaceCreate()
		a.dlg.workspace = msg.Workspace
		a.dlg.trustScriptsHash = msg.ConfigHash
		workspaceName := ""
		repoRoot := ""
		if msg.Workspace != nil {
			workspaceName = msg.Workspace.Name
			repoRoot = msg.Workspace.Repo
		}
		message := fmt.Sprintf("Trust .amux/workspaces.json scripts for '%s' and run setup now?", workspaceName)
		// Show what approval covers: the exact commands being trusted and the repo
		// env key names (values are never rendered — they can carry secrets).
		if manifest := a.trustReviewManifest(repoRoot); manifest != "" {
			message += "\n\n" + manifest
		}
		a.dialog = common.NewConfirmDialog(
			DialogTrustScripts,
			"Trust Project Scripts",
			message,
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
	})
}

// handleShowRemoveProjectDialog shows the remove project dialog.
func (a *App) handleShowRemoveProjectDialog(msg messages.ShowRemoveProjectDialog) {
	if a.dialogOpen() {
		return
	}
	a.requestOverlayOpen(func() {
		a.clearPendingWorkspaceCreate()
		a.dlg.project = msg.Project
		projectName := ""
		if msg.Project != nil {
			projectName = msg.Project.Name
		}
		a.dialog = common.NewConfirmDialog(
			DialogRemoveProject,
			"Remove Project",
			fmt.Sprintf("Remove project '%s' from AMUX? Running agents and project scripts will stop; its repository and worktrees stay on disk.", projectName),
		)
		a.presentDialog(a.dialog)
	})
}

// handleShowSelectAssistantDialog shows the select assistant dialog.
func (a *App) handleShowSelectAssistantDialog() {
	if a.dialogOpen() {
		return
	}
	if a.activeWorkspace == nil && a.pendingWorkspaceCreate.project == nil {
		return
	}
	a.requestOverlayOpen(func() {
		a.dialog = common.NewAgentPicker(a.assistantNames())
		a.presentDialog(a.dialog)
	})
}

// handleShowCleanupTmuxDialog shows the tmux cleanup dialog.
func (a *App) handleShowCleanupTmuxDialog() {
	if a.dialogOpen() {
		return
	}
	a.requestOverlayOpen(func() {
		a.clearPendingWorkspaceCreate()
		a.dialog = common.NewConfirmDialog(
			DialogCleanupTmux,
			"Cleanup tmux sessions",
			fmt.Sprintf("Kill all amux-* tmux sessions on server %q?", a.tmuxOptions.ServerName),
		)
		a.presentDialog(a.dialog)
	})
}

// envAddKeyValidator is the EnvDialog.SetKeyValidator hook shared by the
// workspace and project environment editors: it surfaces the reserved-key
// rule inside the add input, so a reserved name is rejected visibly instead
// of only being dropped by the persist-time filterReservedEnv pass.
func envAddKeyValidator(name string) string {
	if process.IsReservedScriptEnvKey(name) {
		return name + " is reserved (amux injects it)"
	}
	return ""
}

// filterReservedEnv drops any reserved-key (process.IsReservedScriptEnvKey)
// or blank-key entry from env, returning a fresh map. It is used both to seed
// the env dialog (so a reserved key can never appear as an editable row) and,
// defensively, again right before persisting the dialog's read-back map --
// the one place that actually writes ws.Env -- so "reserved keys are never
// written" holds even if a future change to EnvDialog ever let one slip
// through the seed-time filter.
func filterReservedEnv(env map[string]string) map[string]string {
	out := make(map[string]string, len(env))
	for k, v := range env {
		if k == "" || process.IsReservedScriptEnvKey(k) {
			continue
		}
		out[k] = v
	}
	return out
}

// handleEnvDialogResult handles the workspace env dialog's close. On cancel,
// every edit is discarded: no mutation, no persist, matching the settings
// dialog's Esc contract (handleSettingsResult above). On confirm, the edited
// map is filtered (see filterReservedEnv) and persisted via
// WorkspaceStore.SetEnv -- the same load-fresh-then-save shape
// handleRenameWorkspace's store.Rename call uses for its Tier-1 field update,
// so a stale in-memory copy held for the dialog's lifetime cannot clobber a
// field another in-flight operation changed concurrently.
func (a *App) handleEnvDialogResult(res common.EnvDialogResult) tea.Cmd {
	ws := a.overlays.envWorkspace
	a.overlays.envWorkspace = nil
	dialog := a.overlays.env
	a.overlays.env = nil

	if res.Canceled || ws == nil || dialog == nil {
		return nil
	}
	env := filterReservedEnv(dialog.Env())

	if a.workspaceService == nil {
		return nil
	}
	// The service owns the identity rule — it stores under MetadataID(), the
	// persisted record key; ws.ID() drifts across worktree create/remove.
	if err := a.workspaceService.SetWorkspaceEnv(ws, env); err != nil {
		return common.ReportError(errorContext(errorServiceWorkspace, "saving workspace environment"), err, "")
	}
	// Reflect the change immediately on the in-memory active workspace, like
	// handleRenameWorkspace does for Name, so the app's own view of the
	// workspace does not go stale until the next reload.
	if a.activeWorkspace != nil && a.activeWorkspace.Root == ws.Root {
		a.activeWorkspace.Env = env
	}
	return a.toast.ShowSuccess("Updated environment for " + ws.Name)
}
