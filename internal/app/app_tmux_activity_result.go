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
	if msg.Token != a.tmuxActivityToken {
		// Stale result from an older scan; ignore to avoid overwriting newer state.
		return nil
	}

	a.tmuxActivityScanInFlight = false
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

	if a.tmuxActivityRescanPending && a.tmuxAvailable {
		a.tmuxActivityRescanPending = false
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

	previousRoleSet := a.tmuxActivityOwnershipSet
	previousOwner := a.tmuxActivityScannerOwner
	previousEpoch := a.tmuxActivityOwnerEpoch

	a.tmuxActivityOwnershipSet = true
	a.tmuxActivityScannerOwner = msg.ScannerOwner
	if msg.ScannerEpoch > 0 {
		a.tmuxActivityOwnerEpoch = msg.ScannerEpoch
	}

	if !previousRoleSet || previousOwner != msg.ScannerOwner || (msg.ScannerEpoch > 0 && previousEpoch != msg.ScannerEpoch) {
		role := "follower"
		if msg.ScannerOwner {
			role = "owner"
		}
		logging.Info("tmux activity role=%s epoch=%d instance=%s", role, a.tmuxActivityOwnerEpoch, strings.TrimSpace(a.instanceID))
	}

	if !isTmuxActivityOwnerTransition(previousRoleSet, previousOwner, previousEpoch, msg) {
		return
	}

	// Reset local hysteresis when entering owner mode so we never reuse state
	// created under an older owner epoch.
	a.sessionActivityStates = make(map[string]*activity.SessionState)
	// Clear follower/shared activity immediately. If the first owner scan fails,
	// stale follower markers should not remain visible.
	a.tmuxActiveWorkspaceIDs = make(map[string]bool)
	a.syncActiveWorkspacesToDashboard()
	// Do not reset tmuxActivitySettledScans on role transitions. Settlement tracks
	// continuity of successfully applied activity payloads, independent of owner
	// identity, and only increments in applyTmuxActivityPayload.
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
		a.sessionActivityStates[name] = state
	}
	a.tmuxActiveWorkspaceIDs = msg.ActiveWorkspaceIDs
	a.tmuxActivitySettledScans++
	if a.tmuxActivitySettledScans >= tmuxActivitySettleScans {
		a.tmuxActivitySettled = true
	}
	a.syncActiveWorkspacesToDashboard()
	return a.dashboard.StartSpinnerIfNeeded()
}
