package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func (a *App) handleGitStatusResult(msg messages.GitStatusResult) tea.Cmd {
	newDashboard, cmd := a.dashboard.Update(msg)
	a.dashboard = newDashboard
	if a.activeWorkspace != nil && rootsReferToSameWorkspace(msg.Root, a.activeWorkspace.Root) {
		a.sidebar.SetGitStatus(msg.Status)
	}
	return cmd
}

// handleGitStatusBatchResult applies a reload's batched status results: the
// dashboard cache absorbs every entry, the sidebar picks up the active
// workspace's result — all inside one message → one render pass. Each result
// also clears its root's dedup mark and drains any coalesced follow-up.
func (a *App) handleGitStatusBatchResult(msg messages.GitStatusBatchResult) tea.Cmd {
	var cmds []tea.Cmd
	for _, result := range msg.Results {
		newDashboard, cmd := a.dashboard.Update(messages.GitStatusResult{
			Root:   result.Root,
			Status: result.Status,
			Err:    result.Err,
		})
		a.dashboard = newDashboard
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		if a.activeWorkspace != nil && rootsReferToSameWorkspace(result.Root, a.activeWorkspace.Root) {
			a.sidebar.SetGitStatus(result.Status)
		}
		if cmd := a.gitStatusFollowUp(result.Root); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return common.SafeBatch(cmds...)
}
