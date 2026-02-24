package app

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/logging"
	"github.com/andyrewlee/medusa/internal/messages"
	"github.com/andyrewlee/medusa/internal/tmux"
)

func (a *App) cleanupWorkspaceTmuxSessions(ws *data.Workspace) tea.Cmd {
	if ws == nil {
		return nil
	}
	wsID := string(ws.ID())
	opts := a.tmuxOptions
	return func() tea.Msg {
		tags := map[string]string{
			"@medusa":           "1",
			"@medusa_workspace": wsID,
		}
		cleaned, err := tmux.KillSessionsMatchingTags(tags, opts)
		if err != nil {
			logging.Warn("Failed to cleanup tmux sessions for workspace %s: %v", ws.Name, err)
		}
		if cleaned {
			logging.Info("Cleaned up @medusa tmux sessions for workspace %s", ws.Name)
		}
		return nil
	}
}

func (a *App) cleanupAllTmuxSessions() tea.Cmd {
	opts := a.tmuxOptions
	return func() tea.Msg {
		cleanedTagged, err := tmux.KillSessionsMatchingTags(map[string]string{"@medusa": "1"}, opts)
		if err != nil {
			logging.Warn("Failed to cleanup tmux sessions by tag: %v", err)
		} else if cleanedTagged {
			logging.Info("Cleaned up @medusa tmux sessions")
		}
		prefix := tmux.SessionName("medusa") + "-"
		if err := tmux.KillSessionsWithPrefix(prefix, opts); err != nil {
			return messages.Toast{Message: fmt.Sprintf("tmux cleanup failed: %v", err), Level: messages.ToastWarning}
		}
		if cleanedTagged {
			return messages.Toast{Message: fmt.Sprintf("Cleaned up @medusa and %s* tmux sessions", prefix), Level: messages.ToastSuccess}
		}
		return messages.Toast{Message: fmt.Sprintf("Cleaned up %s* tmux sessions", prefix), Level: messages.ToastSuccess}
	}
}

// CleanupTmuxOnExit kills medusa sessions on exit when persistence is disabled.
func (a *App) CleanupTmuxOnExit() {
	if a == nil || a.config == nil {
		return
	}
	if a.config.UI.TmuxPersistence {
		return
	}
	opts := a.tmuxOptions
	opts.CommandTimeout = 2 * time.Second
	if cleaned, err := tmux.KillSessionsMatchingTags(map[string]string{"@medusa": "1"}, opts); err != nil {
		logging.Warn("Failed to cleanup tmux sessions on exit by tag: %v", err)
	} else if cleaned {
		logging.Info("Cleaned up @medusa tmux sessions on exit")
	}
	prefix := tmux.SessionName("medusa") + "-"
	if err := tmux.KillSessionsWithPrefix(prefix, opts); err != nil {
		logging.Warn("Failed to cleanup tmux sessions on exit: %v", err)
		return
	}
	logging.Info("Cleaned up %s* tmux sessions on exit", prefix)
}
