package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/attempt"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func (a *App) createSubtask(issueID string) tea.Cmd {
	issue := a.findIssue(issueID)
	if issue == nil {
		return a.toast.ShowError("Issue not found")
	}
	if a.issueAuthRequired(issue) {
		return a.toast.ShowError("Auth required for this account")
	}
	if wt := a.findWorktreeByIssue(issueID); wt == nil {
		return a.toast.ShowError("Start work before creating subtasks")
	}
	a.subtaskParentIssueID = issueID
	a.dialog = common.NewInputDialog("subtask-title", "New Subtask", "Subtask title...")

	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	return nil
}

func (a *App) createSubtaskWithDetails(parentID, title, description string) tea.Cmd {
	parent := a.findIssue(parentID)
	if parent == nil {
		return a.toast.ShowError("Parent issue not found")
	}
	if a.issueAuthRequired(parent) {
		return a.toast.ShowError("Auth required for this account")
	}
	parentWT := a.findWorktreeByIssue(parentID)
	if parentWT == nil {
		return a.toast.ShowError("No parent worktree found")
	}
	project, projCfg, err := a.chooseProjectForIssue(parent)
	if err != nil {
		return a.toast.ShowError(err.Error())
	}
	parentMeta, _ := attempt.Load(parentWT.Root)
	parentAttemptID := ""
	agentProfile := "claude"
	if parentMeta != nil {
		parentAttemptID = parentMeta.AttemptID
		if parentMeta.AgentProfile != "" {
			agentProfile = parentMeta.AgentProfile
		}
	}

	return func() tea.Msg {
		acct := findAccountForIssue(a.linearService, parent)
		if acct.Name == "" {
			return messages.Error{Err: fmt.Errorf("no Linear account"), Context: "create subtask"}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		newIssue, err := a.linearService.CreateIssue(ctx, acct, parent.Team.ID, title, description)
		if err != nil {
			return messages.Error{Err: err, Context: "create subtask"}
		}
		// Hydrate minimal fields from parent issue for local usage.
		newIssue.Team = parent.Team
		newIssue.Project = parent.Project
		newIssue.Account = parent.Account

		attemptMeta := attempt.NewMetadata()
		attemptMeta.IssueID = newIssue.ID
		attemptMeta.IssueIdentifier = newIssue.Identifier
		attemptMeta.IssueURL = newIssue.URL
		attemptMeta.TeamID = parent.Team.ID
		if parent.Project != nil {
			attemptMeta.ProjectID = parent.Project.ID
		}
		attemptMeta.ParentAttemptID = parentAttemptID
		attemptMeta.AgentProfile = agentProfile
		attemptMeta.BaseRef = parentWT.Branch
		attemptMeta.BranchName = attempt.BranchName(projCfg.BranchPrefix, newIssue.Identifier, attemptMeta.AttemptID)
		if host, err := os.Hostname(); err == nil {
			attemptMeta.Host = host
		}

		worktreeName := worktreeNameForIssue(newIssue.Identifier, attemptMeta.AttemptID)
		worktreePath := buildWorktreePath(a.config.Paths.WorkspacesRoot, project.Name, worktreeName)
		wt := data.NewWorkspace(worktreeName, attemptMeta.BranchName, attemptMeta.BaseRef, project.Path, worktreePath)

		if err := git.CreateWorkspace(project.Path, worktreePath, attemptMeta.BranchName, attemptMeta.BaseRef); err != nil {
			return messages.Error{Err: err, Context: "create subtask worktree"}
		}
		// Wait for .git to appear (git worktree race)
		gitPath := filepath.Join(worktreePath, ".git")
		for i := 0; i < 10; i++ {
			if _, err := os.Stat(gitPath); err == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if err := attempt.Save(worktreePath, attemptMeta); err != nil {
			_ = git.RemoveWorkspace(project.Path, worktreePath)
			_ = git.DeleteBranch(project.Path, attemptMeta.BranchName)
			return messages.Error{Err: err, Context: "save subtask attempt"}
		}

		// Set workspace metadata fields directly
		wt.Assistant = agentProfile
		wt.ScriptMode = "nonconcurrent"
		wt.Env = map[string]string{
			"AMUX_ISSUE_ID":  newIssue.ID,
			"AMUX_ISSUE_KEY": newIssue.Identifier,
		}

		if err := a.workspaces.Save(wt); err != nil {
			_ = git.RemoveWorkspace(project.Path, worktreePath)
			_ = git.DeleteBranch(project.Path, attemptMeta.BranchName)
			return messages.Error{Err: err, Context: "save subtask metadata"}
		}
		if err := a.scripts.RunSetup(wt); err != nil {
			return messages.WorkspaceCreatedWithWarning{Workspace: wt, Warning: err.Error()}
		}

		// Update Linear state and comment
		stateID, _ := a.pickStateID(ctx, acct, parent.Team.ID, "started")
		if stateID != "" {
			_, _ = a.linearService.UpdateIssueState(ctx, acct, newIssue.ID, stateID)
		}
		_ = a.linearService.CreateComment(ctx, acct, newIssue.ID, fmt.Sprintf("Subtask of %s (base %s)", parent.Identifier, parentWT.Branch))
		_ = a.linearService.CreateComment(ctx, acct, parent.ID, fmt.Sprintf("Created subtask %s (%s)", newIssue.Identifier, attemptMeta.BranchName))

		return messages.WorkspaceCreated{Workspace: wt}
	}
}

func (a *App) duplicateIssue(issueID string) tea.Cmd {
	issue := a.findIssue(issueID)
	if issue == nil {
		return a.toast.ShowError("Issue not found")
	}
	if a.issueAuthRequired(issue) {
		return a.toast.ShowError("Auth required for this account")
	}
	return func() tea.Msg {
		acct := findAccountForIssue(a.linearService, issue)
		if acct.Name == "" {
			return messages.Error{Err: fmt.Errorf("no Linear account"), Context: "duplicate issue"}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		title := strings.TrimSpace(issue.Title)
		if title == "" {
			title = issue.Identifier
		}
		title = title + " (copy)"
		newIssue, err := a.linearService.CreateIssue(ctx, acct, issue.Team.ID, title, issue.Description)
		if err != nil {
			return messages.Error{Err: err, Context: "duplicate issue"}
		}
		_ = a.linearService.CreateComment(ctx, acct, newIssue.ID, fmt.Sprintf("Duplicated from %s", issue.Identifier))
		_ = a.linearService.CreateComment(ctx, acct, issue.ID, fmt.Sprintf("Duplicated as %s", newIssue.Identifier))
		return messages.RefreshBoard{}
	}
}

func (a *App) cancelIssue(issueID string) tea.Cmd {
	issue := a.findIssue(issueID)
	if issue == nil {
		return a.toast.ShowError("Issue not found")
	}
	if a.issueAuthRequired(issue) {
		return a.toast.ShowError("Auth required for this account")
	}
	return func() tea.Msg {
		acct := findAccountForIssue(a.linearService, issue)
		if acct.Name == "" {
			return messages.Error{Err: fmt.Errorf("no Linear account"), Context: "cancel issue"}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		stateID, _ := a.pickStateID(ctx, acct, issue.Team.ID, "canceled")
		if stateID == "" {
			stateID, _ = a.pickStateID(ctx, acct, issue.Team.ID, "completed")
		}
		if stateID == "" {
			return messages.Error{Err: fmt.Errorf("no cancel state"), Context: "cancel issue"}
		}
		if _, err := a.linearService.UpdateIssueState(ctx, acct, issue.ID, stateID); err != nil {
			logging.Warn("Failed to cancel issue: %v", err)
		}
		return messages.RefreshBoard{}
	}
}
