package app

import (
	"errors"
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
	Token activityScanToken
}

type tmuxActivityResult struct {
	Token              activityScanToken
	ActiveWorkspaceIDs map[string]bool
	UpdatedStates      map[string]*activity.SessionState // Updated hysteresis states to merge
	RemovedStates      []string                          // Session states pruned this scan (delete on merge)
	StoppedTabs        []messages.TabSessionStatus
	SkipApply          bool
	ScannerOwner       bool
	ScannerEpoch       int64
	RoleKnown          bool
	Err                error
}

// snapshotActivityStates creates a deep copy of session activity states for use in a goroutine.
// This avoids concurrent map access between the Update loop and Cmd goroutines.
func (a *App) snapshotActivityStates() map[string]*activity.SessionState {
	snapshot := make(map[string]*activity.SessionState, len(a.tmuxActivity.sessionStates))
	for name, state := range a.tmuxActivity.sessionStates {
		// Copy the struct to avoid sharing pointers
		stateCopy := *state
		snapshot[name] = &stateCopy
	}
	return snapshot
}

func (a *App) startTmuxActivityTicker() tea.Cmd {
	a.tmuxActivity.token++
	return a.scheduleTmuxActivityTick()
}

func (a *App) scheduleTmuxActivityTick() tea.Cmd {
	token := a.tmuxActivity.token
	return common.SafeTick(tmuxActivityInterval, func(time.Time) tea.Msg {
		return tmuxActivityTick{Token: token}
	})
}

func (a *App) triggerTmuxActivityScan() tea.Cmd {
	token := a.tmuxActivity.token
	return func() tea.Msg {
		return tmuxActivityTick{Token: token}
	}
}

// eagerScanTmuxActivity schedules an immediate activity rescan when tmux is
// available, used on agent tab start/reattach so a freshly running agent does
// not wait up to one full ticker interval (~5s) before its working indicator
// can appear. It coalesces with any in-flight scan via scanTmuxActivityNow and
// no-ops when tmux is unavailable to avoid churn.
func (a *App) eagerScanTmuxActivity() tea.Cmd {
	if !a.tmuxAvailable {
		return nil
	}
	return a.scanTmuxActivityNow()
}

func (a *App) scanTmuxActivityNow() tea.Cmd {
	if a.tmuxActivity.scanInFlight {
		a.tmuxActivity.rescanPending = true
		return nil
	}
	a.tmuxActivity.scanInFlight = true
	a.tmuxActivity.rescanPending = false
	a.tmuxActivity.token++
	scanToken := a.tmuxActivity.token
	infoBySession := a.tabSessionInfoByName()
	statesSnapshot := a.snapshotActivityStates()
	opts := a.tmuxOptions
	if opts.CommandTimeout <= 0 || opts.CommandTimeout > tmuxCommandTimeout {
		opts.CommandTimeout = tmuxCommandTimeout
	}
	svc := a.tmuxService
	return func() tea.Msg {
		return a.runTmuxActivityScan(scanToken, infoBySession, statesSnapshot, opts, svc)
	}
}

func (a *App) handleTmuxActivityTick(msg tmuxActivityTick) []tea.Cmd {
	if msg.Token != a.tmuxActivity.token {
		return []tea.Cmd{a.scheduleTmuxActivityTick()}
	}
	if !a.tmuxAvailable {
		return []tea.Cmd{a.scheduleTmuxActivityTick()}
	}
	if a.tmuxActivity.scanInFlight {
		a.tmuxActivity.rescanPending = true
		return []tea.Cmd{a.scheduleTmuxActivityTick()}
	}
	a.tmuxActivity.scanInFlight = true
	a.tmuxActivity.rescanPending = false
	// Increment token for this scan so out-of-order results are rejected.
	// Each scan gets a unique token; only the most recent result is applied.
	a.tmuxActivity.token++
	scanToken := a.tmuxActivity.token
	sessionInfo := a.tabSessionInfoByName()
	statesSnapshot := a.snapshotActivityStates()
	opts := a.tmuxOptions
	if opts.CommandTimeout <= 0 || opts.CommandTimeout > tmuxCommandTimeout {
		opts.CommandTimeout = tmuxCommandTimeout
	}
	svc := a.tmuxService
	cmds := []tea.Cmd{a.scheduleTmuxActivityTick(), func() tea.Msg {
		return a.runTmuxActivityScan(scanToken, sessionInfo, statesSnapshot, opts, svc)
	}}
	return cmds
}

