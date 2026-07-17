package sidebar

import (
	"github.com/andyrewlee/amux/internal/ui/common"
)

// DetachedTerminalTabInfo identifies a terminal tab detached by automatic
// limit enforcement.
type DetachedTerminalTabInfo struct {
	WorkspaceID string
	TabID       TerminalTabID
}

// EnforceAttachedTerminalTabLimit detaches least-recently-used sidebar
// terminal PTYs when the number of attached terminals exceeds maxAttached.
//
// Tabs of the currently selected workspace are never auto-detached: sidebar
// session discovery re-attaches the active workspace's terminals on its
// periodic scan, so detaching them would thrash. Auto-detached sessions stay
// alive in tmux (UserDetached remains false) and are re-attached
// transparently by discovery when their workspace is selected again.
//
// A tab's recency is the later of its workspace's last selection and its own
// last PTY output, so a background terminal still streaming (e.g. a build)
// outranks one sitting at an idle prompt.
func (m *TerminalModel) EnforceAttachedTerminalTabLimit(maxAttached int) []DetachedTerminalTabInfo {
	// 0 means disabled (unlimited attached terminal PTYs).
	if maxAttached <= 0 {
		return nil
	}

	activeWorkspaceID := m.workspaceID()

	attachedCount := 0
	candidates := make([]common.LRUTabCandidate[*TerminalTab], 0)
	for wsID, tabs := range m.tabs.ByWorkspace {
		for idx, tab := range tabs {
			if tab == nil || tab.State == nil {
				continue
			}
			ts := tab.State
			ts.mu.Lock()
			attached := ts.Running && !ts.Detached
			inFlight := ts.reattachInFlight
			lastOutput := ts.LastOutputAt
			ts.mu.Unlock()
			if !attached {
				continue
			}
			attachedCount++
			if wsID == activeWorkspaceID || inFlight {
				continue
			}
			lastActive := m.lastActiveAt[wsID]
			if lastOutput.After(lastActive) {
				lastActive = lastOutput
			}
			candidates = append(candidates, common.LRUTabCandidate[*TerminalTab]{
				WorkspaceID: wsID,
				Index:       idx,
				LastActive:  lastActive,
				Payload:     tab,
			})
		}
	}

	excess := attachedCount - maxAttached
	if excess <= 0 || len(candidates) == 0 {
		return nil
	}

	common.SortLRUTabCandidates(candidates)

	if excess > len(candidates) {
		excess = len(candidates)
	}

	detached := make([]DetachedTerminalTabInfo, 0, excess)
	for _, candidate := range candidates[:excess] {
		m.detachState(candidate.Payload.State, false)
		detached = append(detached, DetachedTerminalTabInfo{
			WorkspaceID: candidate.WorkspaceID,
			TabID:       candidate.Payload.ID,
		})
	}
	return detached
}
