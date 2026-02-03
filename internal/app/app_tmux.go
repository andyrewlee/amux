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
		tags := map[string]string{
			"@amux":           "1",
			"@amux_workspace": wsID,
		}
		cleaned, err := tmux.KillSessionsMatchingTags(tags, opts)
		if err != nil {
			logging.Warn("Failed to cleanup tmux sessions for workspace %s: %v", ws.Name, err)
		}
		if cleaned {
			logging.Info("Cleaned up @amux tmux sessions for workspace %s", ws.Name)
		}
		if err := tmux.KillWorkspaceSessions(wsID, opts); err != nil {
			logging.Warn("Failed to cleanup tmux sessions for workspace %s: %v", ws.Name, err)
		}
		return nil
	}
}

func (a *App) cleanupAllTmuxSessions() tea.Cmd {
	opts := a.tmuxOptions
	return func() tea.Msg {
		cleanedTagged, err := tmux.KillSessionsMatchingTags(map[string]string{"@amux": "1"}, opts)
		if err != nil {
			logging.Warn("Failed to cleanup tmux sessions by tag: %v", err)
		} else if cleanedTagged {
			logging.Info("Cleaned up @amux tmux sessions")
		}
		prefix := tmux.SessionName("amux") + "-"
		if err := tmux.KillSessionsWithPrefix(prefix, opts); err != nil {
			return messages.Toast{Message: fmt.Sprintf("tmux cleanup failed: %v", err), Level: messages.ToastWarning}
		}
		if cleanedTagged {
			return messages.Toast{Message: fmt.Sprintf("Cleaned up @amux and %s* tmux sessions", prefix), Level: messages.ToastSuccess}
		}
		return messages.Toast{Message: fmt.Sprintf("Cleaned up %s* tmux sessions", prefix), Level: messages.ToastSuccess}
	}
}

// CleanupTmuxOnExit is a no-op since sessions are always persisted across restarts.
func (a *App) CleanupTmuxOnExit() {
}