func (a *App) runTmuxActivityScan(
	scanToken activityScanToken,
	infoBySession map[string]activity.SessionInfo,
	statesSnapshot map[string]*activity.SessionState,
	opts tmux.Options,
	svc TmuxOps,
) tmuxActivityResult {
	if svc == nil {
		return tmuxActivityResult{Token: scanToken, Err: activity.ErrTmuxUnavailable}
	}

	ownerEpoch, sharedRoleKnown, followerResult := a.resolveScanRole(scanToken, infoBySession, opts, svc)
	if followerResult != nil {
		return *followerResult
	}

	sessions, stoppedTabs, err := a.fetchAndSyncActivitySessionStates(infoBySession, opts, svc)
	if err != nil {
		return tmuxActivityResult{
			Token: scanToken,
			Err:   err,
			// sharedRoleKnown implies ownership was resolved before local scan work;
			// keep that role metadata on scan errors so the UI can preserve role state.
			ScannerOwner: sharedRoleKnown,
			ScannerEpoch: ownerEpoch,
			RoleKnown:    sharedRoleKnown,
		}
	}
	recentActivityBySession, err := activity.FetchRecentlyActiveByWindow(svc, tmuxActivityPrefilter, opts)
	if err != nil {
		logging.Warn("tmux activity prefilter failed; using unbounded stale-tag fallback: %v", err)
		recentActivityBySession = nil
	}
	active, updatedStates, removedStates := activity.ActiveWorkspaceIDsFromTagsWithRemoved(infoBySession, sessions, recentActivityBySession, statesSnapshot, opts, svc.CapturePaneTail, svc.ContentHash)
	result := tmuxActivityResult{
		Token:              scanToken,
		ActiveWorkspaceIDs: active,
		UpdatedStates:      updatedStates,
		RemovedStates:      removedStates,
		StoppedTabs:        stoppedTabs,
		ScannerOwner:       true,
		ScannerEpoch:       ownerEpoch,
		RoleKnown:          sharedRoleKnown,
	}
	if sharedRoleKnown {
		a.publishActivitySnapshot(&result, active, opts)
	}
	return result
}

// resolveScanRole resolves shared-scan ownership for this scan. It returns the
// owner epoch and whether the shared role is known. When this instance is a
// follower it also returns the complete follower result to send instead of
// running a local scan; resolution errors fall back to an ownerless local scan.
func (a *App) resolveScanRole(
	scanToken activityScanToken,
	infoBySession map[string]activity.SessionInfo,
	opts tmux.Options,
	svc TmuxOps,
) (ownerEpoch int64, roleKnown bool, followerResult *tmuxActivityResult) {
	if !a.sharedTmuxActivityEnabled() {
		return 0, false, nil
	}
	role, sharedActive, applyShared, epoch, err := a.resolveTmuxActivityScanRole(opts, time.Now())
	if err != nil {
		if !tmux.IsNoServerError(err) {
			logging.Warn("tmux activity ownership resolution failed; falling back to local scan: %v", err)
		}
		return 0, false, nil
	}
	if role == tmuxActivityRoleFollower {
		result := a.runFollowerScan(scanToken, infoBySession, sharedActive, applyShared, epoch, opts, svc)
		return epoch, true, &result
	}
	return epoch, true, nil
}

// runFollowerScan handles the non-owner path: it still syncs per-session
// states (so stopped tabs are detected locally) and then either applies the
// owner's published active set or skips applying when that snapshot is stale.
func (a *App) runFollowerScan(
	scanToken activityScanToken,
	infoBySession map[string]activity.SessionInfo,
	sharedActive map[string]bool,
	applyShared bool,
	epoch int64,
	opts tmux.Options,
	svc TmuxOps,
) tmuxActivityResult {
	_, stoppedTabs, syncErr := a.fetchAndSyncActivitySessionStates(infoBySession, opts, svc)
	if syncErr != nil {
		logging.Warn("tmux activity follower session-state sync failed: %v", syncErr)
	}
	if !applyShared {
		return tmuxActivityResult{
			Token:        scanToken,
			SkipApply:    true,
			StoppedTabs:  stoppedTabs,
			ScannerOwner: false,
			ScannerEpoch: epoch,
			RoleKnown:    true,
		}
	}
	if sharedActive == nil {
		sharedActive = make(map[string]bool)
	}
	return tmuxActivityResult{
		Token:              scanToken,
		ActiveWorkspaceIDs: sharedActive,
		StoppedTabs:        stoppedTabs,
		ScannerOwner:       false,
		ScannerEpoch:       epoch,
		RoleKnown:          true,
	}
}

