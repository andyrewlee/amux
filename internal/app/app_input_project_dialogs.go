package app

// Project-scoped dialog handlers live here — split from
// app_input_messages_dialogs.go under the 500-line file cap.

import (
	"fmt"

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
