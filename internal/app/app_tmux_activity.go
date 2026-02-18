package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
)

type tmuxActivityTick struct {
	Token int
}

type tmuxActivityResult struct {
	Token              int
	ActiveWorkspaceIDs map[string]bool
	UpdatedStates      map[string]*activity.SessionState // Updated hysteresis states to merge
	StoppedTabs        []messages.TabSessionStatus
	Err                error
}

// snapshotActivityStates creates a deep copy of session activity states for use in a goroutine.
// This avoids concurrent map access between the Update loop and Cmd goroutines.
func (a *App) snapshotActivityStates() map[string]*activity.SessionState {
	snapshot := make(map[string]*activity.SessionState, len(a.sessionActivityStates))
	for name, state := range a.sessionActivityStates {
		// Copy the struct to avoid sharing pointers
		stateCopy := *state
		snapshot[name] = &stateCopy
	}
	return snapshot
}

func (a *App) startTmuxActivityTicker() tea.Cmd {
	a.tmuxActivityToken++
	return a.scheduleTmuxActivityTick()
}

func (a *App) scheduleTmuxActivityTick() tea.Cmd {
	token := a.tmuxActivityToken
	return common.SafeTick(tmuxActivityInterval, func(time.Time) tea.Msg {
		return tmuxActivityTick{Token: token}
	})
}

func (a *App) triggerTmuxActivityScan() tea.Cmd {
	token := a.tmuxActivityToken
	return func() tea.Msg {
		return tmuxActivityTick{Token: token}
	}
}

func (a *App) scanTmuxActivityNow() tea.Cmd {
	if a.tmuxActivityScanInFlight {
		a.tmuxActivityRescanPending = true
		return nil
	}
	a.tmuxActivityScanInFlight = true
	a.tmuxActivityRescanPending = false
	a.tmuxActivityToken++
	scanToken := a.tmuxActivityToken
	infoBySession := a.tabSessionInfoByName()
	statesSnapshot := a.snapshotActivityStates()
	opts := a.tmuxOptions
	if opts.CommandTimeout <= 0 || opts.CommandTimeout > tmuxCommandTimeout {
		opts.CommandTimeout = tmuxCommandTimeout
	}
	svc := a.tmuxService
	return func() tea.Msg {
		if svc == nil {
			return tmuxActivityResult{Token: scanToken, Err: activity.ErrTmuxUnavailable}
		}
		sessions, err := activity.FetchTaggedSessions(svc, infoBySession, opts)
		if err != nil {
			return tmuxActivityResult{Token: scanToken, Err: err}
		}
		stoppedTabs := syncActivitySessionStates(infoBySession, sessions, svc, opts)
		recentActivityBySession, err := activity.FetchRecentlyActiveByWindow(svc, tmuxActivityPrefilter, opts)
		if err != nil {
			logging.Warn("tmux activity prefilter failed; using unbounded stale-tag fallback: %v", err)
			recentActivityBySession = nil
		}
		active, updatedStates := activity.ActiveWorkspaceIDsFromTags(infoBySession, sessions, recentActivityBySession, statesSnapshot, opts, svc.CapturePaneTail, svc.ContentHash)
		return tmuxActivityResult{Token: scanToken, ActiveWorkspaceIDs: active, UpdatedStates: updatedStates, StoppedTabs: stoppedTabs}
	}
}

func (a *App) handleTmuxActivityTick(msg tmuxActivityTick) []tea.Cmd {
	if msg.Token != a.tmuxActivityToken {
		return []tea.Cmd{a.scheduleTmuxActivityTick()}
	}
	if !a.tmuxAvailable {
		return []tea.Cmd{a.scheduleTmuxActivityTick()}
	}
	if a.tmuxActivityScanInFlight {
		a.tmuxActivityRescanPending = true
		return []tea.Cmd{a.scheduleTmuxActivityTick()}
	}
	a.tmuxActivityScanInFlight = true
	a.tmuxActivityRescanPending = false
	// Increment token for this scan so out-of-order results are rejected.
	// Each scan gets a unique token; only the most recent result is applied.
	a.tmuxActivityToken++
	scanToken := a.tmuxActivityToken
	sessionInfo := a.tabSessionInfoByName()
	statesSnapshot := a.snapshotActivityStates()
	opts := a.tmuxOptions
	if opts.CommandTimeout <= 0 || opts.CommandTimeout > tmuxCommandTimeout {
		opts.CommandTimeout = tmuxCommandTimeout
	}
	svc := a.tmuxService
	cmds := []tea.Cmd{a.scheduleTmuxActivityTick(), func() tea.Msg {
		if svc == nil {
			return tmuxActivityResult{Token: scanToken, Err: activity.ErrTmuxUnavailable}
		}
		sessions, err := activity.FetchTaggedSessions(svc, sessionInfo, opts)
		if err != nil {
			return tmuxActivityResult{Token: scanToken, Err: err}
		}
		stoppedTabs := syncActivitySessionStates(sessionInfo, sessions, svc, opts)
		recentActivityBySession, err := activity.FetchRecentlyActiveByWindow(svc, tmuxActivityPrefilter, opts)
		if err != nil {
			logging.Warn("tmux activity prefilter failed; using unbounded stale-tag fallback: %v", err)
			recentActivityBySession = nil
		}
		active, updatedStates := activity.ActiveWorkspaceIDsFromTags(sessionInfo, sessions, recentActivityBySession, statesSnapshot, opts, svc.CapturePaneTail, svc.ContentHash)
		return tmuxActivityResult{Token: scanToken, ActiveWorkspaceIDs: active, UpdatedStates: updatedStates, StoppedTabs: stoppedTabs}
	}}
	return cmds
}