// publishActivitySnapshot revalidates the ownership lease and publishes the
// owner's active set to the shared snapshot. On lease loss or revalidation
// failure it demotes the result in place to a skip-apply follower result so
// two instances never both apply locally-scanned activity (split brain).
func (a *App) publishActivitySnapshot(result *tmuxActivityResult, active map[string]bool, opts tmux.Options) {
	if result.ScannerEpoch <= 0 {
		result.ScannerEpoch = 1
	}
	publishAt := time.Now()
	canPublish, leaseEpoch, err := a.canPublishTmuxActivitySnapshot(opts, result.ScannerEpoch, publishAt)
	if err != nil {
		logging.Warn("tmux activity lease revalidation failed before snapshot publish: %v", err)
		// Conservative fallback: if ownership cannot be revalidated, skip
		// applying local activity to avoid split-brain ownership effects.
		result.ScannerOwner = false
		result.SkipApply = true
		return
	}
	if !canPublish {
		result.ScannerOwner = false
		result.SkipApply = true
		if leaseEpoch > 0 {
			result.ScannerEpoch = leaseEpoch
		}
		return
	}
	if err := a.publishTmuxActivitySnapshot(opts, active, result.ScannerEpoch, publishAt); err != nil {
		if errors.Is(err, errTmuxActivityOwnershipLostAfterPublish) {
			result.ScannerOwner = false
			result.SkipApply = true
			_, leaseEpoch, checkErr := a.canPublishTmuxActivitySnapshot(opts, result.ScannerEpoch, time.Now())
			if checkErr != nil {
				logging.Warn("tmux activity lease revalidation failed after ownership loss: %v", checkErr)
			} else if leaseEpoch > 0 {
				result.ScannerEpoch = leaseEpoch
			}
			return
		}
		logging.Warn("tmux activity snapshot publish failed: %v", err)
	}
}

func (a *App) fetchAndSyncActivitySessionStates(
	infoBySession map[string]activity.SessionInfo,
	opts tmux.Options,
	svc TmuxOps,
) ([]activity.TaggedSession, []messages.TabSessionStatus, error) {
	sessions, err := activity.FetchTaggedSessions(svc, infoBySession, opts)
	if err != nil {
		return nil, nil, err
	}
	// Mutates infoBySession so IsRunningSession sees corrected statuses.
	if a.tmuxActivity.missBySession == nil {
		a.tmuxActivity.missBySession = make(map[string]int)
	}
	stoppedTabs := syncActivitySessionStates(infoBySession, sessions, svc, opts, a.tmuxActivity.missBySession)
	return sessions, stoppedTabs, nil
}

func (a *App) handleTmuxAvailableResult(msg tmuxAvailableResult) []tea.Cmd {
	a.tmuxCheckDone = true
	a.tmuxAvailable = msg.available
	a.tmuxInstallHint = msg.installHint
	a.tmuxActivity.settled = false
	a.tmuxActivity.settledScans = 0
	a.tmuxActivity.activeWorkspaceIDs = make(map[string]bool)
	a.syncActiveWorkspacesToDashboard()
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

// syncActivitySessionStates reconciles the in-memory session info map with live
// tmux state. It mutates infoBySession in place — setting Status to "stopped" for
// dead/disappeared sessions and "running" for revived ones — so that the subsequent
// ActiveWorkspaceIDsFromTags call (which filters via IsRunningSession) sees corrected
// statuses. It returns TabSessionStatus messages for sessions whose status changed
// from a running-like state to stopped.
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

		if !state.Exists || !state.HasLivePane {
			var emit bool
			info, emit = recordSessionMiss(missBySession, sessionName, info)
			if emit {
				if wsID := strings.TrimSpace(info.WorkspaceID); wsID != "" {
					stoppedTabs = append(stoppedTabs, messages.TabSessionStatus{
						WorkspaceID: wsID,
						SessionName: sessionName,
						Status:      "stopped",
					})
				}
			}
		} else {
			// Live again: reset the miss counter and revive a previously-stopped
			// session so it is no longer treated as dead.
			delete(missBySession, sessionName)
			if strings.EqualFold(info.Status, "stopped") {
				info.Status = "running"
			}
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
			if wsID := strings.TrimSpace(info.WorkspaceID); wsID != "" {
				stoppedTabs = append(stoppedTabs, messages.TabSessionStatus{
					WorkspaceID: wsID,
					SessionName: sessionName,
					Status:      "stopped",
				})
			}
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
