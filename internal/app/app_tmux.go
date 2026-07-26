package app

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/tmux"
)

// killWorkspaceSessionsSync synchronously tears down every tmux session for a
// workspace, regardless of which amux instance created it. Worktree deletion is
// host-global: once the directory is gone, a session owned by another instance
// is just as invalid as one owned by this process. The delete path calls this
// only after worktree removal succeeds, so a failed delete does not destroy live
// agent sessions. No-op when tmux is unavailable.
func (a *App) killWorkspaceSessionsSync(wsID string) error {
	if a.tmuxService == nil || wsID == "" {
		return nil
	}
	tags := map[string]string{
		"@amux":           "1",
		"@amux_workspace": wsID,
	}
	_, taggedErr := a.tmuxService.KillSessionsMatchingTags(tags, a.tmuxOptions)
	if taggedErr != nil {
		logging.Warn("Failed to kill tagged tmux sessions for deleted workspace %s: %v", wsID, taggedErr)
	}
	legacyErr := a.tmuxService.KillWorkspaceSessions(wsID, a.tmuxOptions)
	if legacyErr != nil {
		logging.Warn("Failed to kill legacy tmux sessions for deleted workspace %s: %v", wsID, legacyErr)
	}
	return errors.Join(taggedErr, legacyErr)
}

func (a *App) killWorkspaceSessionNamesSync(sessionNames []string) error {
	if a.tmuxService == nil {
		return nil
	}
	prefix := tmux.SessionName("amux") + "-"
	var errs []error
	for _, name := range sessionNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !strings.HasPrefix(name, prefix) {
			logging.Warn("Skipping non-amux persisted tmux session during workspace delete: %s", name)
			continue
		}
		if err := a.tmuxService.KillSession(name, a.tmuxOptions); err != nil {
			logging.Warn("Failed to kill persisted tmux session %s for deleted workspace: %v", name, err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
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
