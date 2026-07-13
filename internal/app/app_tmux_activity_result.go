package app

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func (a *App) handleTmuxActivityResult(msg tmuxActivityResult) []tea.Cmd {
	if msg.Token != a.tmuxActivity.token {
		// Stale result from an older scan; ignore to avoid overwriting newer state.
		return nil
	}

	a.tmuxActivity.scanInFlight = false
	a.updateTmuxActivityOwnershipState(msg)

	var cmds []tea.Cmd
	if stoppedTabsCmd := stoppedTabUpdatesCmd(msg.StoppedTabs); stoppedTabsCmd != nil {
		cmds = append(cmds, stoppedTabsCmd)
	}

	if msg.Err != nil {
		logging.Warn("tmux activity scan failed: %v", msg.Err)
	} else if !msg.SkipApply {
		if spinnerCmd := a.applyTmuxActivityPayload(msg); spinnerCmd != nil {
			cmds = append(cmds, spinnerCmd)
		}
	}

	if a.tmuxActivity.rescanPending && a.tmuxAvailable {
		a.tmuxActivity.rescanPending = false
		if scanCmd := a.scanTmuxActivityNow(); scanCmd != nil {
			cmds = append(cmds, scanCmd)
		}
	}
	return cmds
}

func (a *App) updateTmuxActivityOwnershipState(msg tmuxActivityResult) {
	if !msg.RoleKnown {
		return
	}

	previousRoleSet := a.tmuxActivity.ownershipSet
	previousOwner := a.tmuxActivity.scannerOwner
	previousEpoch := a.tmuxActivity.ownerEpoch

	a.tmuxActivity.ownershipSet = true
	a.tmuxActivity.scannerOwner = msg.ScannerOwner
	if msg.ScannerEpoch > 0 {
		a.tmuxActivity.ownerEpoch = msg.ScannerEpoch
	}

	if !previousRoleSet || previousOwner != msg.ScannerOwner || (msg.ScannerEpoch > 0 && previousEpoch != msg.ScannerEpoch) {
		role := "follower"
		if msg.ScannerOwner {
			role = "owner"
		}
		logging.Info("tmux activity role=%s epoch=%d instance=%s", role, a.tmuxActivity.ownerEpoch, strings.TrimSpace(a.instanceID))
	}

	if !isTmuxActivityOwnerTransition(previousRoleSet, previousOwner, previousEpoch, msg) {
		return
	}

	// Reset local hysteresis when entering owner mode so we never reuse state
	// created under an older owner epoch.
	a.tmuxActivity.sessionStates = make(map[string]*activity.SessionState)
	// Clear follower/shared activity immediately. If the first owner scan fails,
	// stale follower markers should not remain visible.
	a.tmuxActivity.activeWorkspaceIDs = make(map[string]bool)
	a.tmuxActivity.agentStates = make(map[string]activity.AgentState)
	// Re-enter the unsettled state so this transient empty set is not published as
	// authoritative. While !settled, syncActiveWorkspacesToDashboard short-circuits
	// to an empty publish that the dashboard treats as "not yet known" rather than a
	// confirmed all-idle set, so working-agent spinners are not blinked off between
	// the handoff and the new owner's first scans. applyTmuxActivityPayload re-settles
	// after tmuxActivitySettleScans successful owner scans, repopulating indicators.
	// Mirrors the tmux-availability reset in scanTmuxActivity.
	a.tmuxActivity.settled = false
	a.tmuxActivity.settledScans = 0
	a.syncActiveWorkspacesToDashboard()
}

func isTmuxActivityOwnerTransition(
	previousRoleSet bool,
	previousOwner bool,
	previousEpoch int64,
	msg tmuxActivityResult,
) bool {
	// Reset hysteresis only on follower->owner transitions. While follower, local
	// hysteresis is unused for shared activity decisions.
	return msg.ScannerOwner &&
		(!previousRoleSet || !previousOwner || (msg.ScannerEpoch > 0 && previousEpoch != msg.ScannerEpoch))
}

func stoppedTabUpdatesCmd(updates []messages.TabSessionStatus) tea.Cmd {
	if len(updates) == 0 {
		return nil
	}
	// Apply stopped-tab updates even when a scan also returns an error.
	// Session-status reconciliation is still valid and should not be dropped.
	stoppedTabCmds := make([]tea.Cmd, 0, len(updates))
	for _, update := range updates {
		updateCopy := update
		stoppedTabCmds = append(stoppedTabCmds, func() tea.Msg { return updateCopy })
	}
	return common.SafeBatch(stoppedTabCmds...)
}

