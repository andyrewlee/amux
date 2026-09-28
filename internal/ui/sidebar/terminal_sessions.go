package sidebar

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
)

type SessionAttachInfo struct {
	Name           string
	Attach         bool
	DetachExisting bool
}

func (m *TerminalModel) tabBySession(wsID, sessionName string) *TerminalTab {
	if sessionName == "" {
		return nil
	}
	for _, tab := range m.tabs.ByWorkspace[wsID] {
		if tab.State != nil && tab.State.SessionName == sessionName {
			return tab
		}
	}
	return nil
}

// shouldAttachExistingTerminalTab begins an automatic attach attempt on the
// tab when it is eligible, returning the attempt's epoch. A user-detached,
// already-live, or already-attaching tab is skipped without side effects.
func shouldAttachExistingTerminalTab(tab *TerminalTab) (epoch uint64, ok bool) {
	if tab == nil || tab.State == nil {
		return 0, false
	}
	ts := tab.State
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.UserDetached {
		return 0, false
	}
	if ts.Running && ts.Terminal != nil && ts.VTerm != nil && !ts.Detached {
		return 0, false
	}
	if !ts.beginReattachLocked() {
		return 0, false
	}
	return ts.reattachEpoch, true
}

// AddTabsFromSessionInfos ensures tabs exist for the provided tmux sessions, optionally attaching.
func (m *TerminalModel) AddTabsFromSessionInfos(ws *data.Workspace, sessions []SessionAttachInfo) []tea.Cmd {
	if ws == nil || len(sessions) == 0 {
		return nil
	}
	wsID := string(ws.ID())
	var cmds []tea.Cmd
	for _, session := range sessions {
		if session.Name == "" {
			continue
		}
		existing := m.tabBySession(wsID, session.Name)
		if existing != nil {
			if session.Attach {
				if epoch, ok := shouldAttachExistingTerminalTab(existing); ok {
					cmds = append(cmds, m.attachToSession(ws, existing.ID, session.Name, session.DetachExisting, "reattach", epoch))
				}
			}
			continue
		}
		tabID := generateTerminalTabID()
		tab := &TerminalTab{
			ID:   tabID,
			Name: nextTerminalName(m.tabs.ByWorkspace[wsID]),
			State: &TerminalState{
				SessionName: session.Name,
				Running:     false,
				Detached:    true,
			},
		}
		m.tabs.ByWorkspace[wsID] = append(m.tabs.ByWorkspace[wsID], tab)
		if len(m.tabs.ByWorkspace[wsID]) == 1 {
			m.tabs.ActiveByWorkspace[wsID] = 0
		}
		if session.Attach {
			var epoch uint64
			if tab.State != nil {
				tab.State.mu.Lock()
				if tab.State.beginReattachLocked() {
					epoch = tab.State.reattachEpoch
				}
				tab.State.mu.Unlock()
			}
			if epoch != 0 {
				cmds = append(cmds, m.attachToSession(ws, tabID, session.Name, session.DetachExisting, "reattach", epoch))
			}
		}
	}
	m.refreshTerminalSize()
	return cmds
}
