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
	for _, tab := range m.tabsByWorkspace[wsID] {
		if tab.State != nil && tab.State.SessionName == sessionName {
			return tab
		}
	}
	return nil
}

func shouldAttachExistingTerminalTab(tab *TerminalTab) bool {
	if tab == nil || tab.State == nil {
		return false
	}
	ts := tab.State
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.reattachInFlight {
		return false
	}
	if ts.UserDetached {
		return false
	}
	if ts.Running && ts.Terminal != nil && ts.VTerm != nil && !ts.Detached {
		return false
	}
	ts.reattachInFlight = true
	return true
}

// AddTabsFromSessions ensures tabs exist for the provided tmux session names.
func (m *TerminalModel) AddTabsFromSessions(ws *data.Workspace, sessions []string) []tea.Cmd {
	if ws == nil || len(sessions) == 0 {
		return nil
	}
	wsID := string(ws.ID())
	var cmds []tea.Cmd
	for _, sessionName := range sessions {
		existing := m.tabBySession(wsID, sessionName)
		if existing != nil {
			if shouldAttachExistingTerminalTab(existing) {
				cmds = append(cmds, m.attachToSession(ws, existing.ID, sessionName, true, "reattach"))
			}
			continue
		}
		tabID := generateTerminalTabID()
		tab := &TerminalTab{
			ID:   tabID,
			Name: nextTerminalName(m.tabsByWorkspace[wsID]),
			State: &TerminalState{
				SessionName:      sessionName,
				Running:          false,
				Detached:         true,
				reattachInFlight: true,
			},
		}
		m.tabsByWorkspace[wsID] = append(m.tabsByWorkspace[wsID], tab)
		if len(m.tabsByWorkspace[wsID]) == 1 {
			m.activeTabByWorkspace[wsID] = 0
		}
		cmds = append(cmds, m.attachToSession(ws, tabID, sessionName, true, "reattach"))
	}
	m.refreshTerminalSize()
	return cmds
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
			if session.Attach && shouldAttachExistingTerminalTab(existing) {
				cmds = append(cmds, m.attachToSession(ws, existing.ID, session.Name, session.DetachExisting, "reattach"))
			}
			continue
		}
		tabID := generateTerminalTabID()
		tab := &TerminalTab{
			ID:   tabID,
			Name: nextTerminalName(m.tabsByWorkspace[wsID]),
			State: &TerminalState{
				SessionName: session.Name,
				Running:     false,
				Detached:    true,
			},
		}
		m.tabsByWorkspace[wsID] = append(m.tabsByWorkspace[wsID], tab)
		if len(m.tabsByWorkspace[wsID]) == 1 {
			m.activeTabByWorkspace[wsID] = 0
		}
		if session.Attach {
			if tab.State != nil {
				tab.State.mu.Lock()
				tab.State.reattachInFlight = true
				tab.State.mu.Unlock()
			}
			cmds = append(cmds, m.attachToSession(ws, tabID, session.Name, session.DetachExisting, "reattach"))
		}
	}
	m.refreshTerminalSize()
	return cmds
}
