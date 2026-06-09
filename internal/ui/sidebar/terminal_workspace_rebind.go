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

	merged := common.RebindTabMaps(m.tabsByWorkspace, m.activeTabByWorkspace, oldID, newID,
		func(t *TerminalTab) TerminalTabID { return t.ID },
		func(t *TerminalTab) bool { return t == nil })

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
