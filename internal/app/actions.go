package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/attempt"
	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/linear"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func (a *App) startIssueWork(issueID string, newAttempt bool) tea.Cmd {
	issue := a.findIssue(issueID)
	if issue == nil {
		return a.toast.ShowError("Issue not found")
	}
	a.logActivityEntry(common.ActivityEntry{
		Kind:    common.ActivityTool,
		Summary: fmt.Sprintf("Start work: %s", issue.Identifier),
		Status:  common.StatusSuccess,
	})

	project, projCfg, err := a.chooseProjectForIssue(issue)
	if err != nil {
		return a.toast.ShowError(err.Error())
	}

	attemptMeta := attempt.NewMetadata()
	attemptMeta.IssueID = issue.ID
	attemptMeta.IssueIdentifier = issue.Identifier
	attemptMeta.IssueURL = issue.URL
	attemptMeta.TeamID = issue.Team.ID
	if issue.Project != nil {
		attemptMeta.ProjectID = issue.Project.ID
	}
	attemptMeta.AgentProfile = "claude"
	if newAttempt {
		if existing := a.loadAttemptMetadata(issueID); existing != nil {
			attemptMeta.ParentAttemptID = existing.AttemptID
		}
	}
	if host, err := os.Hostname(); err == nil {
		attemptMeta.Host = host
	}

	branch := attempt.BranchName(projCfg.BranchPrefix, issue.Identifier, attemptMeta.AttemptID)
	worktreeName := worktreeNameForIssue(issue.Identifier, attemptMeta.AttemptID)
	base := "HEAD"
	if a.githubConfig != nil && a.githubConfig.PreferredBaseBranch != "" {
		base = a.githubConfig.PreferredBaseBranch
	}
	attemptMeta.BranchName = branch
	attemptMeta.BaseRef = base

	return func() tea.Msg {
		worktreePath := buildWorktreePath(a.config.Paths.WorkspacesRoot, project.Name, worktreeName)
		wt := data.NewWorkspace(worktreeName, branch, base, project.Path, worktreePath)

		if err := git.CreateWorkspace(project.Path, worktreePath, branch, base); err != nil {
			return messages.Error{Err: err, Context: "create worktree"}
		}
		// Wait for .git to appear (git worktree race)
		gitPath := filepath.Join(worktreePath, ".git")
		for i := 0; i < 10; i++ {
			if _, err := os.Stat(gitPath); err == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		// Verify .git appeared
		if _, err := os.Stat(gitPath); err != nil {
			return messages.Error{Err: fmt.Errorf("worktree .git file not found after creation"), Context: "create worktree"}
		}
		if err := attempt.Save(worktreePath, attemptMeta); err != nil {
			return messages.Error{Err: err, Context: "save attempt metadata"}
		}

		// Set workspace metadata fields directly
		wt.Assistant = "claude"
		wt.ScriptMode = "nonconcurrent"
		wt.Env = map[string]string{"AMUX_ISSUE_ID": issue.ID, "AMUX_ISSUE_KEY": issue.Identifier}

		if err := a.workspaces.Save(wt); err != nil {
			_ = git.RemoveWorkspace(project.Path, worktreePath)
			_ = git.DeleteBranch(project.Path, branch)
			return messages.Error{Err: err, Context: "save worktree metadata"}
		}
		if err := a.scripts.RunSetup(wt); err != nil {
			return messages.WorkspaceCreatedWithWarning{Workspace: wt, Warning: err.Error()}
		}

		// Update Linear state to In Progress and comment
		acct := findAccountForIssue(a.linearService, issue)
		if acct.Name != "" && !a.issueAuthRequired(issue) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			stateID, _ := a.pickStateID(ctx, acct, issue.Team.ID, "started")
			if stateID != "" {
				_, _ = a.linearService.UpdateIssueState(ctx, acct, issue.ID, stateID)
			}
			comment := fmt.Sprintf("Started work on %s (%s)", branch, worktreePath)
			_ = a.linearService.CreateComment(ctx, acct, issue.ID, comment)
		}

		return messages.WorkspaceCreated{Workspace: wt}
	}
}

func (a *App) resumeIssueWork(issueID string) tea.Cmd {
	wt := a.findWorktreeByIssue(issueID)
	if wt == nil {
		return a.toast.ShowError("No worktree found for issue")
	}
	project := a.findProjectByPath(wt.Repo)
	return func() tea.Msg {
		return messages.WorkspaceActivated{Project: project, Workspace: wt}
	}
}

func (a *App) resumeIssueWorkByBranch(issueID, branch string) tea.Cmd {
	wt := a.findWorktreeByIssueAndBranch(issueID, branch)
	if wt == nil {
		return a.toast.ShowError("No matching attempt found")
	}
	project := a.findProjectByPath(wt.Repo)
	return func() tea.Msg {
		return messages.WorkspaceActivated{Project: project, Workspace: wt}
	}
}

func (a *App) runAgentForIssue(issueID string) tea.Cmd {
	wt := a.findWorktreeByIssue(issueID)
	if wt == nil {
		return a.toast.ShowError("No worktree found for issue")
	}
	delete(a.nextActions, issueID)
	if a.inspector != nil && a.selectedIssue != nil && a.selectedIssue.ID == issueID {
		a.inspector.SetNextActionSummary("", "")
	}
	entryID := a.logActivityEntry(common.ActivityEntry{
		Kind:      common.ActivityCommand,
		Summary:   fmt.Sprintf("Run agent: %s", wt.Branch),
		Status:    common.StatusRunning,
		ProcessID: string(wt.ID()),
	})
	a.agentActivityIDs[string(wt.ID())] = entryID
	// Load workspace metadata to get assistant profile
	_, _ = a.workspaces.LoadMetadataFor(wt)
	assistant := wt.Assistant
	if assistant == "" {
		assistant = "claude"
	}
	return func() tea.Msg {
		return messages.LaunchAgent{Assistant: assistant, Workspace: wt}
	}
}

