package app

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/tmux"
)

const orphanSessionGracePeriod = 30 * time.Second

// orphanGCResult is returned after attempting to clean up orphaned tmux sessions.
type orphanGCResult struct {
	Killed int
	Err    error
}

func (a *App) collectKnownWorkspaceIDs() map[string]bool {
	ids := make(map[string]bool)
	a.eachWorkspace(func(ws *data.Workspace, _ *data.Project) {
		// Collect every identity form: sessions spawned before stable IDs may
		// carry a drifted computed form in their @amux_workspace tag, and an
		// unrecognized tag here means the session gets killed as an orphan.
		for _, id := range workspacesvc.WorkspaceMetadataIDs(ws) {
			ids[string(id)] = true
		}
	})
	for id := range a.lifecycle.snapshotCreating() {
		ids[id] = true
	}
	// A workspace mid-mutation may already be absent from a.projects (loadProjects
	// replaces it after each WorkspaceDeleted), so without this a concurrently
	// scheduled orphan GC would see its session as unknown and kill it — racing
	// the orderly cleanup, and on a failed delete killing a still-needed agent.
	for id := range a.snapshotMutatingWorkspaceIDs() {
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
			return orphanGCResult{Err: activity.ErrTmuxUnavailable}
		}
		now := time.Now()
		byWorkspace, err := a.amuxSessionsByWorkspace(opts)
		if err != nil {
			return orphanGCResult{Err: err}
		}
		a.refreshOwnedSessionHeartbeats(byWorkspace, now, opts)
		// Batched session facts for the per-orphan checks below. If either
		// listing fails we cannot confirm orphans are client-free or dead —
		// fail closed and skip the pass rather than probe per session.
		meta, metaErr := svc.AllSessionMeta(opts)
		allStates, statesErr := svc.AllSessionStates(opts)
		if err := errors.Join(metaErr, statesErr); err != nil {
			return orphanGCResult{Err: fmt.Errorf("session metadata/states: %w", err)}
		}
		killed := a.killOrphanedSessions(byWorkspace, knownIDs, allStates, meta, now, opts)
		return orphanGCResult{Killed: killed}
	}
}

type workspaceSession struct {
	Name             string
	CreatedAt        int64
	InstanceID       string
	OwnerHeartbeatAt time.Time
}

func (a *App) amuxSessionsByWorkspace(opts tmux.Options) (map[string][]workspaceSession, error) {
	if a.tmuxService == nil {
		return nil, activity.ErrTmuxUnavailable
	}
	match := map[string]string{"@amux": "1"}
	rows, err := a.tmuxService.SessionsWithTags(match, []string{
		"@amux_workspace",
		"@amux_created_at",
		"@amux_instance",
		tmux.TagSessionLeaseAt,
		tmux.TagSessionOwnerHeartbeatAt,
	}, opts)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]workspaceSession)
	for _, row := range rows {
		wsID := strings.TrimSpace(row.Tags["@amux_workspace"])
		if wsID == "" {
			continue
		}
		rowInstanceID := strings.TrimSpace(row.Tags["@amux_instance"])
		if !instancesShareState(rowInstanceID, a.instanceID) {
			continue
		}
		var createdAt int64
		if raw := strings.TrimSpace(row.Tags["@amux_created_at"]); raw != "" {
			createdAt, _ = strconv.ParseInt(raw, 10, 64)
		}
		out[wsID] = append(out[wsID], workspaceSession{
			Name:             row.Name,
			CreatedAt:        createdAt,
			InstanceID:       rowInstanceID,
			OwnerHeartbeatAt: sessionOwnerHeartbeatAt(row.Tags),
		})
	}
	return out, nil
}

func (a *App) refreshOwnedSessionHeartbeats(byWorkspace map[string][]workspaceSession, now time.Time, opts tmux.Options) {
	instanceID := strings.TrimSpace(a.instanceID)
	if instanceID == "" || a.tmuxService == nil {
		return
	}
	names := make([]string, 0)
	for _, sessions := range byWorkspace {
		for _, session := range sessions {
			if session.InstanceID != instanceID || session.Name == "" {
				continue
			}
			if !session.OwnerHeartbeatAt.IsZero() && !session.OwnerHeartbeatAt.After(now) &&
				now.Sub(session.OwnerHeartbeatAt) < sessionOwnerHeartbeatInterval {
				continue
			}
			names = append(names, session.Name)
		}
	}
	if len(names) == 0 {
		return
	}
	if err := a.tmuxService.SetSessionTagValueForSessions(
		names,
		tmux.TagSessionOwnerHeartbeatAt,
		strconv.FormatInt(now.UnixMilli(), 10),
		opts,
	); err != nil {
		logging.Warn("session owner heartbeat refresh failed: %v", err)
	}
}

