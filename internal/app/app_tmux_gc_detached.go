package app

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/tmux"
)

// staleDetachedAgentGCResult is returned after attempting to clean up stale
// detached agent sessions.
type staleDetachedAgentGCResult struct {
	Considered       int
	Killed           int
	SkippedAttached  int
	SkippedLiveOwner int
	SkippedFresh     int
	SkippedLivePane  int
	Err              error
}

// collectKnownWorkspaceIDs returns the set of workspace IDs currently tracked
// by the app. Must be called on the Update goroutine.

func (a *App) gcStaleDetachedAgentSessions() tea.Cmd {
	if !a.tmuxAvailable {
		return nil
	}
	opts := a.tmuxOptions
	svc := a.tmuxService
	return func() tea.Msg {
		if svc == nil {
			return staleDetachedAgentGCResult{Err: activity.ErrTmuxUnavailable}
		}

		match := map[string]string{"@amux": "1", "@amux_type": "agent"}
		rows, err := svc.SessionsWithTags(
			match,
			[]string{
				"@amux_instance",
				"@amux_created_at",
				"session_activity",
				tmux.TagLastOutputAt,
				tmux.TagLastInputAt,
				tmux.TagSessionLeaseAt,
				tmux.TagSessionOwnerHeartbeatAt,
			},
			opts,
		)
		if err != nil {
			return staleDetachedAgentGCResult{Err: err}
		}
		// One batched list-sessions call supplies attached-client counts and
		// created-at fallbacks for every row, replacing per-session probes.
		// Without it we cannot confirm any session is client-free — fail closed.
		meta, err := svc.AllSessionMeta(opts)
		if err != nil {
			return staleDetachedAgentGCResult{Err: fmt.Errorf("session metadata: %w", err)}
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
			if !instancesShareState(row.Tags["@amux_instance"], a.instanceID) {
				continue
			}
			result.Considered++

			// Fail closed: never kill a session we could not confirm is
			// client-free. A missing meta entry means the session vanished
			// between listing and classification — same outcome.
			m, metaOK := meta[sessionName]
			if !metaOK {
				logging.Warn("detached agent GC: no session metadata for %s; skipping", sessionName)
				continue
			}
			if m.Attached > 0 {
				result.SkippedAttached++
				continue
			}
			if foreignSessionOwnerAlive(row.Tags, a.instanceID, now) {
				result.SkippedLiveOwner++
				continue
			}

			lastActiveAt := activityTagTime(row.Tags)
			if lastActiveAt.IsZero() {
				// session_created is a tmux-native fallback for sessions whose
				// @amux_created_at tag is absent from list output.
				if m.CreatedAt > 0 {
					lastActiveAt = time.Unix(m.CreatedAt, 0)
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

func (a *App) handleStaleDetachedAgentGCResult(msg staleDetachedAgentGCResult) {
	if msg.Err != nil {
		logging.Warn("detached agent GC: %v", msg.Err)
		return
	}
	if msg.Killed > 0 {
		logging.Info(
			"detached agent GC: killed=%d considered=%d attached=%d live_owner=%d fresh=%d live_pane=%d",
			msg.Killed,
			msg.Considered,
			msg.SkippedAttached,
			msg.SkippedLiveOwner,
			msg.SkippedFresh,
			msg.SkippedLivePane,
		)
	}
}
