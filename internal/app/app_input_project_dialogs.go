package app

// Project-scoped dialog handlers live here — split from
// app_input_messages_dialogs.go under the 500-line file cap.

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/validation"
)

// handleShowCreateWorkspaceDialog shows the create workspace dialog.
func (a *App) handleShowCreateWorkspaceDialog(msg messages.ShowCreateWorkspaceDialog) {
	if a.dialogOpen() {
		return
	}
	// The project is the creation target: one that was loaded at request
	// time but is gone at drain must not host a workspace create.
	required := msg.Project != nil && a.findProjectByPath(msg.Project.Path) != nil
	a.requestOverlayOpen(func() {
		if required && a.findProjectByPath(msg.Project.Path) == nil {
			return
		}
		a.clearPendingWorkspaceCreate()
		a.dlg.project = msg.Project
		a.dialog = common.NewInputDialog(DialogCreateWorkspace, "Create Workspace", "Enter workspace name...")
		a.dialog.SetInputLabel("Workspace name")
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
		// Second field is the base ref the new worktree's branch is created
		// from — empty means "detect the project's default branch"
		// (workspacesvc.ResolveBase). Distinct from the workspace's own
		// branch name, which still derives from the workspace name.
		a.dialog.SetSecondInput("Base (optional)", "default branch", func(s string) string {
			s = validation.SanitizeInput(s)
			if s == "" {
				return ""
			}
			if err := validation.ValidateBaseRef(s); err != nil {
				return err.Error()
			}
			return ""
		})
		a.presentDialog(a.dialog)
	})
}

// handleShowRemoveProjectDialog shows the remove project dialog.
func (a *App) handleShowRemoveProjectDialog(msg messages.ShowRemoveProjectDialog) {
	if a.dialogOpen() {
		return
	}
	required := msg.Project != nil && a.findProjectByPath(msg.Project.Path) != nil
	a.requestOverlayOpen(func() {
		a.clearPendingWorkspaceCreate()
		proj := msg.Project
		if required {
			proj = a.findProjectByPath(msg.Project.Path)
			if proj == nil {
				return // the removal subject no longer exists
			}
		}
		a.dlg.project = proj
		projectName := ""
		if proj != nil {
			projectName = proj.Name
		}
		a.dialog = common.NewConfirmDialog(
			DialogRemoveProject,
			"Remove Project",
			fmt.Sprintf("Remove project '%s' from AMUX? Running agents and project scripts will stop; its repository and worktrees stay on disk.", projectName),
		)
		a.presentDialog(a.dialog)
	})
}

// handleShowRenameProjectDialog shows the rename project dialog, prefilled
// with the project's current name. The re-resolved project is authoritative
// (mirroring the workspace rename dialog) so a stale row cannot rename a
// project that was removed since the dialog was requested.
func (a *App) handleShowRenameProjectDialog(msg messages.ShowRenameProjectDialog) {
	if msg.Project == nil {
		return
	}
	if a.dialogOpen() {
		return
	}
	a.requestOverlayOpen(func() {
		proj := a.findProjectByPath(msg.Project.Path)
		if proj == nil {
			return // the rename subject no longer exists
		}
		// Present-only, matching the create-workspace sibling and the
		// arbiter's contract (app_overlay_arbiter.go): displacing a staged
		// create→agent-picker handoff clears it when this dialog actually
		// presents — a dropped stale open must never tear down an
		// unrelated in-flight create handoff.
		a.clearPendingWorkspaceCreate()
		a.dlg.project = proj
		a.dialog = common.NewInputDialog(DialogRenameProject, "Rename Project", "Enter new project name...")
		a.dialog.SetInputLabel("Project name")
		a.dialog.SetInputValidate(func(s string) string {
			s = validation.SanitizeInput(s)
			if s == "" {
				return "" // Don't show error for empty input
			}
			if err := validation.ValidateProjectName(s); err != nil {
				return err.Error()
			}
			if s == proj.Name {
				return "" // Renaming to the current name is a no-op, not an error
			}
			if strings.EqualFold(s, proj.Name) && a.projectContainerCaseCollides(proj.Name, s) {
				return "only letter case changes and both spellings are one directory; choose a different name"
			}
			for _, other := range a.projects {
				if other.Path != proj.Path && strings.EqualFold(other.Name, s) {
					return "name already used by " + other.Path
				}
			}
			return ""
		})
		a.presentDialog(a.dialog)
		// Prefill after presentDialog: Show() resets the input to empty, so
		// the current name must be set afterward to render ready-to-edit.
		a.dialog.SetInputValue(proj.Name)
	})
}

// projectContainerCaseCollides reports whether a case-only rename of
// projName to newName would keep both spellings resolving to one physical
// container directory — the case-insensitive-filesystem shape
// Service.RenameProject refuses. The dialog mirrors the service's rule so
// inline validation and dispatch agree instead of the validator blessing a
// rename the service then rejects.
func (a *App) projectContainerCaseCollides(projName, newName string) bool {
	if a.config == nil || a.config.Paths == nil {
		return false
	}
	root := strings.TrimSpace(a.config.Paths.WorkspacesRoot)
	same, ok := data.SamePhysicalDir(filepath.Join(root, projName), filepath.Join(root, newName))
	return ok && same
}

// dialogResultRenameProject validates the confirmed value and dispatches the
// rename request. The registry re-validates (uniqueness under concurrency);
// this pass exists to give the user immediate feedback.
func dialogResultRenameProject(a *App, result common.DialogResult, dlg dialogContext) tea.Cmd {
	if result.Value == "" {
		return a.toast.ShowWarning("Project name is required")
	}
	name := validation.SanitizeInput(result.Value)
	if err := validation.ValidateProjectName(name); err != nil {
		return func() tea.Msg {
			return messages.Error{Err: err, Context: errorContext(errorServiceDialog, "validating project name")}
		}
	}
	proj := dlg.project
	if proj == nil && a != nil {
		// Result handlers receive the dialog's captured context; a nil
		// project here means a hand-routed result — resolve by nothing else,
		// refuse rather than guess.
		return a.toast.ShowWarning("Project context lost; reopen the dialog")
	}
	logging.Info("Renaming project from dialog: %s -> %s", proj.Path, name)
	return func() tea.Msg {
		return messages.RenameProject{Project: proj, NewName: name}
	}
}
