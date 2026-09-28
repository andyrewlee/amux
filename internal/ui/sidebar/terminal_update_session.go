package sidebar

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
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
	// The result is stamped with the workspace ID computed at dispatch time; a
	// rebind or delete can invalidate that key while the create is in flight.
	// A live bucket or pending flag under the stamped ID means the key is
	// still good; otherwise decide by current-workspace state.
	wsID := msg.WorkspaceID
	if _, known := m.tabs.ByWorkspace[wsID]; !known && !m.pendingCreationActive(wsID) {
		switch cur := m.workspaceID(); {
		case cur == wsID:
			// Manual create (CreateNewTab doesn't set the pending flag) for
			// the workspace still current — file under the stamped key.
		case cur != "" && m.pendingCreationActive(cur):
			// A rebind migrated the pending flag to the new ID — file under
			// the live key so the flag clears and the tab is visible.
			logging.Warn("sidebar terminal create: result stamped with workspace %s but create pending under %s; filing under current", wsID, cur)
			wsID = cur
		default:
			// Workspace deleted mid-flight — drop rather than resurrect a
			// dead bucket with an invisible running terminal.
			logging.Warn("sidebar terminal create: dropping result for stale workspace %s", wsID)
			closeTerminalForSidebar(msg.Terminal, "stale create result")
			return nil
		}
	}
	delete(m.pendingCreation, msg.WorkspaceID) // belt-and-braces: stale flag can't linger

	currentWidth, currentHeight := m.sessionRestoreLiveSize(msg.CaptureFullPane, msg.SnapshotCols, msg.SnapshotRows)
	initialWidth, initialHeight := ptyio.SessionSnapshotSize(msg.CaptureFullPane, msg.SnapshotCols, msg.SnapshotRows, currentWidth, currentHeight)
	ts := m.createTerminalStateForTabWithSizeAndRefresh(
		wsID,
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
				ptyio.RestorePaneCapture(ts.VTerm, msg.SessionRestoreCapture, currentWidth, currentHeight)
				ts.lastWidth = currentWidth
				ts.lastHeight = currentHeight
			} else if len(msg.ScrollbackCapture) > 0 {
				ptyio.RestoreScrollbackCapture(
					ts.VTerm,
					msg.ScrollbackCapture,
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
		if ptyRows, ptyCols, ok := pty.WinsizeFromInts(currentHeight, currentWidth); ok {
			_ = setTerminalSizeFn(msg.Terminal, ptyRows, ptyCols)
		}
	}
	return m.startPTYReader(wsID, msg.TabID)
}

