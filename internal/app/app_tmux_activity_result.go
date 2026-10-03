package app

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
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

	var cmds []tea.Cmd
	if cmd := a.updateTmuxActivityOwnershipState(msg); cmd != nil {
		cmds = append(cmds, cmd)
	}
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

func (a *App) updateTmuxActivityOwnershipState(msg tmuxActivityResult) tea.Cmd {
	if !msg.RoleKnown {
		return nil
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
		return nil
	}

	// Reset local hysteresis when entering owner mode so we never reuse state
	// created under an older owner epoch. The semantic baseline clears with
	// it: a new epoch re-observes sessions as first observations, which
	// republishes current tags but cannot fire on-done (no locally observed
	// Working edge exists yet).
	a.tmuxActivity.sessionStates = make(map[string]*activity.SessionState)
	a.tmuxActivity.agentStateBaseline = make(map[string]activity.AgentState)
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
	return a.syncActiveWorkspacesToDashboard()
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
	// Adopt the scan's complete retained session map; it already excludes
	// pruned sessions and carries scan-side mutations that emitted no update.
	// Results without it (followers, hand-built) keep the delta merge.
	if msg.SessionStates != nil {
		a.tmuxActivity.sessionStates = msg.SessionStates
	} else {
		for name, state := range msg.UpdatedStates {
			a.tmuxActivity.sessionStates[name] = state
		}
		for _, name := range msg.RemovedStates {
			delete(a.tmuxActivity.sessionStates, name)
		}
	}
	// Per-session @amux_agent_state transitions run only for owner scans:
	// followers consume shared workspace-level state and must not publish
	// per-session tags or fire session hooks. The comparison is against the
	// last ACCEPTED semantic value (the baseline), not a reclassified previous
	// snapshot — only that lets clock-driven transitions (e.g. Done→Idle when
	// DoneWindow expires with no content change) publish, since the snapshot
	// alone can never differ from its own reclassification.
	var agentStateChanges []agentStateTagChange
	if msg.ScannerOwner {
		if a.tmuxActivity.agentStateBaseline == nil {
			a.tmuxActivity.agentStateBaseline = make(map[string]activity.AgentState)
		}
		now := msg.Now
		if now.IsZero() {
			now = time.Now()
		}
		agentStateChanges = sessionAgentStateChanges(a.tmuxActivity.sessionStates, a.tmuxActivity.agentStateBaseline, now)
	}
	prevStates := a.tmuxActivity.agentStates
	doneCount := countWorkingToDone(prevStates, msg.AgentStates)
	a.tmuxActivity.activeWorkspaceIDs = msg.ActiveWorkspaceIDs
	a.tmuxActivity.agentStates = msg.AgentStates
	a.tmuxActivity.settledScans++
	if a.tmuxActivity.settledScans >= tmuxActivitySettleScans {
		a.tmuxActivity.settled = true
	}
	dashboardCmd := a.syncActiveWorkspacesToDashboard()
	spinner := a.dashboard.StartSpinnerIfNeeded()
	tagCmd := agentStateTagWriteCmd(agentStateChanges, a.tmuxOptions)
	hookCmd := a.onDoneHookCmd(agentStateChanges)
	if hookCmd != nil {
		tagCmd = common.SafeBatch(tagCmd, hookCmd)
	}
	if doneCount > 0 && a.toast != nil {
		msgText := "Agent finished"
		if doneCount > 1 {
			msgText = fmt.Sprintf("%d agents finished", doneCount)
		}
		return common.SafeBatch(a.toast.ShowInfo(msgText), spinner, tagCmd, dashboardCmd)
	}
	return common.SafeBatch(spinner, tagCmd, dashboardCmd)
}

// agentStateTagChange pairs a tmux session name with its newly classified
// AgentState, used to coalesce @amux_agent_state tag writes to true state
// transitions (see sessionAgentStateChanges). prev is the classification the
// session had on the previous scan; the on-done hook fires only on a strict
// working→done edge, so it needs both sides.
type agentStateTagChange struct {
	sessionName string
	state       activity.AgentState
	prev        activity.AgentState
}

