package app

import (
	"strings"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/tmux"
)

// tabSessionInfoByName builds an activity.SessionInfo map from the current projects.
// Concurrency safety: built synchronously in the Update loop.
func (a *App) tabSessionInfoByName() map[string]activity.SessionInfo {
	infoBySession := make(map[string]activity.SessionInfo)
	assistants := map[string]struct{}{}
	if a.config != nil {
		for name := range a.config.Assistants {
			assistants[name] = struct{}{}
		}
	}
	for _, project := range a.projects {
		for i := range project.Workspaces {
			ws := &project.Workspaces[i]
			for _, tab := range ws.OpenTabs {
				name := strings.TrimSpace(tab.SessionName)
				if name == "" {
					continue
				}
				status := strings.ToLower(strings.TrimSpace(tab.Status))
				if status == "" {
					status = "running"
				}
				assistant := strings.TrimSpace(tab.Assistant)
				_, isChat := assistants[assistant]
				infoBySession[name] = activity.SessionInfo{
					Status:      status,
					WorkspaceID: string(ws.ID()),
					Assistant:   assistant,
					IsChat:      isChat,
				}
			}
		}
	}
	return infoBySession
}

// activityDemotionMissThreshold is the number of consecutive non-live activity
// observations required before a running session is demoted to stopped. A single
// best-effort AllSessionStates call can miss a live session under load, so one
// miss must not tear down a working background agent.
const activityDemotionMissThreshold = 2

// recordSessionMiss increments the consecutive-non-live counter for a session and,
// once the hysteresis threshold is reached, marks it stopped. It returns the
// (possibly updated) info and whether a stopped message should be emitted — true
// only on the transition from a running-like status.
func recordSessionMiss(missBySession map[string]int, sessionName string, info activity.SessionInfo) (activity.SessionInfo, bool) {
	// A nil counter disables hysteresis (single-miss demotion). The production
	// caller always passes a real map; some unit tests opt out to assert the
	// per-observation decision directly.
	if missBySession != nil {
		missBySession[sessionName]++
		if missBySession[sessionName] < activityDemotionMissThreshold {
			return info, false
		}
	}
	wasRunningLike := activity.IsRunningSession(info, true)
	info.Status = "stopped"
	return info, wasRunningLike
}

func appendStoppedTabStatus(stoppedTabs []messages.TabSessionStatus, sessionName string, info activity.SessionInfo) []messages.TabSessionStatus {
	wsID := strings.TrimSpace(info.WorkspaceID)
	if wsID == "" {
		return stoppedTabs
	}
	return append(stoppedTabs, messages.TabSessionStatus{
		WorkspaceID: wsID,
		SessionName: sessionName,
		Status:      "stopped",
	})
}

// syncActivitySessionStates reconciles the in-memory session info map with live
// tmux state. It mutates infoBySession in place — setting Status to "stopped" for
// dead/disappeared sessions and "running" for revived ones — so that the subsequent
// ActiveWorkspaceIDsFromTagsWithRemoved call (which filters via IsRunningSession) sees corrected
// statuses. It returns TabSessionStatus messages for sessions whose status changed
// from a running-like state to stopped.
func syncActivitySessionStates(
	infoBySession map[string]activity.SessionInfo,
	sessions []activity.TaggedSession,
	svc TmuxOps,
	opts tmux.Options,
	missBySession map[string]int,
) []messages.TabSessionStatus {
	stoppedTabs := make([]messages.TabSessionStatus, 0)
	if len(infoBySession) == 0 {
		clear(missBySession)
		return stoppedTabs
	}
	if svc == nil {
		return stoppedTabs
	}

	// Batch: single tmux call gets existence + live-pane status for all sessions.
	allStates, err := svc.AllSessionStates(opts)
	if err != nil {
		logging.Warn("AllSessionStates failed, skipping session state sync: %v", err)
		return stoppedTabs
	}

	checked := make(map[string]struct{}, len(sessions))
	for _, snapshot := range sessions {
		sessionName := strings.TrimSpace(snapshot.Session.Name)
		if sessionName == "" {
			continue
		}
		if _, ok := checked[sessionName]; ok {
			continue
		}
		checked[sessionName] = struct{}{}

		info, ok := infoBySession[sessionName]
		if !ok {
			continue
		}

		state := allStates[sessionName] // zero value if missing (Exists=false)

		if state.Exists && state.HasLivePane {
			// Live again: reset the miss counter and revive a previously-stopped
			// session so it is no longer treated as dead.
			delete(missBySession, sessionName)
			if strings.EqualFold(info.Status, "stopped") {
				info.Status = "running"
			}
			infoBySession[sessionName] = info
			continue
		}

		var emit bool
		info, emit = recordSessionMiss(missBySession, sessionName, info)
		if emit {
			stoppedTabs = appendStoppedTabStatus(stoppedTabs, sessionName, info)
		}
		infoBySession[sessionName] = info
	}

	// Sessions that no longer appear in list-sessions are non-live this scan; the
	// same hysteresis applies before demoting them.
	for sessionName, info := range infoBySession {
		if _, ok := checked[sessionName]; ok {
			continue
		}
		var emit bool
		info, emit = recordSessionMiss(missBySession, sessionName, info)
		infoBySession[sessionName] = info
		if emit {
			stoppedTabs = appendStoppedTabStatus(stoppedTabs, sessionName, info)
		}
	}

	// Prune miss counters for sessions that are no longer open. infoBySession is
	// rebuilt fresh each scan from currently-open tabs, so any missBySession key
	// absent from it belongs to a closed tab/workspace. Mirrors the sessionStates
	// prune (app_tmux_activity_result.go) so both maps shed teardown leftovers on
	// the same signal instead of growing unbounded over a long session.
	for sessionName := range missBySession {
		if _, ok := infoBySession[sessionName]; !ok {
			delete(missBySession, sessionName)
		}
	}

	return stoppedTabs
}
