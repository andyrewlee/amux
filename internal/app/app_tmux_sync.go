package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
)

func (a *App) handleTmuxSyncTick(msg messages.TmuxSyncTick) []tea.Cmd {
	if msg.Token != a.tmuxSyncToken {
		return nil
	}
	if !a.tmuxAvailable {
		return nil
	}
	var cmds []tea.Cmd
	for _, ws := range a.tmuxSyncWorkspaces() {
		if syncCmd := a.syncWorkspaceTabsFromTmux(ws); syncCmd != nil {
			cmds = append(cmds, syncCmd)
		}
	}
	cmds = append(cmds, a.startTmuxSyncTicker())
	return cmds
}

func (a *App) handleTmuxTabsSyncResult(msg tmuxTabsSyncResult) []tea.Cmd {
	if msg.WorkspaceID == "" || len(msg.Updates) == 0 {
		return nil
	}
	ws := a.findWorkspaceByID(msg.WorkspaceID)
	if ws == nil {
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
			if err := a.workspaces.Save(wsSnapshot); err != nil {
				logging.Warn("Failed to sync workspace tabs: %v", err)
			}
			return nil
		})
	}
	return cmds
}
