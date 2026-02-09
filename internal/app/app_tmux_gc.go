package app

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/tmux"
)

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

const orphanSessionGracePeriod = 30 * time.Second

type workspaceSession struct {
	Name      string
	CreatedAt int64
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
	instanceID := a.instanceID
	return func() tea.Msg {
		if svc == nil {
			return orphanGCResult{Err: errTmuxUnavailable}
		}
		byWorkspace, err := a.amuxSessionsByWorkspace(svc, opts, instanceID)
		if err != nil {
			return orphanGCResult{Err: err}
		}
		killed := 0
		now := time.Now()
		for wsID, sessions := range byWorkspace {
			if knownIDs[wsID] {
				continue
			}
			for _, session := range sessions {
				name := session.Name
				if name == "" {
					continue
				}
				createdAt := session.CreatedAt
				if createdAt == 0 {
					if fallback, err := svc.SessionCreatedAt(name, opts); err == nil {
						createdAt = fallback
					}
				}
				if isRecentOrphanSession(createdAt, now) {
					continue
				}
				hasClients, err := svc.SessionHasClients(name, opts)
				if err != nil {
					logging.Warn("orphan GC: failed to check clients for session %s: %v", name, err)
					continue
				}
				if hasClients {
					continue
				}
				if err := svc.KillSession(name, opts); err != nil {
					logging.Warn("orphan GC: failed to kill session %s: %v", name, err)
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

func (a *App) amuxSessionsByWorkspace(svc *tmuxService, opts tmux.Options, instanceID string) (map[string][]workspaceSession, error) {
	if svc == nil {
		return nil, errTmuxUnavailable
	}
	match := map[string]string{"@amux": "1"}
	if strings.TrimSpace(instanceID) != "" {
		match["@amux_instance"] = strings.TrimSpace(instanceID)
	}
	rows, err := svc.SessionsWithTags(match, []string{"@amux_workspace", "@amux_created_at"}, opts)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]workspaceSession)
	for _, row := range rows {
		wsID := row.Tags["@amux_workspace"]
		if wsID == "" {
			continue
		}
		var createdAt int64
		if raw := strings.TrimSpace(row.Tags["@amux_created_at"]); raw != "" {
			createdAt, _ = strconv.ParseInt(raw, 10, 64)
		}
		out[wsID] = append(out[wsID], workspaceSession{
			Name:      row.Name,
			CreatedAt: createdAt,
		})
	}
	return out, nil
}

func isRecentOrphanSession(createdAt int64, now time.Time) bool {
	if createdAt <= 0 {
		return false
	}
	created := time.Unix(createdAt, 0)
	if created.After(now) {
		return true
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