// handleReattachResult applies the result of a terminal reattach operation.
func (m *TerminalModel) handleReattachResult(msg SidebarTerminalReattachResult) tea.Cmd {
	tab, wsID := m.resolveTabForResult(msg.WorkspaceID, msg.TabID, "sidebar attach result")
	if tab == nil || tab.State == nil {
		// The tab was closed or its workspace deleted while the attach was in
		// flight; close the orphaned PTY or its tmux client leaks for the
		// session lifetime (and keeps the session "attached" forever).
		if msg.Terminal != nil {
			closeTerminalForSidebar(msg.Terminal, "reattach target gone")
		}
		return nil
	}
	ts := tab.State
	termWidth, termHeight := m.sessionRestoreLiveSize(msg.CaptureFullPane, msg.SnapshotCols, msg.SnapshotRows)
	ts.mu.Lock()
	if !ts.reattachAttemptCurrentLocked(msg.Epoch) {
		// A newer attempt owns the tab now (or the attempt was invalidated by
		// detach/teardown/sweep). Applying this would overwrite the newer
		// attachment; release the orphaned client instead. If the incoming
		// pointer somehow is the accepted client (a duplicated result), never
		// close it out from under the live terminal.
		current := ts.Terminal
		ts.mu.Unlock()
		logging.Warn("Dropping superseded sidebar attach result for tab %s (epoch %d)", tab.ID, msg.Epoch)
		if msg.Terminal != nil && msg.Terminal != current {
			closeTerminalForSidebar(msg.Terminal, "superseded attach result")
		}
		return nil
	}
	if msg.Terminal == nil {
		// A current-attempt success with no terminal is a failed attach:
		// release the attempt so the tab can be retried instead of wedging,
		// and surface it like any other attach failure.
		ts.Running = false
		ts.finishReattachLocked()
		ts.mu.Unlock()
		logging.Warn("Sidebar attach for tab %s returned no terminal; releasing attempt", tab.ID)
		return func() tea.Msg {
			return messages.Toast{Message: "Reattach failed: no terminal returned", Level: messages.ToastWarning}
		}
	}
	// A distinct client still held from before this attempt (e.g. a terminal
	// left over by a PTY-lost detach) is obsolete the moment the new client
	// is accepted: stop the reader and close it outside the mutex below.
	obsolete := ts.Terminal
	if obsolete == msg.Terminal {
		obsolete = nil
	}
	if ts.VTerm == nil {
		ts.VTerm = vterm.New(termWidth, termHeight)
	}
	if ts.VTerm != nil {
		ts.VTerm.AllowAltScreenScrollback = true
		if msg.CaptureFullPane {
			ptyio.RestorePaneCapture(ts.VTerm, msg.SessionRestoreCapture, termWidth, termHeight)
		} else {
			if len(msg.ScrollbackCapture) > 0 && len(ts.VTerm.Scrollback) == 0 {
				ptyio.RestoreScrollbackCapture(
					ts.VTerm,
					msg.ScrollbackCapture,
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
	ts.finishReattachLocked()
	ts.SessionName = msg.SessionName
	ts.PendingOutput = nil
	ts.NoiseTrailing = nil
	ts.OverflowTrimCarry = vterm.ParserCarryState{}
	ts.lastWidth = termWidth
	ts.lastHeight = termHeight
	ts.mu.Unlock()
	if obsolete != nil {
		// Stop the reader before closing the superseded client: the reader
		// resolves ts.Terminal live, so the new reader started below must be
		// the only one touching the accepted client.
		m.stopPTYReader(ts)
		closeTerminalForSidebar(obsolete, "replaced by newer attach")
	}
	t := msg.Terminal
	ts.VTerm.SetResponseWriter(func(data []byte) {
		if t != nil {
			_, _ = t.Write(data)
		}
	})
	if ptyRows, ptyCols, ok := pty.WinsizeFromInts(termHeight, termWidth); ok {
		_ = setTerminalSizeFn(t, ptyRows, ptyCols)
	}
	return m.startPTYReader(wsID, tab.ID)
}

// handleReattachFailed handles a failed reattach attempt.
func (m *TerminalModel) handleReattachFailed(msg SidebarTerminalReattachFailed) tea.Cmd {
	tab, _ := m.resolveTabForResult(msg.WorkspaceID, msg.TabID, "sidebar attach failure")
	if tab != nil && tab.State != nil {
		ts := tab.State
		ts.mu.Lock()
		if !ts.reattachAttemptCurrentLocked(msg.Epoch) {
			// A stale attempt's failure says nothing about the newer one:
			// no state change and no misleading failure toast.
			ts.mu.Unlock()
			return nil
		}
		ts.Running = false
		ts.finishReattachLocked()
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
	if _, known := m.tabs.ByWorkspace[msg.WorkspaceID]; !known {
		// The stamped key is dead (rebind or delete mid-flight); the pending
		// flag may have been migrated to the current workspace ID — clear it
		// too or auto-create wedges forever.
		if cur := m.workspaceID(); cur != "" {
			delete(m.pendingCreation, cur)
		}
	}
	return common.ReportError("creating sidebar terminal", msg.Err, "")
}

// handleWorkspaceDeleted tears down all terminal tabs for a deleted workspace.
func (m *TerminalModel) handleWorkspaceDeleted(msg messages.WorkspaceDeleted) tea.Cmd {
	m.teardownWorkspaceTabs(msg.Workspace, msg.WorkspaceIDs, "workspace deletion")
	return nil
}

// handleWorkspaceShelved tears down all terminal tabs for a shelved workspace.
// Shelve removes the worktree and kills its tmux sessions just like delete, so
// keeping tabs keyed to it would resurface dead-session state on restore.
func (m *TerminalModel) handleWorkspaceShelved(msg messages.WorkspaceShelved) tea.Cmd {
	m.teardownWorkspaceTabs(msg.Workspace, msg.WorkspaceIDs, "workspace shelve")
	return nil
}

// teardownWorkspaceTabs drops every tab keyed to the workspace. stampedIDs is
// the pre-removal identity set from the message; when empty it falls back to
// both computed forms — ws.ID() drifts once the worktree is gone, while
// MetadataID() stays stable, and tabs may have been filed under either.
func (m *TerminalModel) teardownWorkspaceTabs(ws *data.Workspace, stampedIDs []string, reason string) {
	if ws == nil {
		return
	}
	wsIDs := stampedIDs
	if len(wsIDs) == 0 {
		wsIDs = data.WorkspaceIdentityStrings(ws)
	}
	for _, wsID := range wsIDs {
		tabs := m.tabs.ByWorkspace[wsID]
		for _, tab := range tabs {
			m.teardownTabState(tab.State, reason)
		}
		m.tabs.DeleteWorkspace(wsID)
		delete(m.pendingCreation, wsID)
		delete(m.lastActiveAt, wsID)
	}
}
