package app

import (
	tea "charm.land/bubbletea/v2"
)

func (a *App) cancelQueuedMessage(issueID string) tea.Cmd {
	wt := a.findWorktreeByIssue(issueID)
	if wt == nil {
		return a.toast.ShowInfo("No queued message")
	}
	delete(a.pendingAgentMessages, wt.Root)
	if a.selectedIssue != nil && a.selectedIssue.ID == issueID {
		a.inspector.SetQueuedMessage("")
	}
	return a.toast.ShowInfo("Queued message cleared")
}