// sessionAgentStateChanges classifies every retained session in `current` via
// activity.ClassifyState at `now` and returns the sessions whose semantic
// AgentState differs from the baseline — the last value this instance
// accepted (and therefore published) for that session. It then updates
// `baseline` in place: accepted values are recorded, first observations are
// seeded with prev=state (publishing the current tag to correct external
// drift while guaranteeing no false working→done hook edge), and sessions no
// longer retained are dropped so a reappearing session re-observes fresh.
//
// Evaluating the complete retained set is what makes clock-driven transitions
// publish: a quiet session emits no update for scans at a time, yet its
// classification still advances (Done→Idle when DoneWindow expires). The
// baseline — not a reclassified snapshot — is the only side of the diff that
// remembers that Done was ever published.
func sessionAgentStateChanges(current map[string]*activity.SessionState, baseline map[string]activity.AgentState, now time.Time) []agentStateTagChange {
	var changes []agentStateTagChange
	for name, state := range current {
		nextState := activity.ClassifyState(state, now)
		prevState, observed := baseline[name]
		baseline[name] = nextState
		if !observed {
			// First observation under this ownership epoch: publish the
			// current value (a stale external tag self-corrects) with prev
			// pinned to the same state so no transition edge is fabricated.
			changes = append(changes, agentStateTagChange{sessionName: name, state: nextState, prev: nextState})
			continue
		}
		if nextState != prevState {
			changes = append(changes, agentStateTagChange{sessionName: name, state: nextState, prev: prevState})
		}
	}
	for name := range baseline {
		if _, retained := current[name]; !retained {
			delete(baseline, name)
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

// runOnDoneHook is a seam over ScriptRunner.RunOnDone so tests can observe
// fires without spawning. Production always uses the runner.
var runOnDoneHook = func(ws *data.Workspace, sessionName string, svc *workspacesvc.Service) error {
	return svc.RunOnDoneScript(ws, sessionName)
}

// onDoneHookCmd turns strict working→done session edges into on-done hook
// invocations. The workspace is resolved on the main thread (findWorkspaceByID
// touches App state); the returned Cmd only does process work off-loop, the
// same discipline as agentStateTagWriteCmd. Edges other than working→done —
// including a first-scan idle→done for an agent that finished before amux
// started — do not fire: a hook could carry side effects (git push, deploy)
// and must only run for a completion the user actually watched happen.
func (a *App) onDoneHookCmd(changes []agentStateTagChange) tea.Cmd {
	var fires []struct {
		ws          *data.Workspace
		sessionName string
	}
	for _, change := range changes {
		if change.state != activity.StateDone || change.prev != activity.StateWorking {
			continue
		}
		ws := a.findWorkspaceByID(activity.WorkspaceIDFromSessionName(change.sessionName))
		if ws == nil {
			continue
		}
		fires = append(fires, struct {
			ws          *data.Workspace
			sessionName string
		}{ws, change.sessionName})
	}
	if len(fires) == 0 {
		return nil
	}
	svc := a.workspaceService
	return func() tea.Msg {
		// Attempt every captured fire in order. One hook's dispatch error
		// must not starve later eligible hooks — the baseline already
		// advanced, so a skipped fire would never retry. Each failure still
		// reports its own WorkspaceOnDoneResult; hooks themselves stay
		// sequential and no edge is replayed.
		var reports []tea.Cmd
		for _, f := range fires {
			if err := runOnDoneHook(f.ws, f.sessionName, svc); err != nil {
				res := messages.WorkspaceOnDoneResult{Workspace: f.ws, SessionName: f.sessionName, Err: err}
				reports = append(reports, func() tea.Msg { return res })
			}
		}
		if cmd := common.SafeBatch(reports...); cmd != nil {
			return cmd()
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
