package app

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
	"github.com/andyrewlee/amux/internal/update"
	"github.com/andyrewlee/amux/internal/validation"
)

// handleDialogResult handles dialog completion
func (a *App) handleDialogResult(result common.DialogResult) tea.Cmd {
	project := a.dialogProject
	workspace := a.dialogWorkspace
	a.dialog = nil
	a.dialogProject = nil
	a.dialogWorkspace = nil
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

	case DialogCreateWorkspace:
		if result.Value != "" && project != nil {
			name := validation.SanitizeInput(result.Value)
			if err := validation.ValidateWorkspaceName(name); err != nil {
				return func() tea.Msg {
					return messages.Error{Err: err, Context: "validating workspace name"}
				}
			}
			return func() tea.Msg {
				return messages.CreateWorkspace{
					Project: project,
					Name:    name,
					Base:    "HEAD",
				}
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
		if a.activeWorkspace != nil {
			assistant := result.Value
			if err := validation.ValidateAssistant(assistant); err != nil {
				return func() tea.Msg {
					return messages.Error{Err: err, Context: "validating assistant"}
				}
			}
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

	// ── Linear dialog results ───────────────────────────────────────────

	case "issue-menu":
		return a.handleIssueMenuSelection(result.Index)

	case "board-search":
		if a.board != nil {
			a.board.Filters.Search = result.Value
			a.updateBoard(a.boardIssues)
		}

	case "board-label-filter":
		if result.Index >= 0 && result.Index < len(a.labelFilterValues) {
			return a.applyLabelFilter(a.labelFilterValues[result.Index])
		}

	case "board-recent-filter":
		if a.board != nil && result.Index >= 0 && result.Index < len(a.recentFilterValues) {
			a.board.Filters.UpdatedWithinDays = a.recentFilterValues[result.Index]
			a.updateBoard(a.boardIssues)
		}

	case "board-account-filter":
		if a.board != nil && result.Index >= 0 && result.Index < len(a.accountFilterValues) {
			a.board.Filters.Account = a.accountFilterValues[result.Index]
			a.updateBoard(a.boardIssues)
		}

	case "board-project-filter":
		if a.board != nil && result.Index >= 0 && result.Index < len(a.projectFilterOptions) {
			a.board.Filters.Project = a.projectFilterOptions[result.Index].ID
			a.updateBoard(a.boardIssues)
		}

	case "oauth-account":
		if result.Index >= 0 && result.Index < len(a.oauthAccountValues) {
			acct := a.oauthAccountValues[result.Index]
			return func() tea.Msg { return messages.StartOAuth{Account: acct} }
		}

	case "edit-issue-title":
		// Store title, show description dialog
		if result.Value != "" {
			a.editIssueTitle = result.Value
		}
		a.dialog = common.NewInputDialog("edit-issue-description", "Edit Issue", "Description (leave blank to keep)")
		a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
		a.dialog.Show()
		return nil // Don't clear dialog yet

	case "edit-issue-description":
		issueID := a.editIssueID
		title := a.editIssueTitle
		desc := a.editIssueDescription
		if result.Value != "" {
			desc = result.Value
		}
		a.editIssueID = ""
		a.editIssueTitle = ""
		a.editIssueDescription = ""
		return a.updateIssue(issueID, title, desc)

	case "rename-branch":
		issueID := a.renameBranchIssueID
		a.renameBranchIssueID = ""
		return a.applyRenameBranch(issueID, result.Value)

	case "subtask-title":
		issueID := a.subtaskParentIssueID
		a.subtaskParentIssueID = ""
		if result.Value != "" {
			return a.createSubtaskWithDetails(issueID, result.Value, "")
		}

	case "change-base":
		issueID := a.changeBaseIssueID
		a.changeBaseIssueID = ""
		return a.changeBaseBranch(issueID, result.Value)

	case "state-picker":
		if a.statePickerIssueID != "" && result.Index >= 0 && result.Index < len(a.statePickerStates) {
			state := a.statePickerStates[result.Index]
			issueID := a.statePickerIssueID
			a.statePickerIssueID = ""
			a.statePickerStates = nil
			issue := a.findIssue(issueID)
			if issue != nil && !a.issueAuthRequired(issue) {
				acct := findAccountForIssue(a.linearService, issue)
				if acct.Name != "" {
					return func() tea.Msg {
						ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
						defer cancel()
						if _, err := a.linearService.UpdateIssueState(ctx, acct, issueID, state.ID); err != nil {
							return messages.Error{Err: err, Context: "set issue state"}
						}
						return messages.RefreshBoard{}
					}
				}
			}
		}

	case "comment":
		issueID := a.commentIssueID
		a.commentIssueID = ""
		if issueID != "" && result.Value != "" {
			return func() tea.Msg {
				return messages.AddIssueComment{IssueID: issueID, Body: result.Value}
			}
		}

	case "diff-comment":
		file := a.diffCommentFile
		side := a.diffCommentSide
		line := a.diffCommentLine
		a.diffCommentFile = ""
		a.diffCommentSide = ""
		a.diffCommentLine = 0
		if result.Value != "" {
			a.addDiffComment(file, side, line, result.Value)
		}

	case "pr-comments":
		// PR comments dialog dismissed; no action needed.

	case "create-issue":
		if result.Value != "" && len(a.createIssueTeams) > 0 {
			team := a.createIssueTeams[0]
			title := result.Value
			return func() tea.Msg {
				acct := findAccountByName(a.linearService, team.Account)
				if acct.Name == "" {
					return messages.Error{Err: fmt.Errorf("no Linear account"), Context: "create issue"}
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if _, err := a.linearService.CreateIssue(ctx, acct, team.TeamID, title, ""); err != nil {
					return messages.Error{Err: err, Context: "create issue"}
				}
				return messages.RefreshBoard{}
			}
		}

	case "attempt-picker":
		if a.attemptPickerIssueID != "" && result.Index >= 0 && result.Index < len(a.attemptPickerBranches) {
			branch := a.attemptPickerBranches[result.Index]
			issueID := a.attemptPickerIssueID
			a.attemptPickerIssueID = ""
			a.attemptPickerBranches = nil
			return a.resumeIssueWorkByBranch(issueID, branch)
		}

	case "edit-preview-url":
		if result.Value != "" && a.previewView != nil {
			a.previewView.URL = result.Value
			if a.drawer != nil {
				a.drawer.SetDevURL(result.Value)
			}
		}

	case "agent-profile-picker":
		if result.Value != "" && a.agentProfileIssueID != "" {
			issueID := a.agentProfileIssueID
			a.agentProfileIssueID = ""
			return a.updateAgentProfile(issueID, result.Value)
		}
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
