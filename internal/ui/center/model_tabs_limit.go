package center

import (
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"
)

// DetachedTabInfo identifies a tab detached by automatic limit enforcement.
type DetachedTabInfo struct {
	WorkspaceID string
	TabID       TabID
}

type attachedTabCandidate struct {
	workspaceID string
	tabID       TabID
	index       int
	lastFocused time.Time
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
	candidates := make([]attachedTabCandidate, 0)
	for wsID, tabs := range m.tabsByWorkspace {
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
			candidates = append(candidates, attachedTabCandidate{
				workspaceID: wsID,
				tabID:       tabID,
				index:       idx,
				lastFocused: lastFocused,
			})
		}
	}

	excess := attachedCount - maxAttached
	if excess <= 0 || len(candidates) == 0 {
		return nil, nil
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if left.lastFocused.Equal(right.lastFocused) {
			if left.workspaceID == right.workspaceID {
				return left.index < right.index
			}
			return left.workspaceID < right.workspaceID
		}
		if left.lastFocused.IsZero() {
			return true
		}
		if right.lastFocused.IsZero() {
			return false
		}
		return left.lastFocused.Before(right.lastFocused)
	})

	if excess > len(candidates) {
		excess = len(candidates)
	}

	detached := make([]DetachedTabInfo, 0, excess)
	var cmds []tea.Cmd
	for _, candidate := range candidates[:excess] {
		tab := m.getTabByID(candidate.workspaceID, candidate.tabID)
		if tab == nil {
			continue
		}
		if cmd := m.detachTab(tab, candidate.index); cmd != nil {
			cmds = append(cmds, cmd)
		}
		detached = append(detached, DetachedTabInfo{
			WorkspaceID: candidate.workspaceID,
			TabID:       candidate.tabID,
		})
	}
	return detached, cmds
}
