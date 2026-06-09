package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/tmux"
)

// killWorkspaceSessionsSync synchronously tears down a workspace's tmux sessions
// by tag. The delete path calls this only after worktree removal succeeds, so a
// failed delete does not destroy live agent sessions. No-op when tmux is
// unavailable.
func (a *App) killWorkspaceSessionsSync(wsID string) {
	if a.tmuxService == nil || wsID == "" {
		return
	}
	tags := map[string]string{
		"@amux":           "1",
		"@amux_workspace": wsID,
	}
	if _, err := a.tmuxService.KillSessionsMatchingTags(tags, a.tmuxOptions); err != nil {
		logging.Warn("Failed to kill tmux sessions for workspace %s before worktree removal: %v", wsID, err)
	}
	if err := a.tmuxService.KillWorkspaceSessions(wsID, a.tmuxOptions); err != nil {
		logging.Warn("Failed to kill tmux sessions for workspace %s before worktree removal: %v", wsID, err)
	}
}

func (a *App) cleanupAllTmuxSessions() tea.Cmd {
	opts := a.tmuxOptions
	svc := a.tmuxService
	return func() tea.Msg {
		if svc == nil {
			return messages.Toast{Message: "tmux cleanup unavailable", Level: messages.ToastWarning}
		}
		cleanedTagged, err := svc.KillSessionsMatchingTags(map[string]string{"@amux": "1"}, opts)
		if err != nil {
			logging.Warn("Failed to cleanup tmux sessions by tag: %v", err)
		} else if cleanedTagged {
			logging.Info("Cleaned up @amux tmux sessions")
		}
		prefix := tmux.SessionName("amux") + "-"
		if err := svc.KillSessionsWithPrefix(prefix, opts); err != nil {
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
