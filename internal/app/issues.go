package app

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/andyrewlee/amux/internal/attempt"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/linear"
	"github.com/andyrewlee/amux/internal/ui/inspector"
)

func (a *App) selectIssue(issueID string) {
	issue := a.findIssue(issueID)
	a.selectedIssue = issue
	a.inspector.SetIssue(issue)
	a.inspector.SetAttempts(a.buildAttempts(issueID))
	a.inspector.SetComments(nil)
	a.inspector.SetConflict(a.issueHasConflicts(issueID))
	a.inspector.SetLogs(a.activityLog)
	a.inspector.SetReviewPreview(a.buildReviewMessage())
	if summary, ok := a.nextActions[issueID]; ok {
		a.inspector.SetNextActionSummary(summary.Summary, summary.Status)
	} else {
		a.inspector.SetNextActionSummary("", "")
	}
	wt := a.findWorktreeByIssue(issueID)
	a.inspector.SetHasWorktree(wt != nil)
	a.inspector.SetAgentProfile("")
	a.inspector.SetAttemptBranch("")
	a.inspector.SetRepoName("")
	a.inspector.SetParentAttempt(a.parentAttemptBranch(issueID))
	if wt != nil {
		if meta, _ := attempt.Load(wt.Root); meta != nil {
			a.inspector.SetAgentProfile(meta.AgentProfile)
		} else {
			// Load workspace metadata and use assistant field
			_, _ = a.workspaces.LoadMetadataFor(wt)
			a.inspector.SetAgentProfile(wt.Assistant)
		}
		a.inspector.SetAttemptBranch(wt.Branch)
		if project := a.findProjectByPath(wt.Repo); project != nil {
			a.inspector.SetRepoName(project.Name)
		}
		a.inspector.SetScriptRunning(a.scripts != nil && a.scripts.IsRunning(wt))
		if a.scripts != nil && a.scripts.IsRunning(wt) {
			a.previewView.SetRunning(true)
		} else {
			a.previewView.SetRunning(false)
		}
		if a.statusManager != nil {
			a.updateInspectorGitLine(issueID)
		}
		if msg, ok := a.pendingAgentMessages[wt.Root]; ok {
			a.inspector.SetQueuedMessage(msg)
		} else {
			a.inspector.SetQueuedMessage("")
		}
	} else {
		a.inspector.SetScriptRunning(false)
		a.inspector.SetQueuedMessage("")
		a.inspector.SetGitLine("")
	}
	if issue != nil {
		a.inspector.SetAuthRequired(a.issueAuthRequired(issue))
		if info, ok := a.prStatus(issue.ID); ok {
			a.inspector.SetPR(info.URL, info.State, info.Number)
		} else if wt != nil {
			if meta, _ := attempt.Load(wt.Root); meta != nil && meta.PRURL != "" {
				a.inspector.SetPR(meta.PRURL, "", 0)
			} else {
				a.inspector.SetPR("", "", 0)
			}
		} else {
			a.inspector.SetPR("", "", 0)
		}
	}
	if issue != nil {
		running := a.center != nil && a.center.HasRunningIssue(issue.ID)
		a.inspector.SetAgentRunning(running)
		if running {
			a.inspector.SetMode(inspector.ModeAttempt)
		} else if wt := a.findWorktreeByIssue(issue.ID); wt != nil && a.activeWorkspace != nil && wt.Root == a.activeWorkspace.Root {
			a.inspector.SetMode(inspector.ModeAttempt)
		} else {
			a.inspector.SetMode(inspector.ModeTask)
		}
	}
}

func (a *App) findIssue(issueID string) *linear.Issue {
	for i := range a.boardIssues {
		if a.boardIssues[i].ID == issueID {
			return &a.boardIssues[i]
		}
	}
	return nil
}

func (a *App) findProjectByPath(path string) *data.Project {
	for i := range a.projects {
		if a.projects[i].Path == path {
			return &a.projects[i]
		}
	}
	return nil
}

func (a *App) findWorktreeByIssueAndBranch(issueID, branch string) *data.Workspace {
	for i := range a.projects {
		project := &a.projects[i]
		for j := range project.Workspaces {
			wt := &project.Workspaces[j]
			if wt.Branch != branch {
				continue
			}
			meta, err := attempt.Load(wt.Root)
			if err != nil || meta == nil {
				continue
			}
			if meta.IssueID == issueID {
				return wt
			}
		}
	}
	return nil
}

