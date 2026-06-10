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

	oldTabs, ok := m.tabs.ByWorkspace[oldID]
	if !ok {
		if m.workspace != nil && m.workspaceID() == oldID {
			m.setWorkspace(current)
		}
		return nil
	}
	// An explicit empty slice (len==0) means the workspace was seen but has no
	// tabs, which is distinct from the !ok case above (workspace never seen).
	// Migrate this "seen but empty" state to preserve the semantic difference.
	if len(oldTabs) == 0 {
		if _, exists := m.tabs.ByWorkspace[newID]; !exists {
			m.tabs.ByWorkspace[newID] = []*Tab{}
		}
		if activeIdx, hasOldActive := m.tabs.ActiveByWorkspace[oldID]; hasOldActive {
			if _, hasNewActive := m.tabs.ActiveByWorkspace[newID]; !hasNewActive {
				m.tabs.ActiveByWorkspace[newID] = activeIdx
			}
			delete(m.tabs.ActiveByWorkspace, oldID)
		}
		delete(m.tabs.ByWorkspace, oldID)
		if m.workspace != nil && m.workspaceID() == oldID {
			m.setWorkspace(current)
		}
		m.noteTabsChanged()
		return nil
	}

	merged := common.RebindTabMaps(m.tabs.ByWorkspace, m.tabs.ActiveByWorkspace, oldID, newID,
		func(t *Tab) TabID { return t.ID },
		func(t *Tab) bool { return t == nil })

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
