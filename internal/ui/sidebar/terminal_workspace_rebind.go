package sidebar

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// RebindWorkspaceID migrates terminal tabs from a previous workspace ID to a new one.
// This preserves running/session-backed terminal tabs across workspace ID rewrites.
func (m *TerminalModel) RebindWorkspaceID(previous, current *data.Workspace) tea.Cmd {
	if m == nil || previous == nil || current == nil {
		return nil
	}

	oldID := string(previous.ID())
	newID := string(current.ID())
	if oldID == "" || newID == "" || oldID == newID {
		return nil
	}

	oldTabs, ok := m.tabsByWorkspace[oldID]
	if !ok || len(oldTabs) == 0 {
		if m.workspace != nil && string(m.workspace.ID()) == oldID {
			m.workspace = current
		}
		if m.pendingCreation[oldID] {
			m.pendingCreation[newID] = true
			delete(m.pendingCreation, oldID)
		}
		return nil
	}

	newTabs := m.tabsByWorkspace[newID]
	oldActive, oldActiveOK := m.activeTabByWorkspace[oldID]
	newActive, newActiveOK := m.activeTabByWorkspace[newID]
	merged, migratedActive := mergeTerminalTabsByID(newTabs, oldTabs, oldActive)

	m.tabsByWorkspace[newID] = merged
	delete(m.tabsByWorkspace, oldID)
	if oldActiveOK && (!newActiveOK || len(newTabs) == 0) {
		if migratedActive < 0 {
			migratedActive = 0
		}
		if len(merged) == 0 {
			migratedActive = 0
		} else if migratedActive >= len(merged) {
			migratedActive = len(merged) - 1
		}
		m.activeTabByWorkspace[newID] = migratedActive
	} else if newActiveOK {
		if len(merged) == 0 {
			m.activeTabByWorkspace[newID] = 0
		} else if newActive >= len(merged) {
			m.activeTabByWorkspace[newID] = len(merged) - 1
		}
	}
	delete(m.activeTabByWorkspace, oldID)

	if m.pendingCreation[oldID] {
		m.pendingCreation[newID] = true
		delete(m.pendingCreation, oldID)
	}
	if m.workspace != nil && string(m.workspace.ID()) == oldID {
		m.workspace = current
	}

	var cmds []tea.Cmd
	for _, tab := range merged {
		if tab == nil || tab.State == nil {
			continue
		}
		ts := tab.State
		ts.mu.Lock()
		shouldRestart := ts.Running && ts.Terminal != nil
		ts.mu.Unlock()

		if shouldRestart {
			m.stopPTYReader(ts)
			if cmd := m.startPTYReader(newID, tab.ID); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	return common.SafeBatch(cmds...)
}

func mergeTerminalTabsByID(existing, incoming []*TerminalTab, incomingActive int) ([]*TerminalTab, int) {
	merged := make([]*TerminalTab, 0, len(existing)+len(incoming))
	indexByID := make(map[TerminalTabID]int, len(existing)+len(incoming))

	for _, tab := range existing {
		if tab == nil {
			continue
		}
		if _, ok := indexByID[tab.ID]; ok {
			continue
		}
		indexByID[tab.ID] = len(merged)
		merged = append(merged, tab)
	}

	migratedActive := -1
	for i, tab := range incoming {
		if tab == nil {
			continue
		}
		if idx, ok := indexByID[tab.ID]; ok {
			if i == incomingActive {
				migratedActive = idx
			}
			continue
		}
		indexByID[tab.ID] = len(merged)
		merged = append(merged, tab)
		if i == incomingActive {
			migratedActive = len(merged) - 1
		}
	}

	return merged, migratedActive
}
