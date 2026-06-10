package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
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
	a.tmuxActivity.activeWorkspaceIDs = msg.ActiveWorkspaceIDs
	a.tmuxActivity.settledScans++
	if a.tmuxActivity.settledScans >= tmuxActivitySettleScans {
		a.tmuxActivity.settled = true
	}
	a.syncActiveWorkspacesToDashboard()
	return a.dashboard.StartSpinnerIfNeeded()
}
