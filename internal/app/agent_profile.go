package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/attempt"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func (a *App) updateAgentProfile(issueID, assistant string) tea.Cmd {
	if issueID == "" || assistant == "" {
		return nil
	}
	wt := a.findWorktreeByIssue(issueID)
	if wt == nil {
		return a.toast.ShowInfo("No worktree found for issue")
	}
	return func() tea.Msg {
		if meta, _ := attempt.Load(wt.Root); meta != nil {
			meta.AgentProfile = assistant
			meta.LastSyncedAt = time.Now()
			_ = attempt.Save(wt.Root, meta)
		}
		// Load existing workspace metadata and update assistant
		_, _ = a.workspaces.LoadMetadataFor(wt)
		wt.Assistant = assistant
		_ = a.workspaces.Save(wt)
		if a.selectedIssue != nil && a.selectedIssue.ID == issueID {
			a.inspector.SetAgentProfile(assistant)
		}
		a.logActivityEntry(common.ActivityEntry{
			Kind:      common.ActivityInfo,
			Summary:   "Agent profile set: " + assistant,
			Status:    common.StatusSuccess,
			ProcessID: string(wt.ID()),
		})
		return nil
	}
}
