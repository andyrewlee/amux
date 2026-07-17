package center

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/ui/common"
)

// DetachedTabInfo identifies a tab detached by automatic limit enforcement.
type DetachedTabInfo struct {
	WorkspaceID string
	TabID       TabID
}

// EnforceAttachedAgentTabLimit detaches least-recently-focused chat tabs when
// the number of attached/running chat tabs exceeds maxAttached.
//
// The currently focused tab in the active workspace is never auto-detached.
func (m *Model) EnforceAttachedAgentTabLimit(maxAttached int) ([]DetachedTabInfo, []tea.Cmd) {
	// 0 means disabled (unlimited attached chat tabs).
	if maxAttached <= 0 {
		return nil, nil
	}

	activeWorkspaceID := m.workspaceID()
	activeTabIdx := m.getActiveTabIdx()

	attachedCount := 0
	candidates := make([]common.LRUTabCandidate[TabID], 0)
	for wsID, tabs := range m.tabs.ByWorkspace {
		for idx, tab := range tabs {
			if tab == nil || tab.isClosed() || !m.isChatTab(tab) {
				continue
			}
			tab.mu.Lock()
			attached := !tab.Detached && tab.Running
			lastFocused := tab.lastFocusedAt
			createdAt := tab.createdAt
			tabID := tab.ID
			tab.mu.Unlock()
			if !attached {
				continue
			}
			attachedCount++
			if wsID == activeWorkspaceID && idx == activeTabIdx {
				continue
			}
			if lastFocused.IsZero() && createdAt > 0 {
				lastFocused = time.Unix(createdAt, 0)
			}
			candidates = append(candidates, common.LRUTabCandidate[TabID]{
				WorkspaceID: wsID,
				Index:       idx,
				LastActive:  lastFocused,
				Payload:     tabID,
			})
		}
	}

	excess := attachedCount - maxAttached
	if excess <= 0 || len(candidates) == 0 {
		return nil, nil
	}

	common.SortLRUTabCandidates(candidates)

	if excess > len(candidates) {
		excess = len(candidates)
	}

	detached := make([]DetachedTabInfo, 0, excess)
	var cmds []tea.Cmd
	for _, candidate := range candidates[:excess] {
		tab := m.getTabByID(candidate.WorkspaceID, candidate.Payload)
		if tab == nil {
			continue
		}
		if cmd := m.detachTab(tab, candidate.Index); cmd != nil {
			cmds = append(cmds, cmd)
		}
		detached = append(detached, DetachedTabInfo{
			WorkspaceID: candidate.WorkspaceID,
			TabID:       candidate.Payload,
		})
	}
	return detached, cmds
}
