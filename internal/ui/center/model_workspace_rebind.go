package center

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// RebindWorkspaceID migrates tab state from a previous workspace ID to a new one.
// This keeps tabs/session state visible when workspace identity changes during reloads.
func (m *Model) RebindWorkspaceID(previous, current *data.Workspace) tea.Cmd {
	if m == nil || previous == nil || current == nil {
		return nil
	}

	oldID := string(previous.ID())
	newID := string(current.ID())
	if oldID == "" || newID == "" || oldID == newID {
		return nil
	}

	oldTabs, ok := m.tabsByWorkspace[oldID]
	if !ok {
		if m.workspace != nil && m.workspaceID() == oldID {
			m.setWorkspace(current)
		}
		return nil
	}
	if len(oldTabs) == 0 {
		if _, exists := m.tabsByWorkspace[newID]; !exists {
			m.tabsByWorkspace[newID] = []*Tab{}
		}
		if activeIdx, hasOldActive := m.activeTabByWorkspace[oldID]; hasOldActive {
			if _, hasNewActive := m.activeTabByWorkspace[newID]; !hasNewActive {
				m.activeTabByWorkspace[newID] = activeIdx
			}
			delete(m.activeTabByWorkspace, oldID)
		}
		delete(m.tabsByWorkspace, oldID)
		if m.workspace != nil && m.workspaceID() == oldID {
			m.setWorkspace(current)
		}
		m.noteTabsChanged()
		return nil
	}

	newTabs := m.tabsByWorkspace[newID]
	oldActive, oldActiveOK := m.activeTabByWorkspace[oldID]
	newActive, newActiveOK := m.activeTabByWorkspace[newID]
	merged, migratedActive := mergeTabsByID(newTabs, oldTabs, oldActive)

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

	if m.workspace != nil && m.workspaceID() == oldID {
		m.setWorkspace(current)
	}

	var cmds []tea.Cmd
	for _, tab := range merged {
		if tab == nil {
			continue
		}
		tab.mu.Lock()
		tab.Workspace = current
		shouldRestart := tab.Running && tab.Agent != nil && tab.Agent.Terminal != nil && !tab.Agent.Terminal.IsClosed()
		tab.mu.Unlock()

		if shouldRestart {
			m.stopPTYReader(tab)
			if cmd := m.startPTYReader(newID, tab); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	m.noteTabsChanged()
	return common.SafeBatch(cmds...)
}

func mergeTabsByID(existing, incoming []*Tab, incomingActive int) ([]*Tab, int) {
	merged := make([]*Tab, 0, len(existing)+len(incoming))
	indexByID := make(map[TabID]int, len(existing)+len(incoming))

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
