package sidebar

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
)

// SessionAttachInfo describes one discovered tmux session for automatic tab
// attach. Attaching never detaches the session's existing clients — amux
// sessions are shared across instances (see tmux.NewClientCommand) — so there
// is no detach policy to carry.
//
// Attach is an explicit seam: production discovery currently lists only
// sessions it wants attached (the sole producer,
// app.buildSidebarSessionAttachInfos, always writes true), and the false
// branch — file the tab without starting an attach — exists for restore-style
// flows and is pinned by tests.
type SessionAttachInfo struct {
	Name   string
	Attach bool
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
// already-live, already-attaching, or torn-down tab is skipped without side
// effects.
func shouldAttachExistingTerminalTab(tab *TerminalTab) (epoch uint64, ok bool) {
	if tab == nil || tab.State == nil {
		return 0, false
	}
	ts := tab.State
	ts.mu.Lock()
	defer ts.mu.Unlock()
	switch ts.tab {
	case tabDetachedUser, tabRunning, tabAttaching, tabTornDown:
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
					cmds = append(cmds, m.attachToSession(ws, existing.ID, session.Name, "reattach", epoch))
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
				// Discovered but not yet attached: no client PTY exists, and
				// auto-attach (not the user) owns the decision.
				tab: tabDetachedAuto,
			},
		}
		m.tabs.ByWorkspace[wsID] = append(m.tabs.ByWorkspace[wsID], tab)
		if len(m.tabs.ByWorkspace[wsID]) == 1 {
			m.tabs.ActiveByWorkspace[wsID] = 0
		}
		if session.Attach {
			var epoch uint64
			if tab.State != nil {
				withMu(&tab.State.mu, func() {
					if tab.State.beginReattachLocked() {
						epoch = tab.State.reattachEpoch
					}
				})
			}
			if epoch != 0 {
				cmds = append(cmds, m.attachToSession(ws, tabID, session.Name, "reattach", epoch))
			}
		}
	}
	m.refreshTerminalSize()
	return cmds
}
