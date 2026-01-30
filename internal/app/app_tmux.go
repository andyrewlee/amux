package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/tmux"
)

func (a *App) cleanupWorkspaceTmuxSessions(ws *data.Workspace) tea.Cmd {
	if ws == nil {
		return nil
	}
	wsID := string(ws.ID())
	opts := a.tmuxOptions
	return func() tea.Msg {
		if err := tmux.KillWorkspaceSessions(wsID, opts); err != nil {
			logging.Warn("Failed to cleanup tmux sessions for workspace %s: %v", ws.Name, err)
		}
		return nil
	}
}

func (a *App) cleanupAllTmuxSessions() tea.Cmd {
	opts := a.tmuxOptions
	return func() tea.Msg {
		if err := tmux.KillSessionsWithPrefix(tmux.SessionName("amux"), opts); err != nil {
			return messages.Toast{Message: fmt.Sprintf("tmux cleanup failed: %v", err), Level: messages.ToastWarning}
		}
		return messages.Toast{Message: "Cleaned up amux tmux sessions", Level: messages.ToastSuccess}
	}
}
