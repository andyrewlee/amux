package center

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/vterm"
)

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

// updatePtyTabReattachResult handles ptyTabReattachResult.
func (m *Model) updatePtyTabReattachResult(msg ptyTabReattachResult) (*Model, tea.Cmd) {
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab == nil || msg.Agent == nil {
		return m, nil
	}
	rows := msg.Rows
	cols := msg.Cols
	if rows <= 0 || cols <= 0 {
		tm := m.terminalMetrics()
		rows = tm.Height
		cols = tm.Width
	}
	tab.mu.Lock()
	if tab.Terminal == nil {
		tab.Terminal = vterm.New(cols, rows)
	}
	tab.Agent = msg.Agent
	tab.SessionName = msg.Agent.Session
	tab.Detached = false
	tab.Running = true
	tab.monitorDirty = true
	tab.mu.Unlock()

	if tab.Terminal != nil && msg.Agent.Terminal != nil {
		tab.Terminal.SetResponseWriter(func(data []byte) {
			if len(data) == 0 {
				return
			}
			// Look up current agent through tab to avoid stale reference
			tab.mu.Lock()
			agent := tab.Agent
			tab.mu.Unlock()
			if agent == nil || agent.Terminal == nil {
				return
			}
			if err := agent.Terminal.SendString(string(data)); err != nil {
				logging.Warn("Response write failed for tab %s: %v", tab.ID, err)
				tab.mu.Lock()
				tab.Running = false
				tab.Detached = true
				tab.mu.Unlock()
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
	return m, common.SafeBatch(func() tea.Msg {
		return messages.TabStateChanged{WorkspaceID: msg.WorkspaceID, TabID: string(tab.ID)}
	})
}

// updateTabActorReady handles tabActorReady.
func (m *Model) updateTabActorReady(_ tabActorReady) (*Model, tea.Cmd) {
	m.setTabActorReady()
	m.noteTabActorHeartbeat()
	return m, nil
}

// updateTabActorHeartbeat handles tabActorHeartbeat.
func (m *Model) updateTabActorHeartbeat(_ tabActorHeartbeat) (*Model, tea.Cmd) {
	m.noteTabActorHeartbeat()
	return m, nil
}

// updateMonitorSnapshotTick handles monitorSnapshotTick.
func (m *Model) updateMonitorSnapshotTick(msg monitorSnapshotTick) (*Model, tea.Cmd) {
	return m, m.handleMonitorSnapshotTick(msg)
}

// updateMonitorSnapshotResult handles monitorSnapshotResult.
func (m *Model) updateMonitorSnapshotResult(msg monitorSnapshotResult) (*Model, tea.Cmd) {
	m.applyMonitorSnapshotResult(msg.snapshots)
	return m, nil
}

// updateOpenDiff handles messages.OpenDiff.
func (m *Model) updateOpenDiff(msg messages.OpenDiff) (*Model, tea.Cmd) {
	// Check if new-style Change is provided, otherwise convert from legacy fields
	if msg.Change != nil {
		return m, m.createDiffTab(msg.Change, msg.Mode, msg.Workspace)
	}
	// Legacy path: convert File/StatusCode to Change
	change := &git.Change{
		Path: msg.File,
	}
	mode := git.DiffModeUnstaged
	if msg.StatusCode == "??" {
		change.Kind = git.ChangeUntracked
	} else if len(msg.StatusCode) >= 1 && msg.StatusCode[0] != ' ' {
		// Staged change
		mode = git.DiffModeStaged
		switch msg.StatusCode[0] {
		case 'A':
			change.Kind = git.ChangeAdded
		case 'D':
			change.Kind = git.ChangeDeleted
		case 'M':
			change.Kind = git.ChangeModified
		case 'R':
			change.Kind = git.ChangeRenamed
		}
		change.Staged = true
	} else {
		// Unstaged change
		if len(msg.StatusCode) >= 2 {
			switch msg.StatusCode[1] {
			case 'A':
				change.Kind = git.ChangeAdded
			case 'D':
				change.Kind = git.ChangeDeleted
			case 'M':
				change.Kind = git.ChangeModified
			}
		}
	}
	return m, m.createDiffTab(change, mode, msg.Workspace)
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

// updatePTYOutput handles PTYOutput.
func (m *Model) updatePTYOutput(msg PTYOutput) tea.Cmd {
	var cmds []tea.Cmd
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab != nil && !tab.isClosed() {
		m.tracePTYOutput(tab, msg.Data)
		tab.pendingOutput = append(tab.pendingOutput, msg.Data...)
		if len(tab.pendingOutput) > ptyMaxBufferedBytes {
			overflow := len(tab.pendingOutput) - ptyMaxBufferedBytes
			perf.Count("pty_output_drop_bytes", int64(overflow))
			tab.pendingOutput = append([]byte(nil), tab.pendingOutput[overflow:]...)
		}
		perf.Count("pty_output_bytes", int64(len(msg.Data)))
		tab.lastOutputAt = time.Now()
		if !tab.flushScheduled {
			tab.flushScheduled = true
			tab.flushPendingSince = tab.lastOutputAt
			quiet, _ := m.flushTiming(tab, m.isActiveTab(msg.WorkspaceID, msg.TabID))
			tabID := msg.TabID // Capture for closure
			cmds = append(cmds, common.SafeTick(quiet, func(t time.Time) tea.Msg {
				return PTYFlush{WorkspaceID: msg.WorkspaceID, TabID: tabID}
			}))
		}
	}
	return common.SafeBatch(cmds...)
}

// updatePTYFlush handles PTYFlush.
func (m *Model) updatePTYFlush(msg PTYFlush) tea.Cmd {
	var cmds []tea.Cmd
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab != nil && !tab.isClosed() {
		now := time.Now()
		quietFor := now.Sub(tab.lastOutputAt)
		pendingFor := time.Duration(0)
		if !tab.flushPendingSince.IsZero() {
			pendingFor = now.Sub(tab.flushPendingSince)
		}
		quiet, maxInterval := m.flushTiming(tab, m.isActiveTab(msg.WorkspaceID, msg.TabID))
		if quietFor < quiet && pendingFor < maxInterval {
			delay := quiet - quietFor
			if delay < time.Millisecond {
				delay = time.Millisecond
			}
			tabID := msg.TabID
			tab.flushScheduled = true
			cmds = append(cmds, common.SafeTick(delay, func(t time.Time) tea.Msg {
				return PTYFlush{WorkspaceID: msg.WorkspaceID, TabID: tabID}
			}))
			return common.SafeBatch(cmds...)
		}

		tab.flushScheduled = false
		tab.flushPendingSince = time.Time{}
		if len(tab.pendingOutput) > 0 {
			var chunk []byte
			writeOutput := false
			tab.mu.Lock()
			if tab.Terminal != nil {
				chunkSize := len(tab.pendingOutput)
				if chunkSize > ptyFlushChunkSize {
					chunkSize = ptyFlushChunkSize
				}
				chunk = append(chunk, tab.pendingOutput[:chunkSize]...)
				copy(tab.pendingOutput, tab.pendingOutput[chunkSize:])
				tab.pendingOutput = tab.pendingOutput[:len(tab.pendingOutput)-chunkSize]
				writeOutput = true
			}
			tab.mu.Unlock()
			if writeOutput && len(chunk) > 0 {
				if m.isTabActorReady() {
					if !m.sendTabEvent(tabEvent{
						tab:         tab,
						workspaceID: msg.WorkspaceID,
						tabID:       msg.TabID,
						kind:        tabEventWriteOutput,
						output:      chunk,
					}) {
						tab.mu.Lock()
						if tab.Terminal != nil {
							flushDone := perf.Time("pty_flush")
							tab.Terminal.Write(chunk)
							flushDone()
							perf.Count("pty_flush_bytes", int64(len(chunk)))
							tab.monitorDirty = true
						}
						tab.mu.Unlock()
					}
				} else {
					tab.mu.Lock()
					if tab.Terminal != nil {
						flushDone := perf.Time("pty_flush")
						tab.Terminal.Write(chunk)
						flushDone()
						perf.Count("pty_flush_bytes", int64(len(chunk)))
						tab.monitorDirty = true
					}
					tab.mu.Unlock()
				}
			}
			if len(tab.pendingOutput) == 0 {
				tab.pendingOutput = tab.pendingOutput[:0]
			} else {
				tab.flushScheduled = true
				tab.flushPendingSince = time.Now()
				tabID := msg.TabID
				cmds = append(cmds, common.SafeTick(time.Millisecond, func(t time.Time) tea.Msg {
					return PTYFlush{WorkspaceID: msg.WorkspaceID, TabID: tabID}
				}))
			}
		}
	}
	return common.SafeBatch(cmds...)
}

// updatePTYStopped handles PTYStopped.
func (m *Model) updatePTYStopped(msg PTYStopped) tea.Cmd {
	var cmds []tea.Cmd
	// Terminal closed - mark tab as not running, but keep it visible
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab != nil {
		termAlive := tab.Agent != nil && tab.Agent.Terminal != nil && !tab.Agent.Terminal.IsClosed()
		m.stopPTYReader(tab)
		if termAlive {
			shouldRestart := true
			var backoff time.Duration
			tab.mu.Lock()
			if tab.ptyRestartSince.IsZero() || time.Since(tab.ptyRestartSince) > ptyRestartWindow {
				tab.ptyRestartSince = time.Now()
				tab.ptyRestartCount = 0
			}
			tab.ptyRestartCount++
			if tab.ptyRestartCount > ptyRestartMax {
				shouldRestart = false
				tab.Running = false
				// Mark as detached (tmux session may still be alive)
				tab.Detached = true
				tab.ptyRestartBackoff = 0
			} else {
				backoff = tab.ptyRestartBackoff
				if backoff <= 0 {
					backoff = 200 * time.Millisecond
				} else {
					backoff *= 2
					if backoff > 5*time.Second {
						backoff = 5 * time.Second
					}
				}
				tab.ptyRestartBackoff = backoff
			}
			tab.mu.Unlock()
			if shouldRestart {
				tabID := msg.TabID
				wtID := msg.WorkspaceID
				cmds = append(cmds, common.SafeTick(backoff, func(time.Time) tea.Msg {
					return PTYRestart{WorkspaceID: wtID, TabID: tabID}
				}))
				logging.Warn("PTY stopped for tab %s; restarting in %s: %v", msg.TabID, backoff, msg.Err)
			} else {
				logging.Warn("PTY stopped for tab %s; restart limit reached, marking detached: %v", msg.TabID, msg.Err)
				cmds = append(cmds, func() tea.Msg {
					return messages.TabStateChanged{WorkspaceID: msg.WorkspaceID, TabID: string(msg.TabID)}
				})
			}
		} else {
			tab.mu.Lock()
			tab.Running = false
			// Mark as detached - tmux session may still be alive, sync will confirm
			tab.Detached = true
			tab.ptyRestartBackoff = 0
			tab.ptyRestartCount = 0
			tab.ptyRestartSince = time.Time{}
			tab.mu.Unlock()
			logging.Info("PTY stopped for tab %s, marking detached: %v", msg.TabID, msg.Err)
			cmds = append(cmds, func() tea.Msg {
				return messages.TabStateChanged{WorkspaceID: msg.WorkspaceID, TabID: string(msg.TabID)}
			})
		}
	}
	return common.SafeBatch(cmds...)
}

// updatePTYRestart handles PTYRestart.
func (m *Model) updatePTYRestart(msg PTYRestart) tea.Cmd {
	var cmds []tea.Cmd
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab == nil {
		return nil
	}
	if tab.Agent == nil || tab.Agent.Terminal == nil || tab.Agent.Terminal.IsClosed() {
		tab.mu.Lock()
		tab.ptyRestartBackoff = 0
		tab.mu.Unlock()
		return nil
	}
	if cmd := m.startPTYReader(msg.WorkspaceID, tab); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return common.SafeBatch(cmds...)
}

// updateSelectionScrollTick handles selectionScrollTick.
func (m *Model) updateSelectionScrollTick(msg selectionScrollTick) tea.Cmd {
	var cmds []tea.Cmd
	if m.isTabActorReady() {
		tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
		if tab == nil {
			return nil
		}
		if m.sendTabEvent(tabEvent{
			tab:         tab,
			workspaceID: msg.WorkspaceID,
			tabID:       msg.TabID,
			kind:        tabEventSelectionScrollTick,
			gen:         msg.Gen,
		}) {
			return nil
		}
	}
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab == nil {
		return nil
	}
	tab.mu.Lock()
	if !tab.Selection.Active || tab.selectionGen != msg.Gen || tab.Terminal == nil || tab.selectionScrollDir == 0 || !tab.selectionScrollActive {
		tab.selectionScrollActive = false
		tab.mu.Unlock()
		return nil
	}
	// Nudge selection to keep scrollback advancing while dragging.
	tab.Terminal.ScrollView(tab.selectionScrollDir)
	tab.monitorDirty = true
	tab.mu.Unlock()
	tabID := msg.TabID
	wtID := msg.WorkspaceID
	cmds = append(cmds, common.SafeTick(100*time.Millisecond, func(time.Time) tea.Msg {
		return selectionScrollTick{WorkspaceID: wtID, TabID: tabID, Gen: msg.Gen}
	}))
	return common.SafeBatch(cmds...)
}
