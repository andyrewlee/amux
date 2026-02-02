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

func (m *TerminalModel) hasSession(wsID, sessionName string) bool {
	if sessionName == "" {
		return false
	}
	for _, tab := range m.tabsByWorkspace[wsID] {
		if tab.State != nil && tab.State.SessionName == sessionName {
			return true
		}
	}
	return false
}

// AddTabsFromSessions ensures tabs exist for the provided tmux session names.
func (m *TerminalModel) AddTabsFromSessions(ws *data.Workspace, sessions []string) []tea.Cmd {
	if ws == nil || len(sessions) == 0 {
		return nil
	}
	wsID := string(ws.ID())
	var cmds []tea.Cmd
	for _, sessionName := range sessions {
		if m.hasSession(wsID, sessionName) {
			continue
		}
		tabID := generateTerminalTabID()
		tab := &TerminalTab{
			ID:   tabID,
			Name: nextTerminalName(m.tabsByWorkspace[wsID]),
			State: &TerminalState{
				SessionName: sessionName,
				Running:     false,
				Detached:    true,
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
		if m.hasSession(wsID, session.Name) {
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
			cmds = append(cmds, m.attachToSession(ws, tabID, session.Name, session.DetachExisting, "reattach"))
		}
	}
	m.refreshTerminalSize()
	return cmds
}
