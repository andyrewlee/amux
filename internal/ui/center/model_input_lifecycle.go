package center

import (
	"fmt"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/vterm"
)

const activityTagThrottle = 1 * time.Second

func (m *Model) userInputActivityTagCmd(tab *Tab) tea.Cmd {
	if tab == nil || tab.isClosed() || !m.isChatTab(tab) {
		return nil
	}
	sessionName := tab.SessionName
	if sessionName == "" && tab.Agent != nil {
		sessionName = tab.Agent.Session
	}
	if sessionName == "" {
		return nil
	}
	now := time.Now()
	if now.Sub(tab.lastInputTagAt) < activityTagThrottle {
		return nil
	}
	tab.lastInputTagAt = now
	opts := m.getTmuxOptions()
	timestamp := now.UnixMilli()
	return func() tea.Msg {
		raw := strconv.FormatInt(timestamp, 10)
		_ = tmux.SetSessionTagValues(sessionName, []tmux.OptionValue{
			{Key: tmux.TagLastInputAt, Value: raw},
			{Key: tmux.TagSessionLeaseAt, Value: raw},
		}, opts)
		return nil
	}
}

// updateLaunchAgent handles messages.LaunchAgent.
func (m *Model) updateLaunchAgent(msg messages.LaunchAgent) (*Model, tea.Cmd) {
	return m, m.createAgentTab(msg.Assistant, msg.Workspace)
}

// updateOpenFileInVim handles messages.OpenFileInVim.
func (m *Model) updateOpenFileInVim(msg messages.OpenFileInVim) (*Model, tea.Cmd) {
	return m, m.createVimTab(msg.Path, msg.Workspace)
}

// updatePtyTabCreateResult handles ptyTabCreateResult.
func (m *Model) updatePtyTabCreateResult(msg ptyTabCreateResult) (*Model, tea.Cmd) {
	return m, m.handlePtyTabCreated(msg)
}

func (m *Model) sessionRestoreLiveSize(captureFullPane bool, captureCols, captureRows int) (int, int) {
	if captureFullPane && captureCols > 0 && captureRows > 0 && (m.width <= 0 || m.height <= 0) {
		return captureCols, captureRows
	}
	tm := m.terminalMetrics()
	cols := tm.Width
	rows := tm.Height
	if cols <= 0 || rows <= 0 {
		return 80, 24
	}
	return cols, rows
}

// updatePtyTabReattachResult handles ptyTabReattachResult.
func (m *Model) updatePtyTabReattachResult(msg ptyTabReattachResult) (*Model, tea.Cmd) {
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab == nil || msg.Agent == nil {
		return m, nil
	}
	captureRows := msg.Rows
	captureCols := msg.Cols
	cols, rows := m.sessionRestoreLiveSize(msg.CaptureFullPane, captureCols, captureRows)
	initialCols, initialRows := common.SessionSnapshotSize(msg.CaptureFullPane, msg.SnapshotCols, msg.SnapshotRows, cols, rows)
	tab.mu.Lock()
	createdTerminal := false
	if tab.Terminal == nil {
		tab.Terminal = vterm.New(initialCols, initialRows)
		createdTerminal = true
	}
	if tab.Terminal != nil {
		// Do not reset parser state when reusing an existing terminal here.
		// pendingOutput may still contain continuation bytes queued under the
		// current parser carry, and reconnect must preserve that continuity until
		// buffered output is explicitly reconciled.
		tab.Terminal.AllowAltScreenScrollback = true
		m.applyTerminalCursorPolicyLocked(tab)
		if msg.CaptureFullPane {
			// The tmux snapshot is now the source of truth for the restored frame.
			// Any preserved local PTY backlog may already be represented there and
			// would duplicate on the next flush if we kept it alive.
			tab.pendingOutput = nil
			common.RestorePaneCapture(
				tab.Terminal,
				msg.ScrollbackCapture,
				msg.PostAttachScrollbackCapture,
				msg.SnapshotCursorX,
				msg.SnapshotCursorY,
				msg.SnapshotHasCursor,
				msg.SnapshotModeState,
				msg.SnapshotCols,
				msg.SnapshotRows,
				cols,
				rows,
			)
		} else if createdTerminal || len(tab.Terminal.Scrollback) == 0 {
			common.RestoreScrollbackCapture(tab.Terminal, msg.ScrollbackCapture, captureCols, captureRows, cols, rows)
		} else if m.width > 0 && m.height > 0 {
			common.ResizeTerminalForSessionRestore(tab.Terminal, cols, rows)
		}
	}
	tab.Agent = msg.Agent
	tab.SessionName = msg.Agent.Session
	tab.Detached = false
	tab.reattachInFlight = false
	tab.Running = true
	resetChatCursorActivityStateLocked(tab)
	tab.parserResetPending = false
	tab.settlePTYBytesLocked(tab.actorQueuedBytes)
	tab.actorQueuedBytes = 0
	tab.actorWritesPending = 0
	tab.actorWriteEpoch++
	tab.clearCatchUpLocked()
	tab.pendingOutputBytes = len(tab.pendingOutput)
	tab.overflowTrimCarry = vterm.ParserCarryState{}
	tab.ptyNoiseTrailing = nil
	tab.actorQueuedNoiseTrailing = tab.actorQueuedNoiseTrailing[:0]
	tab.actorQueuedCarry = tab.Terminal.ParserCarryState()
	tab.bootstrapActivity = true
	tab.bootstrapLastOutputAt = time.Now()
	tab.mu.Unlock()
	tab.resetActivityANSIState()

	if tab.Terminal != nil && msg.Agent.Terminal != nil {
		agentTerm := msg.Agent.Terminal
		workspaceID := msg.WorkspaceID
		tabID := tab.ID
		tab.Terminal.SetResponseWriter(func(data []byte) {
			if len(data) == 0 || agentTerm == nil {
				return
			}
			if err := agentTerm.SendString(string(data)); err != nil {
				logging.Warn("Response write failed for tab %s: %v", tabID, err)
				if m.msgSink != nil {
					m.msgSink(TabInputFailed{TabID: tabID, WorkspaceID: workspaceID, Err: err})
				}
			}
		})
	}

	m.resizePTY(tab, rows, cols)

	cmd := m.startPTYReader(msg.WorkspaceID, tab)
	return m, common.SafeBatch(cmd, func() tea.Msg {
		return messages.TabReattached{WorkspaceID: msg.WorkspaceID, TabID: string(msg.TabID)}
	})
}

