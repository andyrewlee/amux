package app

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/tmux"
)

const orphanSessionGracePeriod = 30 * time.Second

// orphanGCResult is returned after attempting to clean up orphaned tmux sessions.
type orphanGCResult struct {
	Killed int
	Err    error
}

// terminalGCResult is returned after attempting to clean up stale terminal sessions.
// This is now a no-op since sessions are always persisted.
type terminalGCResult struct {
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
	for id := range a.creatingWorkspaceIDs {
		ids[id] = true
	}
	return ids
}

// gcOrphanedTmuxSessions returns a Cmd that finds and kills tmux sessions
// belonging to workspaces that no longer exist.
func (a *App) gcOrphanedTmuxSessions() tea.Cmd {
	if !a.tmuxAvailable || !a.projectsLoaded {
		return nil
	}
	knownIDs := a.collectKnownWorkspaceIDs()
	opts := a.tmuxOptions
	svc := a.tmuxService
	return func() tea.Msg {
		if svc == nil {
			return orphanGCResult{Err: errTmuxUnavailable}
		}
		byWorkspace, err := a.amuxSessionsByWorkspace(opts)
		if err != nil {
			return orphanGCResult{Err: err}
		}
		now := time.Now()
		killed := 0
		for wsID, sessions := range byWorkspace {
			if knownIDs[wsID] {
				continue
			}
			for _, ws := range sessions {
				createdAt := ws.CreatedAt
				if createdAt == 0 {
					if ts, err := svc.SessionCreatedAt(ws.Name, opts); err == nil {
						createdAt = ts
					}
				}
				if isRecentOrphanSession(createdAt, now) {
					continue
				}
				hasClients, err := svc.SessionHasClients(ws.Name, opts)
				if err != nil {
					logging.Warn("orphan GC: failed to check clients for %s: %v", ws.Name, err)
				}
				if hasClients {
					continue
				}
				if err := svc.KillSession(ws.Name, opts); err != nil {
					logging.Warn("orphan GC: failed to kill session %s: %v", ws.Name, err)
				} else {
					killed++
				}
			}
		}
		return orphanGCResult{Killed: killed}
	}
}

// gcStaleTerminalSessions is a no-op since sessions are always persisted.
func (a *App) gcStaleTerminalSessions() tea.Cmd {
	return nil
}

type workspaceSession struct {
	Name      string
	CreatedAt int64
}

func (a *App) amuxSessionsByWorkspace(opts tmux.Options) (map[string][]workspaceSession, error) {
	if a.tmuxService == nil {
		return nil, errTmuxUnavailable
	}
	match := map[string]string{"@amux": "1"}
	if a.instanceID != "" {
		match["@amux_instance"] = a.instanceID
	}
	rows, err := a.tmuxService.SessionsWithTags(match, []string{"@amux_workspace", "@amux_created_at"}, opts)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]workspaceSession)
	for _, row := range rows {
		wsID := strings.TrimSpace(row.Tags["@amux_workspace"])
		if wsID == "" {
			continue
		}
		var createdAt int64
		if raw := strings.TrimSpace(row.Tags["@amux_created_at"]); raw != "" {
			createdAt, _ = strconv.ParseInt(raw, 10, 64)
		}
		out[wsID] = append(out[wsID], workspaceSession{Name: row.Name, CreatedAt: createdAt})
	}
	return out, nil
}

func isRecentOrphanSession(createdAt int64, now time.Time) bool {
	if createdAt <= 0 {
		return false
	}
	created := time.Unix(createdAt, 0)
	if created.After(now) {
		return true // clock skew protection
	}
	return now.Sub(created) < orphanSessionGracePeriod
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

func (a *App) handleTerminalGCResult(msg terminalGCResult) {
	if msg.Err != nil {
		logging.Warn("terminal GC: %v", msg.Err)
		return
	}
	if msg.Killed > 0 {
		logging.Info("terminal GC: killed %d stale terminal session(s)", msg.Killed)
	}
}
