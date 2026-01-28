package app

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func (a *App) editIssue(issueID string) tea.Cmd {
	issue := a.findIssue(issueID)
	if issue == nil {
		return a.toast.ShowError("Issue not found")
	}
	if a.issueAuthRequired(issue) {
		return a.toast.ShowError("Auth required for this account")
	}
	a.editIssueID = issueID
	a.editIssueTitle = issue.Title
	a.editIssueDescription = issue.Description
	a.dialog = common.NewInputDialog("edit-issue-title", "Edit Issue", "Title (leave blank to keep)")

	a.dialog.SetShowKeymapHints(a.config.UI.ShowKeymapHints)
	a.dialog.Show()
	return nil
}

func (a *App) updateIssue(issueID, title, description string) tea.Cmd {
	if issueID == "" {
		return a.toast.ShowError("Issue not found")
	}
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
			return messages.Error{Err: errNoLinearAccount, Context: "edit issue"}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := a.linearService.UpdateIssue(ctx, acct, issueID, title, description); err != nil {
			return messages.Error{Err: err, Context: "edit issue"}
		}
		return messages.RefreshBoard{}
	}
}

var errNoLinearAccount = errors.New("no Linear account")
