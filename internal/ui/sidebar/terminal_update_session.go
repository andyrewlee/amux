package sidebar

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/vterm"
)

// handleTerminalCreated wires up a newly created terminal and its scrollback.
func (m *TerminalModel) handleTerminalCreated(msg SidebarTerminalCreated) tea.Cmd {
	cmd := m.HandleTerminalCreated(msg.WorkspaceID, msg.TabID, msg.Terminal, msg.SessionName)
	if len(msg.Scrollback) > 0 {
		tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
		if tab != nil && tab.State != nil {
			ts := tab.State
			ts.mu.Lock()
			if ts.VTerm != nil {
				ts.VTerm.PrependScrollback(msg.Scrollback)
			}
			ts.mu.Unlock()
		}
	}
	return cmd
}

// handleReattachResult applies the result of a terminal reattach operation.
func (m *TerminalModel) handleReattachResult(msg SidebarTerminalReattachResult) tea.Cmd {
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab == nil || tab.State == nil {
		return nil
	}
	ts := tab.State
	termWidth, termHeight := m.terminalContentSize()
	ts.mu.Lock()
	if ts.VTerm == nil {
		ts.VTerm = vterm.New(termWidth, termHeight)
	}
	if ts.VTerm != nil {
		ts.VTerm.AllowAltScreenScrollback = true
		if len(msg.Scrollback) > 0 && len(ts.VTerm.Scrollback) == 0 {
			ts.VTerm.PrependScrollback(msg.Scrollback)
		}
	}
	ts.Terminal = msg.Terminal
	ts.Running = true
	ts.Detached = false
	ts.UserDetached = false
	ts.reattachInFlight = false
	ts.SessionName = msg.SessionName
	ts.pendingOutput = nil
	ts.mu.Unlock()
	if msg.Terminal != nil {
		t := msg.Terminal
		ts.VTerm.SetResponseWriter(func(data []byte) {
			if t != nil {
				_, _ = t.Write(data)
			}
		})
		_ = msg.Terminal.SetSize(uint16(termHeight), uint16(termWidth))
	}
	return m.startPTYReader(msg.WorkspaceID, tab.ID)
}

// handleReattachFailed handles a failed reattach attempt.
func (m *TerminalModel) handleReattachFailed(msg SidebarTerminalReattachFailed) tea.Cmd {
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab != nil && tab.State != nil {
		ts := tab.State
		ts.mu.Lock()
		ts.Running = false
		ts.reattachInFlight = false
		if msg.Stopped {
			ts.Detached = false
		}
		ts.mu.Unlock()
	}
	action := msg.Action
	if action == "" {
		action = "reattach"
	}
	label := "Reattach"
	if action == "restart" {
		label = "Restart"
	}
	return func() tea.Msg {
		return messages.Toast{Message: fmt.Sprintf("%s failed: %v", label, msg.Err), Level: messages.ToastWarning}
	}
}

// handleCreateFailed clears the pending-creation flag so the user can retry.
func (m *TerminalModel) handleCreateFailed(msg SidebarTerminalCreateFailed) tea.Cmd {
	delete(m.pendingCreation, msg.WorkspaceID)
	return common.ReportError("creating sidebar terminal", msg.Err, "")
}

// handleWorkspaceDeleted tears down all terminal tabs for a deleted workspace.
func (m *TerminalModel) handleWorkspaceDeleted(msg messages.WorkspaceDeleted) tea.Cmd {
	if msg.Workspace == nil {
		return nil
	}
	wsID := string(msg.Workspace.ID())
	tabs := m.tabsByWorkspace[wsID]
	for _, tab := range tabs {
		if tab.State != nil {
			m.stopPTYReader(tab.State)
			tab.State.mu.Lock()
			if tab.State.Terminal != nil {
				tab.State.Terminal.Close()
			}
			tab.State.Running = false
			tab.State.ptyRestartBackoff = 0
			tab.State.mu.Unlock()
		}
	}
	delete(m.tabsByWorkspace, wsID)
	delete(m.activeTabByWorkspace, wsID)
	delete(m.pendingCreation, wsID)
	return nil
}
