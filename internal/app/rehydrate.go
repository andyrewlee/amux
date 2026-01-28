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
	"github.com/andyrewlee/amux/internal/messages"
)

func (a *App) rehydrateIssueWorktree(issueID string) tea.Cmd {
	issue := a.findIssue(issueID)
	if issue == nil {
		return a.toast.ShowError("Issue not found")
	}
	if wt := a.findWorktreeByIssue(issueID); wt != nil {
		return a.toast.ShowInfo("Worktree already exists")
	}

	project, projCfg, err := a.chooseProjectForIssue(issue)
	if err != nil {
		return a.toast.ShowError(err.Error())
	}

	return func() tea.Msg {
		branch, remote, err := findIssueBranch(project.Path, projCfg.BranchPrefix, issue.Identifier)
		if err != nil {
			return messages.Error{Err: err, Context: "rehydrate worktree"}
		}
		if branch == "" {
			return messages.Error{Err: fmt.Errorf("no branch found for issue"), Context: "rehydrate worktree"}
		}

		attemptID := branchAttemptID(branch)
		if attemptID == "" {
			attemptID = attempt.NewAttemptID()
		}
		worktreeName := worktreeNameForIssue(issue.Identifier, attemptID)
		worktreePath := buildWorktreePath(a.config.Paths.WorkspacesRoot, project.Name, worktreeName)

		if err := os.MkdirAll(filepath.Dir(worktreePath), 0755); err != nil {
			return messages.Error{Err: err, Context: "rehydrate worktree"}
		}

		if remote {
			if _, err := git.RunGit(project.Path, "fetch", "origin", branch); err != nil {
				return messages.Error{Err: err, Context: "rehydrate worktree"}
			}
			if _, err := git.RunGit(project.Path, "worktree", "add", "-b", branch, worktreePath, "origin/"+branch); err != nil {
				return messages.Error{Err: err, Context: "rehydrate worktree"}
			}
		} else {
			if _, err := git.RunGit(project.Path, "worktree", "add", worktreePath, branch); err != nil {
				return messages.Error{Err: err, Context: "rehydrate worktree"}
			}
		}

		base := "HEAD"
		if a.githubConfig != nil && a.githubConfig.PreferredBaseBranch != "" {
			base = a.githubConfig.PreferredBaseBranch
		}

		attemptMeta := attempt.NewMetadata()
		attemptMeta.AttemptID = attemptID
		attemptMeta.IssueID = issue.ID
		attemptMeta.IssueIdentifier = issue.Identifier
		attemptMeta.IssueURL = issue.URL
		attemptMeta.TeamID = issue.Team.ID
		if issue.Project != nil {
			attemptMeta.ProjectID = issue.Project.ID
		}
		attemptMeta.BranchName = branch
		attemptMeta.BaseRef = base
		attemptMeta.LastSyncedAt = time.Now()
		if host, err := os.Hostname(); err == nil {
			attemptMeta.Host = host
		}
		_ = attempt.Save(worktreePath, attemptMeta)

		wt := data.NewWorkspace(worktreeName, branch, base, project.Path, worktreePath)
		// Set workspace metadata fields directly
		wt.Assistant = "claude"
		wt.ScriptMode = "nonconcurrent"
		wt.Env = map[string]string{"AMUX_ISSUE_ID": issue.ID, "AMUX_ISSUE_KEY": issue.Identifier}

		if err := a.workspaces.Save(wt); err != nil {
			return messages.Error{Err: err, Context: "rehydrate worktree metadata"}
		}

		// Update Linear state to started if possible.
		acct := findAccountForIssue(a.linearService, issue)
		if acct.Name != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if stateID, _ := a.pickStateID(ctx, acct, issue.Team.ID, "started"); stateID != "" {
				_, _ = a.linearService.UpdateIssueState(ctx, acct, issue.ID, stateID)
			}
		}

		return messages.WorkspaceCreated{Workspace: wt}
	}
}

func findIssueBranch(repoPath, prefix, identifier string) (string, bool, error) {
	pattern := fmt.Sprintf("%s/%s/*", prefix, identifier)
	if prefix == "" {
		pattern = fmt.Sprintf("*%s*", identifier)
	}

	localOut, err := git.RunGit(repoPath, "branch", "--list", pattern)
	if err != nil {
		return "", false, err
	}
	if branch := firstBranchFromList(localOut); branch != "" {
		return branch, false, nil
	}

	remoteOut, err := git.RunGit(repoPath, "branch", "-r", "--list", "origin/"+pattern)
	if err != nil {
		return "", false, err
	}
	if branch := firstBranchFromList(remoteOut); branch != "" {
		return strings.TrimPrefix(branch, "origin/"), true, nil
	}

	return "", false, nil
}

func firstBranchFromList(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func branchAttemptID(branch string) string {
	parts := strings.Split(branch, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}
