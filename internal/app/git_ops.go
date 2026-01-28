package app

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/attempt"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func (a *App) pushBranch(issueID string) tea.Cmd {
	wt := a.findWorktreeByIssue(issueID)
	if wt == nil {
		return a.toast.ShowError("No worktree found for issue")
	}
	if a.center != nil && a.center.HasRunningIssue(issueID) {
		return a.toast.ShowInfo("Agent is running; stop it before pushing")
	}
	if a.issueHasConflicts(issueID) {
		return a.toast.ShowInfo("Resolve conflicts before pushing")
	}
	a.logActivityEntry(common.ActivityEntry{
		Kind:      common.ActivityCommand,
		Summary:   fmt.Sprintf("git push origin %s", wt.Branch),
		Status:    common.StatusRunning,
		ProcessID: string(wt.ID()),
	})
	return func() tea.Msg {
		_, err := git.RunGit(wt.Root, "push", "-u", "origin", wt.Branch)
		if err != nil {
			return messages.Error{Err: err, Context: "git push"}
		}
		return messages.LogActivity{Line: "Push complete", Kind: "command", Status: "success", ProcessID: string(wt.ID())}
	}
}

func (a *App) mergePullRequest(issueID string) tea.Cmd {
	wt := a.findWorktreeByIssue(issueID)
	if wt == nil {
		return a.toast.ShowError("No worktree found for issue")
	}
	if a.center != nil && a.center.HasRunningIssue(issueID) {
		return a.toast.ShowInfo("Agent is running; stop it before merging")
	}
	if a.issueHasConflicts(issueID) {
		return a.toast.ShowInfo("Resolve conflicts before merging")
	}
	meta, _ := attempt.Load(wt.Root)
	if meta == nil || meta.PRURL == "" {
		return a.toast.ShowError("No PR linked")
	}
	prURL := meta.PRURL
	a.logActivityEntry(common.ActivityEntry{
		Kind:      common.ActivityCommand,
		Summary:   "gh pr merge " + prURL,
		Status:    common.StatusRunning,
		ProcessID: string(wt.ID()),
	})
	return func() tea.Msg {
		cmd := exec.Command("gh", "pr", "merge", prURL, "--merge")
		cmd.Dir = wt.Root
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			return messages.Error{Err: fmt.Errorf("gh pr merge failed: %s", out.String()), Context: "merge PR"}
		}
		return messages.LogActivity{Line: "PR merged", Kind: "command", Status: "success", ProcessID: string(wt.ID())}
	}
}

func (a *App) showChangeBaseDialog(issueID string) tea.Cmd {
	if issueID == "" {
		return nil
	}
	a.changeBaseIssueID = issueID
	a.dialog = common.NewInputDialog("change-base", "Change Base Branch", "Base branch (e.g. main)")

	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	return nil
}

func (a *App) changeBaseBranch(issueID, base string) tea.Cmd {
	base = strings.TrimSpace(base)
	if base == "" {
		return a.toast.ShowInfo("Base branch required")
	}
	wt := a.findWorktreeByIssue(issueID)
	if wt == nil {
		return a.toast.ShowError("No worktree found for issue")
	}
	return func() tea.Msg {
		// Load existing workspace metadata and update base
		_, _ = a.workspaces.LoadMetadataFor(wt)
		wt.Base = base
		_ = a.workspaces.Save(wt)
		if attemptMeta, _ := attempt.Load(wt.Root); attemptMeta != nil {
			attemptMeta.BaseRef = base
			attemptMeta.LastSyncedAt = time.Now()
			_ = attempt.Save(wt.Root, attemptMeta)
		}
		return messages.RefreshBoard{}
	}
}