func (a *App) buildAttempts(issueID string) []inspector.AttemptInfo {
	if issueID == "" {
		return nil
	}
	attempts := []inspector.AttemptInfo{}
	for i := range a.projects {
		project := &a.projects[i]
		for j := range project.Workspaces {
			wt := &project.Workspaces[j]
			meta, err := attempt.Load(wt.Root)
			if err != nil || meta == nil {
				continue
			}
			if meta.IssueID != issueID {
				continue
			}
			attempts = append(attempts, inspector.AttemptInfo{
				Branch:   wt.Branch,
				Executor: meta.AgentProfile,
				Updated:  relativeTime(meta.LastSyncedAt),
				Status:   meta.Status,
			})
		}
	}
	return attempts
}

func (a *App) parentAttemptBranch(issueID string) string {
	if issueID == "" {
		return ""
	}
	for i := range a.projects {
		project := &a.projects[i]
		for j := range project.Workspaces {
			wt := &project.Workspaces[j]
			meta, err := attempt.Load(wt.Root)
			if err != nil || meta == nil {
				continue
			}
			if meta.IssueID != issueID {
				continue
			}
			if meta.ParentAttemptID == "" {
				continue
			}
			if branch := a.findAttemptBranchByID(meta.ParentAttemptID); branch != "" {
				return branch
			}
		}
	}
	return ""
}

func (a *App) findAttemptBranchByID(attemptID string) string {
	if attemptID == "" {
		return ""
	}
	for i := range a.projects {
		project := &a.projects[i]
		for j := range project.Workspaces {
			wt := &project.Workspaces[j]
			meta, err := attempt.Load(wt.Root)
			if err != nil || meta == nil {
				continue
			}
			if meta.AttemptID == attemptID {
				return wt.Branch
			}
		}
	}
	return ""
}

func (a *App) issueHasConflicts(issueID string) bool {
	wt := a.findWorktreeByIssue(issueID)
	if wt == nil {
		return false
	}
	// Check if .git/MERGE_HEAD or .git/rebase-merge exists (indicates conflicts)
	rebaseMerge := filepath.Join(wt.Root, ".git", "rebase-merge")
	if _, err := os.Stat(rebaseMerge); err == nil {
		return true
	}
	mergeHead := filepath.Join(wt.Root, ".git", "MERGE_HEAD")
	if _, err := os.Stat(mergeHead); err == nil {
		return true
	}
	return false
}

func (a *App) updateInspectorGitLine(issueID string) {
	if a.inspector == nil {
		return
	}
	wt := a.findWorktreeByIssue(issueID)
	if wt == nil || a.statusManager == nil {
		a.inspector.SetGitLine("")
		a.inspector.SetGitInfo(inspector.GitInfo{})
		return
	}
	status := a.statusManager.GetCached(wt.Root)
	if status == nil {
		a.inspector.SetGitLine("")
		a.inspector.SetGitInfo(inspector.GitInfo{})
		return
	}
	base := wt.Base
	if base == "" && a.githubConfig != nil {
		base = a.githubConfig.PreferredBaseBranch
	}
	if base == "" {
		base = "main"
	}
	line := fmt.Sprintf("Git: %s → %s • %s", wt.Branch, base, status.GetStatusSummary())
	a.inspector.SetGitLine(line)
	ahead, behind, _ := git.AheadBehind(wt.Root, base, wt.Branch)
	info := inspector.GitInfo{
		Branch:           wt.Branch,
		Base:             base,
		Summary:          status.GetStatusSummary(),
		Ahead:            ahead,
		Behind:           behind,
		RebaseInProgress: git.RebaseInProgress(wt.Root),
	}
	a.inspector.SetGitInfo(info)
}

func relativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func (a *App) issueAuthRequired(issue *linear.Issue) bool {
	if issue == nil {
		return false
	}
	for _, acct := range a.authMissingAccounts {
		if acct == issue.Account {
			return true
		}
	}
	return false
}

func findAccountForIssue(service *linear.Service, issue *linear.Issue) linear.AccountConfig {
	if service == nil || issue == nil {
		return linear.AccountConfig{}
	}
	for _, acct := range service.ActiveAccounts() {
		if acct.Name == issue.Account {
			return acct
		}
	}
	return linear.AccountConfig{}
}