func (a *App) createPRForIssue(issueID string) tea.Cmd {
	issue := a.findIssue(issueID)
	if issue == nil {
		return a.toast.ShowError("Issue not found")
	}
	if a.center != nil && a.center.HasRunningIssue(issueID) {
		return a.toast.ShowInfo("Agent is running; stop it before creating a PR")
	}
	if a.issueHasConflicts(issueID) {
		return a.toast.ShowInfo("Resolve conflicts before creating a PR")
	}
	wt := a.findWorktreeByIssue(issueID)
	if wt == nil {
		return a.toast.ShowError("No worktree found for issue")
	}
	a.logActivityEntry(common.ActivityEntry{
		Kind:      common.ActivityCommand,
		Summary:   fmt.Sprintf("Create PR: %s", wt.Branch),
		Status:    common.StatusRunning,
		ProcessID: string(wt.ID()),
	})
	return func() tea.Msg {
		title := fmt.Sprintf("%s %s", issue.Identifier, issue.Title)
		body := fmt.Sprintf("Closes %s", issue.URL)
		base := wt.Base
		if base == "" {
			base = a.githubConfig.PreferredBaseBranch
		}
		if base == "" {
			base = "main"
		}
		cmd := exec.Command("gh", "pr", "create", "--base", base, "--head", wt.Branch, "--title", title, "--body", body)
		cmd.Dir = wt.Root
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			return messages.Error{Err: fmt.Errorf("gh pr create failed: %s", out.String()), Context: "create PR"}
		}
		prURL := strings.TrimSpace(out.String())
		if strings.HasPrefix(prURL, "http") {
			meta, _ := attempt.Load(wt.Root)
			if meta != nil {
				meta.PRURL = prURL
				if err := attempt.Save(wt.Root, meta); err != nil {
					logging.Warn("Failed to save PR URL to metadata: %v", err)
				}
			}
			acct := findAccountForIssue(a.linearService, issue)
			if acct.Name != "" && !a.issueAuthRequired(issue) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				alreadyLinked := false
				if comments, err := a.linearService.FetchIssueComments(ctx, acct, issue.ID); err == nil {
					for _, comment := range comments {
						if strings.Contains(comment.Body, prURL) {
							alreadyLinked = true
							break
						}
					}
				}
				if !alreadyLinked {
					_ = a.linearService.CreateComment(ctx, acct, issue.ID, "PR created: "+prURL)
				}
				if a.githubConfig != nil && a.githubConfig.AutoMoveToReviewOnPR {
					if stateID, _ := a.pickStateID(ctx, acct, issue.Team.ID, "review"); stateID != "" {
						_, _ = a.linearService.UpdateIssueState(ctx, acct, issue.ID, stateID)
					}
				}
			}
			return messages.LogActivity{
				Line:      "PR created: " + prURL,
				Kind:      "command",
				Status:    "success",
				ProcessID: string(wt.ID()),
			}
		}
		return nil
	}
}

func (a *App) chooseProjectForIssue(issue *linear.Issue) (*data.Project, *config.ProjectConfig, error) {
	var candidates []*data.Project
	var configs []*config.ProjectConfig
	for i := range a.projects {
		project := &a.projects[i]
		cfg, err := config.LoadProjectConfig(project.Path)
		if err != nil {
			logging.Warn("Failed to load project config: %v", err)
			continue
		}
		if cfg.Tracker != "linear" {
			continue
		}
		candidates = append(candidates, project)
		configs = append(configs, cfg)
	}

	if len(candidates) == 0 {
		return nil, nil, fmt.Errorf("no projects configured for Linear")
	}
	if len(candidates) == 1 {
		return candidates[0], configs[0], nil
	}

	// Try to match team key or project ID.
	for idx, project := range candidates {
		cfg := configs[idx]
		if cfg.LinearTeamKey != "" && strings.EqualFold(cfg.LinearTeamKey, issue.Team.Key) {
			return project, cfg, nil
		}
		if cfg.LinearProjectID != "" && issue.Project != nil && cfg.LinearProjectID == issue.Project.ID {
			return project, cfg, nil
		}
		if strings.EqualFold(project.Name, issue.Team.Key) {
			return project, cfg, nil
		}
	}
	return nil, nil, fmt.Errorf("multiple Linear projects; set linearTeamKey in .amux/project.json")
}

func (a *App) pickStateID(ctx context.Context, acct linear.AccountConfig, teamID, stateType string) (string, error) {
	states, err := a.linearService.FetchTeamStates(ctx, acct, teamID)
	if err != nil {
		return "", err
	}
	for _, state := range states {
		if strings.EqualFold(state.Type, stateType) {
			return state.ID, nil
		}
	}
	return "", nil
}

func worktreeNameForIssue(identifier, attemptID string) string {
	base := strings.ToLower(strings.ReplaceAll(identifier, " ", "-"))
	base = strings.ReplaceAll(base, "/", "-")
	short := attempt.ShortID(attemptID)
	if short != "" {
		return base + "-" + short
	}
	return base
}

func buildWorktreePath(root, projectName, worktreeName string) string {
	return filepath.Join(root, projectName, worktreeName)
}

func findAccountByName(service *linear.Service, name string) linear.AccountConfig {
	if name == "" {
		accounts := service.ActiveAccounts()
		if len(accounts) == 1 {
			return accounts[0]
		}
	}
	for _, acct := range service.ActiveAccounts() {
		if acct.Name == name {
			return acct
		}
	}
	return linear.AccountConfig{}
}
