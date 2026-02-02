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

type terminalGCResult struct {
	Killed int
	Err    error
}

type terminalSessionInfo struct {
	Name       string
	Workspace  string
	CreatedAt  int64
	HasClients bool
	InstanceID string
}

const terminalSessionTTL = 24 * time.Hour

func selectStaleTerminalSessions(sessions []terminalSessionInfo, now time.Time, ttl time.Duration) []string {
	var out []string
	for _, session := range sessions {
		if session.Name == "" || session.HasClients {
			continue
		}
		if session.CreatedAt == 0 {
			continue
		}
		age := now.Sub(time.Unix(session.CreatedAt, 0))
		if age >= ttl {
			out = append(out, session.Name)
		}
	}
	return out
}

func shouldSkipTerminalForInstance(sessionInstance, currentInstance string, activeInstances map[string]bool) bool {
	if sessionInstance == "" || sessionInstance == currentInstance {
		return false
	}
	return activeInstances[sessionInstance]
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
	return func() tea.Msg {
		byWorkspace, err := a.amuxSessionsByWorkspace(opts)
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

// gcStaleTerminalSessions kills terminal tmux sessions that are inactive and stale.
func (a *App) gcStaleTerminalSessions() tea.Cmd {
	if !a.tmuxAvailable || !a.projectsLoaded {
		return nil
	}
	opts := a.tmuxOptions
	return func() tea.Msg {
		activeInstances := make(map[string]bool)
		activeRows, err := tmux.SessionsWithTags(
			map[string]string{"@amux": "1"},
			[]string{"@amux_instance"},
			opts,
		)
		if err == nil {
			for _, row := range activeRows {
				instanceID := strings.TrimSpace(row.Tags["@amux_instance"])
				if instanceID == "" {
					continue
				}
				hasClients, err := tmux.SessionHasClients(row.Name, opts)
				if err == nil && hasClients {
					activeInstances[instanceID] = true
				}
			}
		}

		match := map[string]string{
			"@amux":      "1",
			"@amux_type": "terminal",
		}
		rows, err := tmux.SessionsWithTags(
			match,
			[]string{"@amux_workspace", "@amux_created_at", "@amux_instance"},
			opts,
		)
		if err != nil {
			return terminalGCResult{Err: err}
		}
		now := time.Now()
		sessions := make([]terminalSessionInfo, 0, len(rows))
		for _, row := range rows {
			if row.Name == "" {
				continue
			}
			instanceID := strings.TrimSpace(row.Tags["@amux_instance"])
			if shouldSkipTerminalForInstance(instanceID, a.instanceID, activeInstances) {
				continue
			}
			createdAt := int64(0)
			if raw := strings.TrimSpace(row.Tags["@amux_created_at"]); raw != "" {
				createdAt, _ = strconv.ParseInt(raw, 10, 64)
			}
			if createdAt == 0 {
				if fallback, err := tmux.SessionCreatedAt(row.Name, opts); err == nil {
					createdAt = fallback
				}
			}
			hasClients, err := tmux.SessionHasClients(row.Name, opts)
			if err != nil {
				continue
			}
			sessions = append(sessions, terminalSessionInfo{
				Name:       row.Name,
				Workspace:  row.Tags["@amux_workspace"],
				CreatedAt:  createdAt,
				HasClients: hasClients,
				InstanceID: instanceID,
			})
		}
		toKill := selectStaleTerminalSessions(sessions, now, terminalSessionTTL)
		if len(toKill) == 0 {
			return terminalGCResult{Killed: 0}
		}
		killed := 0
		for _, name := range toKill {
			if err := tmux.KillSession(name, opts); err == nil {
				killed++
			}
		}
		return terminalGCResult{Killed: killed}
	}
}

func (a *App) amuxSessionsByWorkspace(opts tmux.Options) (map[string][]string, error) {
	match := map[string]string{"@amux": "1"}
	if a.instanceID != "" {
		match["@amux_instance"] = a.instanceID
	}
	rows, err := tmux.SessionsWithTags(match, []string{"@amux_workspace"}, opts)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string)
	for _, row := range rows {
		wsID := row.Tags["@amux_workspace"]
		if wsID == "" {
			continue
		}
		out[wsID] = append(out[wsID], row.Name)
	}
	return out, nil
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
