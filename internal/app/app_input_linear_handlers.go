package app

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/drawer"
)

// ── Stub / Helper Handlers ──────────────────────────────────────────────

func (a *App) handleBoardIssuesLoaded(msg messages.BoardIssuesLoaded) tea.Cmd {
	if msg.Err != nil {
		logging.Warn("BoardIssuesLoaded error: %v", msg.Err)
		if msg.Cached {
			return nil
		}
		return a.toast.ShowWarning("Failed to load issues")
	}
	a.boardIssues = msg.Issues
	a.updateBoard(a.boardIssues)
	a.updateTeamOptions(a.boardIssues)
	a.updateProjectOptions(a.boardIssues)
	a.updateAuthStatus()
	if a.selectedIssue != nil {
		a.selectIssue(a.selectedIssue.ID)
	}
	// After loading issues, fetch PR status for issues with worktrees.
	var cmds []tea.Cmd
	for _, issue := range a.boardIssues {
		if wt := a.findWorktreeByIssue(issue.ID); wt != nil {
			if cmd := a.fetchPRStatus(issue.ID); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	return tea.Batch(cmds...)
}

func (a *App) handleMoveIssueState(issueID string) tea.Cmd {
	issue := a.findIssue(issueID)
	if issue == nil {
		return a.toast.ShowError("Issue not found")
	}
	if a.issueAuthRequired(issue) {
		return a.toast.ShowError("Auth required for this account")
	}
	acct := findAccountForIssue(a.linearService, issue)
	if acct.Name == "" {
		return a.toast.ShowError("No Linear account")
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		states, err := a.linearService.FetchTeamStates(ctx, acct, issue.Team.ID)
		if err != nil {
			return messages.Error{Err: err, Context: "fetch states"}
		}
		return messages.IssueStatesLoaded{IssueID: issueID, States: states}
	}
}

func (a *App) handleSetIssueStateType(issueID, stateType string) tea.Cmd {
	issue := a.findIssue(issueID)
	if issue == nil {
		return a.toast.ShowError("Issue not found")
	}
	if a.issueAuthRequired(issue) {
		return a.toast.ShowError("Auth required for this account")
	}
	acct := findAccountForIssue(a.linearService, issue)
	if acct.Name == "" {
		return a.toast.ShowError("No Linear account")
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		stateID, _ := a.pickStateID(ctx, acct, issue.Team.ID, stateType)
		if stateID == "" {
			return messages.Error{Err: fmt.Errorf("no %s state found", stateType), Context: "set issue state"}
		}
		if _, err := a.linearService.UpdateIssueState(ctx, acct, issue.ID, stateID); err != nil {
			return messages.Error{Err: err, Context: "set issue state"}
		}
		return messages.RefreshBoard{}
	}
}

func (a *App) handleAddIssueComment(issueID, body string) tea.Cmd {
	issue := a.findIssue(issueID)
	if issue == nil {
		return a.toast.ShowError("Issue not found")
	}
	if a.issueAuthRequired(issue) {
		return a.toast.ShowError("Auth required for this account")
	}
	acct := findAccountForIssue(a.linearService, issue)
	if acct.Name == "" {
		return a.toast.ShowError("No Linear account")
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := a.linearService.CreateComment(ctx, acct, issue.ID, body); err != nil {
			return messages.Error{Err: err, Context: "add comment"}
		}
		return messages.LogActivity{Line: "Comment added", Kind: "info", Status: "success"}
	}
}

func (a *App) handleOpenIssueDiff(issueID string) tea.Cmd {
	a.diffIssueID = issueID
	a.auxMode = AuxDiff
	return a.refreshDiff(false)
}

func (a *App) handleDiffLoaded(msg messages.DiffLoaded) tea.Cmd {
	if msg.Err != nil {
		return a.toast.ShowError("Diff failed: " + msg.Err.Error())
	}
	if a.diffView != nil {
		a.diffView.SetFiles(msg.Files)
	}
	return nil
}

func (a *App) handlePRCommentsLoaded(msg messages.PRCommentsLoaded) tea.Cmd {
	if msg.Err != nil {
		return a.toast.ShowWarning("Failed to load PR comments: " + msg.Err.Error())
	}
	a.prCommentIssueID = msg.IssueID
	a.prCommentOptions = msg.Options
	if len(msg.Options) == 0 {
		return a.toast.ShowInfo("No PR comments found")
	}
	a.dialog = common.NewSelectDialog("pr-comments", "PR Comments", "Select comment:", msg.Options)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	return nil
}

func (a *App) handleSendFollowUp(issueID, body string) tea.Cmd {
	wt := a.findWorktreeByIssue(issueID)
	if wt == nil {
		return a.toast.ShowError("No worktree found for issue")
	}
	if a.center != nil && a.center.HasRunningIssue(issueID) {
		a.center.SendToTerminalForWorktreeID(string(wt.ID()), body+"\n")
		return a.toast.ShowInfo("Message sent to agent")
	}
	a.pendingAgentMessages[wt.Root] = body
	if a.selectedIssue != nil && a.selectedIssue.ID == issueID {
		a.inspector.SetQueuedMessage(body)
	}
	return a.toast.ShowInfo("Message queued for next agent run")
}

func (a *App) handleRebaseWorkspace(issueID string) tea.Cmd {
	wt := a.findWorktreeByIssue(issueID)
	if wt == nil {
		return a.toast.ShowError("No worktree found for issue")
	}
	if a.center != nil && a.center.HasRunningIssue(issueID) {
		return a.toast.ShowInfo("Agent is running; stop it before rebasing")
	}
	base := wt.Base
	if base == "" && a.githubConfig != nil {
		base = a.githubConfig.PreferredBaseBranch
	}
	if base == "" {
		base = "main"
	}
	return func() tea.Msg {
		if _, err := git.RunGit(wt.Root, "rebase", base); err != nil {
			return messages.Error{Err: err, Context: "rebase"}
		}
		return messages.LogActivity{Line: "Rebase complete", Kind: "command", Status: "success"}
	}
}

func (a *App) handleAbortRebase(issueID string) tea.Cmd {
	wt := a.findWorktreeByIssue(issueID)
	if wt == nil {
		return a.toast.ShowError("No worktree found for issue")
	}
	return func() tea.Msg {
		if _, err := git.RunGit(wt.Root, "rebase", "--abort"); err != nil {
			return messages.Error{Err: err, Context: "abort rebase"}
		}
		return messages.LogActivity{Line: "Rebase aborted", Kind: "command", Status: "success"}
	}
}

func (a *App) handleOAuthCompleted(msg messages.OAuthCompleted) tea.Cmd {
	if msg.Err != nil {
		return a.toast.ShowError("OAuth failed: " + msg.Err.Error())
	}
	if msg.Token != "" && msg.Account != "" && a.linearConfig != nil {
		// Persist the token to the account configuration.
		for i := range a.linearConfig.Accounts {
			if a.linearConfig.Accounts[i].Name == msg.Account {
				a.linearConfig.Accounts[i].Auth.AccessToken = msg.Token
				break
			}
		}
		a.updateAuthStatus()
		return a.refreshBoard(true)
	}
	return a.toast.ShowSuccess("OAuth complete")
}

func (a *App) handleShowBoardSearchDialog() tea.Cmd {
	if a.dialog != nil && a.dialog.Visible() {
		return nil
	}
	a.dialog = common.NewInputDialog("board-search", "Search Issues", "Search...")
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	return nil
}

func (a *App) handleShowAccountFilterDialog() tea.Cmd {
	if a.dialog != nil && a.dialog.Visible() {
		return nil
	}
	accounts := a.linearService.ActiveAccounts()
	if len(accounts) == 0 {
		return a.toast.ShowInfo("No Linear accounts configured")
	}
	options := []string{"All accounts"}
	values := []string{""}
	for _, acct := range accounts {
		options = append(options, acct.Name)
		values = append(values, acct.Name)
	}
	a.accountFilterValues = values
	a.dialog = common.NewSelectDialog("board-account-filter", "Account Filter", "Select account:", options)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	return nil
}

func (a *App) handleShowProjectFilterDialog() tea.Cmd {
	if a.dialog != nil && a.dialog.Visible() {
		return nil
	}
	if len(a.projectFilterOptions) == 0 {
		return a.toast.ShowInfo("No projects found")
	}
	options := make([]string, len(a.projectFilterOptions))
	for i, opt := range a.projectFilterOptions {
		options[i] = opt.Name
	}
	a.dialog = common.NewSelectDialog("board-project-filter", "Project Filter", "Select project:", options)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	return nil
}

func (a *App) handleShowCreateIssueDialog() tea.Cmd {
	if a.dialog != nil && a.dialog.Visible() {
		return nil
	}
	if len(a.createIssueTeams) == 0 {
		return a.toast.ShowInfo("No teams available")
	}
	a.dialog = common.NewInputDialog("create-issue", "Create Issue", "Issue title...")
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	return nil
}

func (a *App) handleShowCommentDialog() tea.Cmd {
	if a.dialog != nil && a.dialog.Visible() {
		return nil
	}
	if a.selectedIssue == nil {
		return a.toast.ShowInfo("No issue selected")
	}
	a.commentIssueID = a.selectedIssue.ID
	a.dialog = common.NewInputDialog("comment", "Add Comment", "Comment...")
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	return nil
}

func (a *App) handleShowDiffCommentDialog(msg messages.ShowDiffCommentDialog) tea.Cmd {
	if a.dialog != nil && a.dialog.Visible() {
		return nil
	}
	a.diffCommentFile = msg.File
	a.diffCommentSide = msg.Side
	a.diffCommentLine = msg.Line
	a.dialog = common.NewInputDialog("diff-comment", "Diff Comment", "Comment...")
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	return nil
}

func (a *App) handleShowAttemptsDialog(issueID string) tea.Cmd {
	if a.dialog != nil && a.dialog.Visible() {
		return nil
	}
	attempts := a.buildAttempts(issueID)
	if len(attempts) == 0 {
		return a.toast.ShowInfo("No attempts found")
	}
	options := make([]string, len(attempts))
	branches := make([]string, len(attempts))
	for i, att := range attempts {
		options[i] = fmt.Sprintf("%s (%s) %s", att.Branch, att.Executor, att.Status)
		branches[i] = att.Branch
	}
	a.attemptPickerIssueID = issueID
	a.attemptPickerBranches = branches
	a.dialog = common.NewSelectDialog("attempt-picker", "Select Attempt", "Choose attempt:", options)
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	return nil
}

func (a *App) handleShowAttemptPicker() tea.Cmd {
	if a.selectedIssue == nil {
		return a.toast.ShowInfo("No issue selected")
	}
	return a.handleShowAttemptsDialog(a.selectedIssue.ID)
}

func (a *App) handleShowDrawerPane(pane string) {
	a.drawerOpen = true
	if a.drawer != nil {
		switch pane {
		case "approvals":
			a.drawer.SetPane(drawer.PaneApprovals)
		case "processes":
			a.drawer.SetPane(drawer.PaneProcesses)
		default:
			a.drawer.SetPane(drawer.PaneLogs)
		}
	}
}

func (a *App) handleOpenWorkspaceInEditor(issueID string) tea.Cmd {
	wt := a.findWorktreeByIssue(issueID)
	if wt == nil {
		return a.toast.ShowError("No worktree found for issue")
	}
	return a.openFileInEditor(wt.Root)
}

func (a *App) handleCopyPreviewURL() tea.Cmd {
	if a.previewView == nil || a.previewView.URL == "" {
		return a.toast.ShowInfo("No preview URL")
	}
	return a.copyToClipboard(a.previewView.URL)
}

func (a *App) handleEditPreviewURL() tea.Cmd {
	if a.dialog != nil && a.dialog.Visible() {
		return nil
	}
	a.dialog = common.NewInputDialog("edit-preview-url", "Preview URL", "Enter URL...")
	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	return nil
}

func (a *App) handleStopPreviewServer() tea.Cmd {
	if a.selectedIssue == nil {
		return nil
	}
	wt := a.findWorktreeByIssue(a.selectedIssue.ID)
	if wt == nil {
		return nil
	}
	if a.scripts != nil {
		_ = a.scripts.Stop(wt)
	}
	a.previewView.SetRunning(false)
	a.previewView.URL = ""
	if a.drawer != nil {
		a.drawer.SetDevURL("")
	}
	return nil
}
