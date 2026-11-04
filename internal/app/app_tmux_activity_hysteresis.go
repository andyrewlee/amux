package app

import (
	"strings"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/tmux"
)

// tabSessionInfoByName builds an activity.SessionInfo map from the current projects.
// Concurrency safety: built synchronously in the Update loop.
func (a *App) tabSessionInfoByName() map[string]activity.SessionInfo {
	infoBySession := make(map[string]activity.SessionInfo)
	for _, project := range a.projects {
		for i := range project.Workspaces {
			ws := &project.Workspaces[i]
			for _, tab := range ws.OpenTabs {
				name := strings.TrimSpace(tab.SessionName)
				if name == "" {
					continue
				}
				assistant := strings.TrimSpace(tab.Assistant)
				// Single source of truth for chat-classification so activity
				// detection and the center renderer agree, including when no
				// config is loaded (empty-config falls back to the registry).
				isChat := a.config.IsChatAssistant(assistant)
				infoBySession[name] = activity.SessionInfo{
					Status:      data.ParseSessionStatus(string(tab.Status)),
					WorkspaceID: string(ws.ID()),
					Assistant:   assistant,
					IsChat:      isChat,
				}
			}
		}
	}
	return infoBySession
}

// recordSessionMiss advances the session's demotion-hysteresis tracker (the
// checked liveness machine in internal/app/activity) and, once the tracker
// has demoted, maps the session's status through the checked session-status
// table. It returns the (possibly updated) info and whether a stopped
// message should be emitted — true only while the info still reads
// running-like, exactly the historical emit rule.
func recordSessionMiss(missBySession map[string]*activity.Liveness, sessionName string, info activity.SessionInfo) (activity.SessionInfo, bool) {
	// A nil map disables hysteresis (single-miss demotion). The production
	// caller always passes a real map; some unit tests opt out to assert the
	// per-observation decision directly.
	tracker := activity.NewLiveness()
	if missBySession != nil {
		if existing := missBySession[sessionName]; existing != nil {
			tracker = existing
		} else {
			missBySession[sessionName] = tracker
		}
	} else {
		tracker.DemoteNow()
	}
	tracker.ObserveMiss()
	wasRunningLike := activity.IsRunningSession(info, true)
	info.Status = tracker.StatusFor(info.Status)
	return info, wasRunningLike && tracker.Demoted()
}

func appendStoppedTabStatus(stoppedTabs []messages.TabSessionStatus, sessionName string, info activity.SessionInfo) []messages.TabSessionStatus {
	wsID := strings.TrimSpace(info.WorkspaceID)
	if wsID == "" {
		return stoppedTabs
	}
	return append(stoppedTabs, messages.TabSessionStatus{
		WorkspaceID: wsID,
		SessionName: sessionName,
		Status:      string(data.SessionStopped),
	})
}

// syncActivitySessionStates reconciles the in-memory session info map with live
// tmux state. It mutates infoBySession in place — setting Status to "stopped" for
// dead/disappeared sessions and "running" for revived ones — so that the subsequent
// ActiveWorkspaceIDsFromTagsWithRemoved call (which filters via IsRunningSession) sees corrected
// statuses. It returns TabSessionStatus messages for sessions whose status changed
// from a running-like state to stopped, plus the batched session-state map the
// scan already paid for so downstream captures can reuse its liveness hints
// (nil when the batched probe failed).
func syncActivitySessionStates(
	infoBySession map[string]activity.SessionInfo,
	sessions []activity.TaggedSession,
	svc TmuxOps,
	opts tmux.Options,
	missBySession map[string]*activity.Liveness,
) ([]messages.TabSessionStatus, map[string]tmux.SessionState) {
	stoppedTabs := make([]messages.TabSessionStatus, 0)
	if len(infoBySession) == 0 {
		clear(missBySession)
		return stoppedTabs, nil
	}
	if svc == nil {
		return stoppedTabs, nil
	}

	// Batch: single tmux call gets existence + live-pane status for all sessions.
	allStates, err := svc.AllSessionStates(opts)
	if err != nil {
		logging.Warn("AllSessionStates failed, skipping session state sync: %v", err)
		return stoppedTabs, nil
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
			// session so it is no longer treated as dead. The revival runs
			// through the checked session-status table (stopped --live-->
			// running); detached and other statuses are untouched.
			delete(missBySession, sessionName)
			if info.Status == data.SessionStopped {
				if next, ok := data.NextSessionStatus(info.Status, data.SessionObservedLive); ok {
					info.Status = next
				}
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

	return stoppedTabs, allStates
}