func (a *App) handleTmuxActivityResult(msg tmuxActivityResult) []tea.Cmd {
	if msg.Token != a.tmuxActivityToken {
		// Stale result from an older scan; ignore to avoid overwriting newer state
		return nil
	}
	a.tmuxActivityScanInFlight = false
	var cmds []tea.Cmd
	if msg.Err != nil {
		logging.Warn("tmux activity scan failed: %v", msg.Err)
	} else {
		if msg.ActiveWorkspaceIDs == nil {
			msg.ActiveWorkspaceIDs = make(map[string]bool)
		}
		// Merge updated hysteresis states back into the main map (on main thread)
		for name, state := range msg.UpdatedStates {
			a.sessionActivityStates[name] = state
		}
		if len(msg.StoppedTabs) > 0 {
			stoppedTabCmds := make([]tea.Cmd, 0, len(msg.StoppedTabs))
			for _, update := range msg.StoppedTabs {
				stoppedTabCmds = append(stoppedTabCmds, func() tea.Msg { return update })
			}
			if len(stoppedTabCmds) > 0 {
				cmds = append(cmds, common.SafeBatch(stoppedTabCmds...))
			}
		}
		a.tmuxActiveWorkspaceIDs = msg.ActiveWorkspaceIDs
		a.syncActiveWorkspacesToDashboard()
		if cmd := a.dashboard.StartSpinnerIfNeeded(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if a.tmuxActivityRescanPending && a.tmuxAvailable {
		a.tmuxActivityRescanPending = false
		if scanCmd := a.scanTmuxActivityNow(); scanCmd != nil {
			cmds = append(cmds, scanCmd)
		}
	}
	return cmds
}

func (a *App) handleTmuxAvailableResult(msg tmuxAvailableResult) []tea.Cmd {
	a.tmuxCheckDone = true
	a.tmuxAvailable = msg.available
	a.tmuxInstallHint = msg.installHint
	if !msg.available {
		return []tea.Cmd{a.toast.ShowError("tmux not installed. " + msg.installHint)}
	}
	cmds := []tea.Cmd{a.scanTmuxActivityNow()}
	if a.activeWorkspace != nil {
		if discoverCmd := a.discoverWorkspaceTabsFromTmux(a.activeWorkspace); discoverCmd != nil {
			cmds = append(cmds, discoverCmd)
		}
		if discoverTermCmd := a.discoverSidebarTerminalsFromTmux(a.activeWorkspace); discoverTermCmd != nil {
			cmds = append(cmds, discoverTermCmd)
		}
		if syncCmd := a.syncWorkspaceTabsFromTmux(a.activeWorkspace); syncCmd != nil {
			cmds = append(cmds, syncCmd)
		}
	}
	if a.tmuxService != nil {
		cmds = append(cmds, func() tea.Msg {
			_ = a.tmuxService.SetMonitorActivityOn(a.tmuxOptions)
			_ = a.tmuxService.SetStatusOff(a.tmuxOptions)
			return nil
		})
	}
	return cmds
}

// resetAllTabStatuses marks all non-stopped tabs as stopped and schedules
// persistence for changed workspaces. Used when switching tmux servers so
// the UI doesn't show stale running/detached status.
func (a *App) resetAllTabStatuses() []tea.Cmd {
	var cmds []tea.Cmd
	for i := range a.projects {
		for j := range a.projects[i].Workspaces {
			ws := &a.projects[i].Workspaces[j]
			changed := false
			for k := range ws.OpenTabs {
				if ws.OpenTabs[k].Status != "" && ws.OpenTabs[k].Status != "stopped" {
					ws.OpenTabs[k].Status = "stopped"
					changed = true
				}
			}
			if changed {
				cmds = append(cmds, a.persistWorkspaceTabs(string(ws.ID())))
			}
		}
	}
	return cmds
}

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

func syncActivitySessionStates(
	infoBySession map[string]activity.SessionInfo,
	sessions []activity.TaggedSession,
	svc *tmuxService,
	opts tmux.Options,
) []messages.TabSessionStatus {
	stoppedTabs := make([]messages.TabSessionStatus, 0)
	if svc == nil || len(infoBySession) == 0 {
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
		prevStatus := strings.TrimSpace(strings.ToLower(info.Status))
		isRunningLikeStatus := prevStatus == "" || prevStatus == "running" || prevStatus == "detached"

		state := allStates[sessionName] // zero value if missing (Exists=false)

		if !state.Exists || !state.HasLivePane {
			info.Status = "stopped"
			if isRunningLikeStatus {
				if wsID := strings.TrimSpace(info.WorkspaceID); wsID != "" {
					stoppedTabs = append(stoppedTabs, messages.TabSessionStatus{
						WorkspaceID: wsID,
						SessionName: sessionName,
						Status:      "stopped",
					})
				}
			}
		} else if strings.EqualFold(info.Status, "stopped") {
			info.Status = "running"
		}
		infoBySession[sessionName] = info
	}

	// Sessions that no longer appear in list-sessions are no longer running.
	for sessionName, info := range infoBySession {
		if _, ok := checked[sessionName]; ok {
			continue
		}
		prevStatus := strings.TrimSpace(strings.ToLower(info.Status))
		isRunningLikeStatus := prevStatus == "" || prevStatus == "running" || prevStatus == "detached"
		if isRunningLikeStatus {
			info.Status = "stopped"
			infoBySession[sessionName] = info
			wsID := strings.TrimSpace(info.WorkspaceID)
			if wsID != "" {
				stoppedTabs = append(stoppedTabs, messages.TabSessionStatus{
					WorkspaceID: wsID,
					SessionName: sessionName,
					Status:      "stopped",
				})
			}
		}
	}

	return stoppedTabs
}
