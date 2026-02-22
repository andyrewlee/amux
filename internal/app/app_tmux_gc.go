package app

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/tmux"
)

const orphanSessionGracePeriod = 30 * time.Second

// orphanGCResult is returned after attempting to clean up orphaned tmux sessions.
type orphanGCResult struct {
	Killed int
	Err    error
}

// staleDetachedAgentGCResult is returned after attempting to clean up stale
// detached agent sessions.
type staleDetachedAgentGCResult struct {
	Considered      int
	Killed          int
	SkippedAttached int
	SkippedFresh    int
	SkippedLivePane int
	Err             error
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
		killed := a.killOrphanedSessions(byWorkspace, knownIDs, now, opts)
		return orphanGCResult{Killed: killed}
	}
}

// gcStaleTerminalSessions is a no-op since sessions are always persisted.
func (a *App) gcStaleTerminalSessions() tea.Cmd {
	return nil
}

func (a *App) gcStaleDetachedAgentSessions() tea.Cmd {
	if !a.tmuxAvailable {
		return nil
	}
	if a.sharedTmuxActivityEnabled() && a.tmuxActivityOwnershipSet && !a.tmuxActivityScannerOwner {
		return nil
	}
	opts := a.tmuxOptions
	svc := a.tmuxService
	return func() tea.Msg {
		if svc == nil {
			return staleDetachedAgentGCResult{Err: errTmuxUnavailable}
		}

		rows, err := svc.SessionsWithTags(
			map[string]string{"@amux": "1", "@amux_type": "agent"},
			[]string{
				"@amux_created_at",
				tmux.TagLastOutputAt,
				tmux.TagLastInputAt,
				tmux.TagSessionLeaseAt,
			},
			opts,
		)
		if err != nil {
			return staleDetachedAgentGCResult{Err: err}
		}

		allStates, err := svc.AllSessionStates(opts)
		if err != nil {
			return staleDetachedAgentGCResult{Err: err}
		}

		now := time.Now()
		result := staleDetachedAgentGCResult{}
		for _, row := range rows {
			sessionName := strings.TrimSpace(row.Name)
			if sessionName == "" {
				continue
			}
			result.Considered++

			hasClients, err := svc.SessionHasClients(sessionName, opts)
			if err != nil {
				logging.Warn("detached agent GC: failed to check clients for %s: %v", sessionName, err)
				continue
			}
			if hasClients {
				result.SkippedAttached++
				continue
			}

			lastActiveAt := activityTagTime(row.Tags)
			if lastActiveAt.IsZero() {
				if createdAt, err := svc.SessionCreatedAt(sessionName, opts); err == nil && createdAt > 0 {
					lastActiveAt = time.Unix(createdAt, 0)
				}
			}
			if lastActiveAt.IsZero() {
				lastActiveAt = now
			}
			if lastActiveAt.After(now) {
				result.SkippedFresh++
				continue
			}
			inactiveFor := now.Sub(lastActiveAt)
			if inactiveFor < detachedAgentStaleAfter {
				result.SkippedFresh++
				continue
			}
			state, ok := allStates[sessionName]
			if ok && state.Exists && state.HasLivePane && inactiveFor < detachedAgentLivePaneStaleAfter {
				result.SkippedLivePane++
				continue
			}

			if err := svc.KillSession(sessionName, opts); err != nil {
				logging.Warn("detached agent GC: failed to kill session %s: %v", sessionName, err)
				continue
			}
			result.Killed++
		}
		return result
	}
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

func (a *App) killOrphanedSessions(byWorkspace map[string][]workspaceSession, knownIDs map[string]bool, now time.Time, opts tmux.Options) int {
	if a.tmuxService == nil {
		return 0
	}
	killed := 0
	for wsID, sessions := range byWorkspace {
		if knownIDs[wsID] {
			continue
		}
		for _, ws := range sessions {
			if ws.Name == "" {
				continue
			}
			createdAt := ws.CreatedAt
			if createdAt == 0 {
				if ts, err := a.tmuxService.SessionCreatedAt(ws.Name, opts); err == nil {
					createdAt = ts
				}
			}
			if isRecentOrphanSession(createdAt, now) {
				continue
			}
			hasClients, err := a.tmuxService.SessionHasClients(ws.Name, opts)
			if err != nil {
				logging.Warn("orphan GC: failed to check clients for %s: %v", ws.Name, err)
			}
			if hasClients {
				continue
			}
			if err := a.tmuxService.KillSession(ws.Name, opts); err != nil {
				logging.Warn("orphan GC: failed to kill session %s: %v", ws.Name, err)
				continue
			}
			killed++
		}
	}
	return killed
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

// handleOrphanGCTick runs orphan GC and restarts the ticker.
func (a *App) handleOrphanGCTick() []tea.Cmd {
	var cmds []tea.Cmd
	if gcCmd := a.gcStaleDetachedAgentSessions(); gcCmd != nil {
		cmds = append(cmds, gcCmd)
	}
	if gcCmd := a.gcOrphanedTmuxSessions(); gcCmd != nil {
		cmds = append(cmds, gcCmd)
	}
	cmds = append(cmds, a.startOrphanGCTicker())
	return cmds
}

func (a *App) handleStaleDetachedAgentGCResult(msg staleDetachedAgentGCResult) {
	if msg.Err != nil {
		logging.Warn("detached agent GC: %v", msg.Err)
		return
	}
	if msg.Killed > 0 {
		logging.Info(
			"detached agent GC: killed=%d considered=%d attached=%d fresh=%d live_pane=%d",
			msg.Killed,
			msg.Considered,
			msg.SkippedAttached,
			msg.SkippedFresh,
			msg.SkippedLivePane,
		)
	}
}

// sessionCountResult is returned after counting amux tmux sessions.
type sessionCountResult struct {
	Count int
	Err   error
}

// logSessionCount returns a Cmd that counts @amux=1 sessions and logs the result.
func (a *App) logSessionCount() tea.Cmd {
	if !a.tmuxAvailable {
		return nil
	}
	opts := a.tmuxOptions
	svc := a.tmuxService
	return func() tea.Msg {
		if svc == nil {
			return sessionCountResult{Err: errTmuxUnavailable}
		}
		match := map[string]string{"@amux": "1"}
		rows, err := svc.SessionsWithTags(match, nil, opts)
		if err != nil {
			return sessionCountResult{Err: err}
		}
		return sessionCountResult{Count: len(rows)}
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

func (a *App) handleSessionCountResult(msg sessionCountResult) {
	if msg.Err != nil {
		logging.Warn("session count: %v", msg.Err)
		return
	}
	logging.Info("tmux session count at startup: %d", msg.Count)
}

func activityTagTime(tags map[string]string) time.Time {
	best := time.Time{}
	updateBest := func(candidate time.Time) {
		if candidate.IsZero() {
			return
		}
		if best.IsZero() || candidate.After(best) {
			best = candidate
		}
	}
	if raw := strings.TrimSpace(tags[tmux.TagSessionLeaseAt]); raw != "" {
		if parsed, ok := activity.ParseLastOutputAtTag(raw); ok {
			updateBest(parsed)
		}
	}
	if raw := strings.TrimSpace(tags[tmux.TagLastOutputAt]); raw != "" {
		if parsed, ok := activity.ParseLastOutputAtTag(raw); ok {
			updateBest(parsed)
		}
	}
	if raw := strings.TrimSpace(tags[tmux.TagLastInputAt]); raw != "" {
		if parsed, ok := activity.ParseLastOutputAtTag(raw); ok {
			updateBest(parsed)
		}
	}
	if raw := strings.TrimSpace(tags["@amux_created_at"]); raw != "" {
		if sec, err := strconv.ParseInt(raw, 10, 64); err == nil && sec > 0 {
			updateBest(time.Unix(sec, 0))
		}
	}
	return best
}
