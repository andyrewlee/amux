package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/tmux"
)

// orphanGCResult is returned after attempting to clean up orphaned tmux sessions.
type orphanGCResult struct {
	Killed int
	Err    error
}

// collectKnownWorkspaceIDs returns the set of workspace IDs currently tracked
// by the app. Must be called on the Update goroutine.
func (a *App) collectKnownWorkspaceIDs() map[string]bool {
	ids := make(map[string]bool)
	for i := range a.projects {
		for j := range a.projects[i].Workspaces {
			ids[string(a.projects[i].Workspaces[j].ID())] = true
		}
	}
	return ids
}

// gcOrphanedTmuxSessions returns a Cmd that finds and kills tmux sessions
// belonging to workspaces that no longer exist.
func (a *App) gcOrphanedTmuxSessions() tea.Cmd {
	if !a.tmuxAvailable {
		return nil
	}
	knownIDs := a.collectKnownWorkspaceIDs()
	opts := a.tmuxOptions
	return func() tea.Msg {
		byWorkspace, err := tmux.AmuxSessionsByWorkspace(opts)
		if err != nil {
			return orphanGCResult{Err: err}
		}
		killed := 0
		for wsID, sessions := range byWorkspace {
			if knownIDs[wsID] {
				continue
			}
			for _, name := range sessions {
				if err := tmux.KillSession(name, opts); err != nil {
					logging.Warn("orphan GC: failed to kill session %s: %v", name, err)
				} else {
					killed++
				}
			}
		}
		return orphanGCResult{Killed: killed}
	}
}

// handleOrphanGCResult logs the outcome of an orphan GC pass.
func (a *App) handleOrphanGCResult(msg orphanGCResult) {
	if msg.Err != nil {
		logging.Warn("orphan GC: %v", msg.Err)
		return
	}
	if msg.Killed > 0 {
		logging.Info("orphan GC: killed %d orphaned tmux session(s)", msg.Killed)
	}
}