func (a *App) killOrphanedSessions(byWorkspace map[string][]workspaceSession, knownIDs map[string]bool, allStates map[string]tmux.SessionState, meta map[string]tmux.SessionMeta, now time.Time, opts tmux.Options) int {
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
			if foreignWorkspaceSessionOwnerAlive(ws, a.instanceID, now) {
				continue
			}
			m, metaOK := meta[ws.Name]
			createdAt := ws.CreatedAt
			if createdAt == 0 && metaOK {
				createdAt = m.CreatedAt
			}
			if isRecentOrphanSession(createdAt, now) {
				continue
			}
			// Fail closed: never kill a session we could not confirm is
			// client-free. A missing meta entry means it vanished between
			// listing and classification — same outcome as the old
			// SessionHasClients error path.
			if !metaOK {
				logging.Warn("orphan GC: no session metadata for %s; skipping", ws.Name)
				continue
			}
			if m.Attached > 0 {
				continue
			}
			state, ok := allStates[ws.Name]
			if !ok {
				// A detached session can still be running an agent, build, or proof.
				// Missing metadata is not enough evidence to terminate that work;
				// only collect sessions whose panes are confirmed dead.
				logging.Warn("orphan GC: no pane state for %s; skipping", ws.Name)
				continue
			}
			if state.Exists && state.HasLivePane {
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

func sessionOwnerHeartbeatAt(tags map[string]string) time.Time {
	if heartbeat, ok := activity.ParseLastOutputAtTag(tags[tmux.TagSessionOwnerHeartbeatAt]); ok {
		return heartbeat
	}
	// Compatibility fallback: current releases set the activity lease at
	// create/reattach time, so a peer gets a safety window before the first
	// dedicated owner heartbeat is written.
	if lease, ok := activity.ParseLastOutputAtTag(tags[tmux.TagSessionLeaseAt]); ok {
		return lease
	}
	return time.Time{}
}

func ownerHeartbeatAlive(heartbeatAt, now time.Time) bool {
	if heartbeatAt.IsZero() {
		return false
	}
	if heartbeatAt.After(now) {
		return true // fail closed under clock skew
	}
	return now.Sub(heartbeatAt) <= sessionOwnerStaleAfter
}

func foreignWorkspaceSessionOwnerAlive(session workspaceSession, currentInstanceID string, now time.Time) bool {
	owner := strings.TrimSpace(session.InstanceID)
	current := strings.TrimSpace(currentInstanceID)
	return owner != "" && owner != current && ownerHeartbeatAlive(session.OwnerHeartbeatAt, now)
}

func foreignSessionOwnerAlive(tags map[string]string, currentInstanceID string, now time.Time) bool {
	owner := strings.TrimSpace(tags["@amux_instance"])
	current := strings.TrimSpace(currentInstanceID)
	return owner != "" && owner != current && ownerHeartbeatAlive(sessionOwnerHeartbeatAt(tags), now)
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
			return sessionCountResult{Err: activity.ErrTmuxUnavailable}
		}
		match := map[string]string{"@amux": "1"}
		rows, err := svc.SessionsWithTags(match, nil, opts)
		if err != nil {
			return sessionCountResult{Err: err}
		}
		return sessionCountResult{Count: len(rows)}
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
	for _, key := range []string{
		"session_activity",
		tmux.TagSessionLeaseAt,
		tmux.TagLastOutputAt,
		tmux.TagLastInputAt,
	} {
		// For GC, lease refreshes represent owner-maintained activity and are
		// intentionally considered alongside output/input tags.
		// session_activity stores unix seconds; ParseLastOutputAtTag resolves
		// units by magnitude (seconds vs millis/nanos).
		raw := strings.TrimSpace(tags[key])
		if raw == "" {
			continue
		}
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
