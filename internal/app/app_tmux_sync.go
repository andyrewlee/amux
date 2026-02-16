package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
)

// handleTmuxSyncTick reconciles tmux state for the active workspace on each tick.
//
// Cost model: tmuxSyncWorkspaces() currently returns only the active workspace.
// Each tick performs:
// 1) discovery for new sessions created outside this UI instance, and
// 2) status sync for known tabs.
// The default 7s tick interval and per-command 5s timeout bound worst-case latency.
func (a *App) handleTmuxSyncTick(msg messages.TmuxSyncTick) []tea.Cmd {
	if msg.Token != a.tmuxSyncToken {
		return nil
	}
	var cmds []tea.Cmd
	if a.tmuxAvailable {
		for _, ws := range a.tmuxSyncWorkspaces() {
			if discoverCmd := a.discoverWorkspaceTabsFromTmux(ws); discoverCmd != nil {
				cmds = append(cmds, discoverCmd)
			}
			if syncCmd := a.syncWorkspaceTabsFromTmux(ws); syncCmd != nil {
				cmds = append(cmds, syncCmd)
			}
		}
		if gcCmd := a.gcOrphanedTmuxSessions(); gcCmd != nil {
			cmds = append(cmds, gcCmd)
		}
		if a.lastTerminalGCRun.IsZero() || time.Since(a.lastTerminalGCRun) > time.Hour {
			if gcCmd := a.gcStaleTerminalSessions(); gcCmd != nil {
				cmds = append(cmds, gcCmd)
				a.lastTerminalGCRun = time.Now()
			}
		}
	}
	cmds = append(cmds, a.startTmuxSyncTicker())
	return cmds
}

func (a *App) handleTmuxTabsSyncResult(msg tmuxTabsSyncResult) []tea.Cmd {
	if msg.WorkspaceID == "" {
		return nil
	}
	ws := a.findWorkspaceByID(msg.WorkspaceID)
	if ws == nil || len(msg.Updates) == 0 {
		return nil
	}
	changed := false
	var cmds []tea.Cmd
	for _, update := range msg.Updates {
		if update.SessionName == "" {
			continue
		}
		for i := range ws.OpenTabs {
			tab := &ws.OpenTabs[i]
			if tab.SessionName != update.SessionName {
				continue
			}
			if tab.Status != update.Status {
				tab.Status = update.Status
				changed = true
				if update.NotifyStopped && update.Status == "stopped" {
					sessionName := update.SessionName
					wsID := msg.WorkspaceID
					cmds = append(cmds, func() tea.Msg {
						return messages.TabSessionStatus{
							WorkspaceID: wsID,
							SessionName: sessionName,
							Status:      "stopped",
						}
					})
				}
			}
			break
		}
	}
	if changed {
		wsSnapshot := snapshotWorkspaceForSave(ws)
		cmds = append(cmds, func() tea.Msg {
			if err := a.workspaceService.Save(wsSnapshot); err != nil {
				logging.Warn("Failed to sync workspace tabs: %v", err)
			} else {
				a.markLocalWorkspaceSaveForID(string(wsSnapshot.ID()))
			}
			return nil
		})
	}
	return cmds
}
