package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/attempt"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func (a *App) renameBranch(issueID string) tea.Cmd {
	wt := a.findWorktreeByIssue(issueID)
	if wt == nil {
		return a.toast.ShowError("No worktree found for issue")
	}
	a.renameBranchIssueID = issueID
	a.dialog = common.NewInputDialog("rename-branch", "Rename Branch", "New branch name")

	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	return nil
}

func (a *App) applyRenameBranch(issueID, newBranch string) tea.Cmd {
	newBranch = strings.TrimSpace(newBranch)
	if newBranch == "" {
		return a.toast.ShowInfo("Branch name required")
	}
	wt := a.findWorktreeByIssue(issueID)
	if wt == nil {
		return a.toast.ShowError("No worktree found for issue")
	}
	old := wt.Branch
	return func() tea.Msg {
		if _, err := git.RunGit(wt.Root, "branch", "-m", old, newBranch); err != nil {
			return messages.Error{Err: err, Context: "rename branch"}
		}
		wt.Branch = newBranch
		_ = a.workspaces.Save(wt)
		if attemptMeta, _ := attempt.Load(wt.Root); attemptMeta != nil {
			attemptMeta.BranchName = newBranch
			_ = attempt.Save(wt.Root, attemptMeta)
		}
		return messages.RefreshBoard{}
	}
}