// updatePtyTabReattachFailed handles ptyTabReattachFailed.
func (m *Model) updatePtyTabReattachFailed(msg ptyTabReattachFailed) (*Model, tea.Cmd) {
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab == nil {
		return m, nil
	}
	tab.mu.Lock()
	tab.Running = false
	tab.reattachInFlight = false
	// Any stopped reattach clears Detached so the tab shows as stopped.
	if msg.Stopped {
		tab.Detached = false
	}
	tab.mu.Unlock()
	logging.Warn("Reattach failed for tab %s: %v", msg.TabID, msg.Err)
	action := msg.Action
	if action == "" {
		action = "reattach"
	}
	label := "Reattach"
	switch action {
	case "restart":
		label = "Restart"
	case "reattach":
		label = "Reattach"
	}
	return m, common.SafeBatch(func() tea.Msg {
		return messages.TabStateChanged{WorkspaceID: msg.WorkspaceID, TabID: string(msg.TabID)}
	}, func() tea.Msg {
		return messages.Toast{
			Message: fmt.Sprintf("%s failed: %v", label, msg.Err),
			Level:   messages.ToastWarning,
		}
	})
}

// updateTabSessionStatus handles messages.TabSessionStatus.
func (m *Model) updateTabSessionStatus(msg messages.TabSessionStatus) (*Model, tea.Cmd) {
	if msg.Status != "stopped" {
		return m, nil
	}
	tab := m.getTabBySession(msg.WorkspaceID, msg.SessionName)
	if tab == nil {
		return m, nil
	}
	m.stopPTYReader(tab)
	tab.mu.Lock()
	agent := tab.Agent
	tab.Agent = nil
	tab.mu.Unlock()
	if agent != nil {
		_ = m.agentManager.CloseAgent(agent)
	}
	tab.mu.Lock()
	tab.Running = false
	tab.Detached = false
	tab.mu.Unlock()
	tab.resetActivityANSIState()
	return m, common.SafeBatch(func() tea.Msg {
		return messages.TabStateChanged{WorkspaceID: msg.WorkspaceID, TabID: string(tab.ID)}
	})
}

// updateOpenDiff handles messages.OpenDiff.
func (m *Model) updateOpenDiff(msg messages.OpenDiff) (*Model, tea.Cmd) {
	if msg.Change == nil {
		return m, nil
	}
	return m, m.createDiffTab(msg.Change, msg.Mode, msg.Workspace)
}

// updateWorkspaceDeleted handles messages.WorkspaceDeleted.
func (m *Model) updateWorkspaceDeleted(msg messages.WorkspaceDeleted) (*Model, tea.Cmd) {
	m.CleanupWorkspace(msg.Workspace)
	return m, nil
}

// updateTabSelectionResult handles tabSelectionResult.
func (m *Model) updateTabSelectionResult(msg tabSelectionResult) (*Model, tea.Cmd) {
	if msg.clipboard != "" {
		if err := common.CopyToClipboard(msg.clipboard); err != nil {
			logging.Error("Failed to copy to clipboard: %v", err)
		} else {
			logging.Info("Copied %d chars to clipboard", len(msg.clipboard))
		}
	}
	return m, nil
}

// updateSelectionTickRequest handles selectionTickRequest.
func (m *Model) updateSelectionTickRequest(msg selectionTickRequest) (*Model, tea.Cmd) {
	cmd := common.SafeTick(100*time.Millisecond, func(time.Time) tea.Msg {
		return selectionScrollTick{WorkspaceID: msg.workspaceID, TabID: msg.tabID, Gen: msg.gen}
	})
	return m, cmd
}

// updateTabDiffCmd handles tabDiffCmd.
func (m *Model) updateTabDiffCmd(msg tabDiffCmd) (*Model, tea.Cmd) {
	return m, msg.cmd
}