func (a *App) applyTmuxActivityPayload(msg tmuxActivityResult) tea.Cmd {
	// A scan contributes to settle only when activity is actually applied.
	// Follower scans without a readable shared snapshot set SkipApply=true so we
	// don't settle on unknown activity state.
	if msg.ActiveWorkspaceIDs == nil {
		msg.ActiveWorkspaceIDs = make(map[string]bool)
	}
	// Compute per-session @amux_agent_state transitions before merging: this
	// reads a.tmuxActivity.sessionStates as it stood at the end of the previous
	// scan (the "prev" side of the transition) against msg.UpdatedStates (the
	// "next" side), which is keyed by session name and therefore gives a clean
	// (session name, new state) pairing with no workspace-level ambiguity.
	agentStateChanges := sessionAgentStateChanges(a.tmuxActivity.sessionStates, msg.UpdatedStates, time.Now())
	// Merge updated hysteresis states back on the main thread.
	for name, state := range msg.UpdatedStates {
		a.tmuxActivity.sessionStates[name] = state
	}
	// Prune states the scan dropped after they went unseen long enough; this
	// bounds the otherwise monotonic growth of sessionActivityStates (deleted
	// workspaces' sessions never reappear in the scan). Delete after the merge so
	// a same-scan re-add cannot be undone.
	for _, name := range msg.RemovedStates {
		delete(a.tmuxActivity.sessionStates, name)
	}
	prevStates := a.tmuxActivity.agentStates
	doneCount := countWorkingToDone(prevStates, msg.AgentStates)
	a.tmuxActivity.activeWorkspaceIDs = msg.ActiveWorkspaceIDs
	a.tmuxActivity.agentStates = msg.AgentStates
	a.tmuxActivity.settledScans++
	if a.tmuxActivity.settledScans >= tmuxActivitySettleScans {
		a.tmuxActivity.settled = true
	}
	a.syncActiveWorkspacesToDashboard()
	spinner := a.dashboard.StartSpinnerIfNeeded()
	tagCmd := agentStateTagWriteCmd(agentStateChanges, a.tmuxOptions)
	if doneCount > 0 && a.toast != nil {
		msgText := "Agent finished"
		if doneCount > 1 {
			msgText = fmt.Sprintf("%d agents finished", doneCount)
		}
		return common.SafeBatch(a.toast.ShowInfo(msgText), spinner, tagCmd)
	}
	return common.SafeBatch(spinner, tagCmd)
}

// agentStateTagChange pairs a tmux session name with its newly classified
// AgentState, used to coalesce @amux_agent_state tag writes to true state
// transitions (see sessionAgentStateChanges).
type agentStateTagChange struct {
	sessionName string
	state       activity.AgentState
}

// sessionAgentStateChanges classifies each session in updatedStates via
// activity.ClassifyState (the same deterministic per-session classification
// ClassifyWorkspaceStates and the dashboard indicators use) both before and
// after this scan's update, and returns only the sessions whose classification
// actually changed. This is the coalescing that keeps @amux_agent_state writes
// bounded to real transitions instead of firing on every ~5s scan tick,
// mirroring how the working->done toast (countWorkingToDone) only fires on
// transition.
func sessionAgentStateChanges(prevStates, updatedStates map[string]*activity.SessionState, now time.Time) []agentStateTagChange {
	var changes []agentStateTagChange
	for name, next := range updatedStates {
		prevState := activity.ClassifyState(prevStates[name], now)
		nextState := activity.ClassifyState(next, now)
		if nextState != prevState {
			changes = append(changes, agentStateTagChange{sessionName: name, state: nextState})
		}
	}
	return changes
}

// setAgentStateTag is a seam over tmux.SetSessionTagValue so tests can assert
// exactly which @amux_agent_state writes agentStateTagWriteCmd issues without a
// live tmux server. Production always uses the real tmux.SetSessionTagValue.
var setAgentStateTag = tmux.SetSessionTagValue

// agentStateTagWriteCmd returns a best-effort command that publishes
// @amux_agent_state for each changed session. Like the existing
// @amux_last_output_at / @amux_last_input_at tag writes, failures are ignored
// (external orchestrators tolerate a missing/stale tag) and the write happens
// off the main Update-loop goroutine via the returned tea.Cmd — the session
// names and target states were already resolved synchronously on the main
// thread by sessionAgentStateChanges, so this closure touches no App state.
func agentStateTagWriteCmd(changes []agentStateTagChange, opts tmux.Options) tea.Cmd {
	if len(changes) == 0 {
		return nil
	}
	return func() tea.Msg {
		for _, change := range changes {
			_ = setAgentStateTag(change.sessionName, tmux.TagAgentState, change.state.String(), opts)
		}
		return nil
	}
}

// countWorkingToDone counts the number of workspaces that transitioned from
// StateWorking to StateDone between prev and next. Only strict working→done
// transitions are counted to avoid spurious toasts on first scan (when prev is
// empty and a workspace may already read StateDone).
func countWorkingToDone(prev, next map[string]activity.AgentState) int {
	count := 0
	for wsID, st := range next {
		if st == activity.StateDone && prev[wsID] == activity.StateWorking {
			count++
		}
	}
	return count
}
