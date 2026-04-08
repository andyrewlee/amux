package sidebar

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/vterm"
)

func (m *TerminalModel) sessionRestoreLiveSize(captureFullPane bool, snapshotCols, snapshotRows int) (int, int) {
	if captureFullPane && snapshotCols > 0 && snapshotRows > 0 && (m.width <= 0 || m.height <= 0) {
		return snapshotCols, snapshotRows
	}
	return m.terminalContentSize()
}

// handleTerminalCreated wires up a newly created terminal and its scrollback.
func (m *TerminalModel) handleTerminalCreated(msg SidebarTerminalCreated) tea.Cmd {
	currentWidth, currentHeight := m.sessionRestoreLiveSize(msg.CaptureFullPane, msg.SnapshotCols, msg.SnapshotRows)
	initialWidth, initialHeight := common.SessionSnapshotSize(msg.CaptureFullPane, msg.SnapshotCols, msg.SnapshotRows, currentWidth, currentHeight)
	ts := m.createTerminalStateForTabWithSizeAndRefresh(
		msg.WorkspaceID,
		msg.TabID,
		msg.Terminal,
		msg.SessionName,
		initialWidth,
		initialHeight,
		!msg.CaptureFullPane,
		!msg.CaptureFullPane,
	)
	currentWidth, currentHeight = m.sessionRestoreLiveSize(msg.CaptureFullPane, msg.SnapshotCols, msg.SnapshotRows)
	if ts != nil {
		ts.mu.Lock()
		if ts.VTerm != nil {
			if msg.CaptureFullPane {
				common.RestorePaneCapture(
					ts.VTerm,
					msg.Scrollback,
					msg.PostAttachScrollback,
					msg.SnapshotCursorX,
					msg.SnapshotCursorY,
					msg.SnapshotHasCursor,
					msg.SnapshotModeState,
					msg.SnapshotCols,
					msg.SnapshotRows,
					currentWidth,
					currentHeight,
				)
				ts.lastWidth = currentWidth
				ts.lastHeight = currentHeight
			} else if len(msg.Scrollback) > 0 {
				common.RestoreScrollbackCapture(
					ts.VTerm,
					msg.Scrollback,
					msg.CaptureCols,
					msg.CaptureRows,
					currentWidth,
					currentHeight,
				)
			}
		}
		ts.mu.Unlock()
	}
	if msg.CaptureFullPane {
		m.refreshTerminalSize()
	}
	if msg.Terminal != nil && (initialWidth != currentWidth || initialHeight != currentHeight) {
		_ = setTerminalSizeFn(msg.Terminal, uint16(currentHeight), uint16(currentWidth))
	}
	return m.startPTYReader(msg.WorkspaceID, msg.TabID)
}

// handleReattachResult applies the result of a terminal reattach operation.
func (m *TerminalModel) handleReattachResult(msg SidebarTerminalReattachResult) tea.Cmd {
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab == nil || tab.State == nil {
		return nil
	}
	ts := tab.State
	termWidth, termHeight := m.sessionRestoreLiveSize(msg.CaptureFullPane, msg.SnapshotCols, msg.SnapshotRows)
	ts.mu.Lock()
	if ts.VTerm == nil {
		ts.VTerm = vterm.New(termWidth, termHeight)
	}
	if ts.VTerm != nil {
		ts.VTerm.AllowAltScreenScrollback = true
		if msg.CaptureFullPane {
			common.RestorePaneCapture(
				ts.VTerm,
				msg.Scrollback,
				msg.PostAttachScrollback,
				msg.SnapshotCursorX,
				msg.SnapshotCursorY,
				msg.SnapshotHasCursor,
				msg.SnapshotModeState,
				msg.SnapshotCols,
				msg.SnapshotRows,
				termWidth,
				termHeight,
			)
		} else {
			if len(msg.Scrollback) > 0 && len(ts.VTerm.Scrollback) == 0 {
				common.RestoreScrollbackCapture(
					ts.VTerm,
					msg.Scrollback,
					msg.CaptureCols,
					msg.CaptureRows,
					termWidth,
					termHeight,
				)
			} else if ts.VTerm.Width != termWidth || ts.VTerm.Height != termHeight {
				ts.VTerm.Resize(termWidth, termHeight)
			}
		}
	}
	ts.Terminal = msg.Terminal
	ts.Running = true
	ts.Detached = false
	ts.UserDetached = false
	ts.reattachInFlight = false
	ts.SessionName = msg.SessionName
	ts.pendingOutput = nil
	ts.ptyNoiseTrailing = nil
	ts.overflowTrimCarry = vterm.ParserCarryState{}
	ts.lastWidth = termWidth
	ts.lastHeight = termHeight
	ts.mu.Unlock()
	if msg.Terminal != nil {
		t := msg.Terminal
		ts.VTerm.SetResponseWriter(func(data []byte) {
			if t != nil {
				_, _ = t.Write(data)
			}
		})
		_ = setTerminalSizeFn(msg.Terminal, uint16(termHeight), uint16(termWidth))
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
