package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
)

// handleTmuxSyncTick syncs tab status for all workspaces on each tick.
//
// Cost model: tmuxSyncWorkspaces() returns every workspace (in monitor mode, all of
// them). Each workspace spawns a separate Bubble Tea Cmd goroutine, so syncs run
// concurrently. Per workspace, each tab with a session name incurs 1-2 tmux commands
// (has-session + list-panes). The default 7s tick interval and per-command 5s timeout
// bound worst-case latency. For typical usage (a handful of projects, a few tabs each)
// the overhead is negligible. If the number of workspaces grows large, a per-tick cap
// or adaptive interval can be added here.
func (a *App) handleTmuxSyncTick(msg messages.TmuxSyncTick) []tea.Cmd {
	if msg.Token != a.tmuxSyncToken {
		return nil
	}
	var cmds []tea.Cmd
	if a.tmuxAvailable {
		for _, ws := range a.tmuxSyncWorkspaces() {
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
			if err := a.workspaces.Save(wsSnapshot); err != nil {
				logging.Warn("Failed to sync workspace tabs: %v", err)
			}
			return nil
		})
	}
	return cmds
}
