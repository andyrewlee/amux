package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/tmux"
)

var setTmuxSessionTagValue = tmux.SetSessionTagValue

type workspaceTmuxRebindResultMsg struct {
	oldID   string
	newID   string
	success bool
}

func (a *App) rebindWorkspaceTmuxSessions(oldID, newID string) tea.Cmd {
	oldID = strings.TrimSpace(oldID)
	newID = strings.TrimSpace(newID)
	if oldID == "" || newID == "" || oldID == newID {
		return nil
	}
	if !a.tmuxAvailable || a.tmuxService == nil {
		a.queueWorkspaceTmuxRebind(oldID, newID)
		return nil
	}
	return a.workspaceTmuxRebindCmd(oldID, newID)
}

func (a *App) workspaceTmuxRebindCmd(oldID, newID string) tea.Cmd {
	oldID = strings.TrimSpace(oldID)
	newID = strings.TrimSpace(newID)
	instanceID := strings.TrimSpace(a.instanceID)
	if oldID == "" || newID == "" || oldID == newID || a.tmuxService == nil {
		return nil
	}

	svc := a.tmuxService
	opts := a.tmuxOptions
	return func() tea.Msg {
		rows, err := svc.SessionsWithTags(map[string]string{
			"@amux":           "1",
			"@amux_workspace": oldID,
		}, []string{"@amux_instance"}, opts)
		if err != nil {
			logging.Warn("tmux workspace rebind discovery failed for %s -> %s: %v", oldID, newID, err)
			return workspaceTmuxRebindResultMsg{oldID: oldID, newID: newID}
		}
		success := true
		for _, row := range rows {
			sessionName := strings.TrimSpace(row.Name)
			if sessionName == "" {
				continue
			}
			rowInstanceID := strings.TrimSpace(row.Tags["@amux_instance"])
			if rowInstanceID == "" || rowInstanceID != instanceID {
				hasClients, err := svc.SessionHasClients(sessionName, opts)
				if err != nil {
					logging.Warn("tmux workspace rebind client check failed for %s (%s -> %s): %v", sessionName, oldID, newID, err)
					success = false
					continue
				}
				if hasClients {
					success = false
					continue
				}
			}
			if err := setTmuxSessionTagValue(sessionName, "@amux_workspace", newID, opts); err != nil {
				logging.Warn("tmux workspace rebind retag failed for %s (%s -> %s): %v", sessionName, oldID, newID, err)
				success = false
			}
		}
		return workspaceTmuxRebindResultMsg{oldID: oldID, newID: newID, success: success}
	}
}

func (a *App) queueWorkspaceTmuxRebind(oldID, newID string) {
	oldID = strings.TrimSpace(oldID)
	newID = strings.TrimSpace(newID)
	if oldID == "" || newID == "" || oldID == newID {
		return
	}
	if a.pendingTmuxRebinds == nil {
		a.pendingTmuxRebinds = make(map[string]string)
	}
	for from, to := range a.pendingTmuxRebinds {
		if to == oldID {
			a.pendingTmuxRebinds[from] = newID
		}
	}
	a.pendingTmuxRebinds[oldID] = newID
	for from, to := range a.pendingTmuxRebinds {
		if from == to {
			delete(a.pendingTmuxRebinds, from)
		}
	}
}

func (a *App) drainPendingWorkspaceTmuxRebinds() []tea.Cmd {
	if !a.tmuxAvailable || a.tmuxService == nil || len(a.pendingTmuxRebinds) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(a.pendingTmuxRebinds))
	for oldID, newID := range a.pendingTmuxRebinds {
		if cmd := a.workspaceTmuxRebindCmd(oldID, newID); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return cmds
}

func (a *App) handleWorkspaceTmuxRebindResult(msg workspaceTmuxRebindResultMsg) {
	oldID := strings.TrimSpace(msg.oldID)
	newID := strings.TrimSpace(msg.newID)
	if oldID == "" || newID == "" {
		return
	}
	if !msg.success {
		a.queueWorkspaceTmuxRebind(oldID, newID)
		return
	}
	if strings.TrimSpace(a.pendingTmuxRebinds[oldID]) == newID {
		delete(a.pendingTmuxRebinds, oldID)
	}
}
