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
	"github.com/andyrewlee/amux/internal/ui/ptyio"
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
	opts := m.tmuxOpts
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
	tab, wsID := m.resolveTabForResult(msg.WorkspaceID, msg.TabID, "reattach result")
	if tab == nil {
		// The tab was closed (or its workspace deleted) while the reattach was
		// in flight: release the freshly created agent/PTY so it does not leak.
		// Logged because this also silently discards a successful attach, which
		// is indistinguishable from a hang if it ever happens to a live tab.
		logging.Info("Discarding reattach result for unknown tab %s (workspace %s)", msg.TabID, msg.WorkspaceID)
		if msg.Agent != nil {
			_ = m.agentManager.CloseAgent(msg.Agent)
		}
		return m, nil
	}
	if superseded, _ := m.reattachSuperseded(tab, msg.Epoch); superseded {
		// A newer reattach has started since this one was dispatched — possible
		// once the stall sweep releases a lock. Applying this result would
		// overwrite the newer attempt's agent with a stale one and leak the tmux
		// client it replaced, so drop it and release what it created.
		logging.Warn("Dropping superseded reattach result for tab %s (epoch %d)", msg.TabID, msg.Epoch)
		if msg.Agent != nil {
			_ = m.agentManager.CloseAgent(msg.Agent)
		}
		return m, nil
	}
	if msg.Agent == nil {
		// A result with no agent cannot attach anything, but the reattach lock
		// is still held: release it so the tab reads as detached and the user
		// can retry instead of being stuck in REATTACHING forever.
		tab.mu.Lock()
		tab.markReattachFailedLocked(false)
		tab.mu.Unlock()
		logging.Warn("Reattach for tab %s returned no agent; releasing reattach lock", msg.TabID)
		return m, func() tea.Msg {
			return messages.TabStateChanged{WorkspaceID: wsID, TabID: string(msg.TabID)}
		}
	}
	// Reject a result for a tab that was explicitly detached while this reattach
	// was in flight: detachTab clears reattachInFlight and sets Detached, so a
	// live reattach/restart (reattachInFlight=true) still applies, but a
	// user-detached tab is not silently resurrected. Release the freshly created
	// agent/PTY so it does not leak.
	tab.mu.Lock()
	staleDetached := tab.Detached && !tab.reattachInFlight
	tab.mu.Unlock()
	if staleDetached {
		_ = m.agentManager.CloseAgent(msg.Agent)
		return m, nil
	}
	captureRows := msg.Rows
	captureCols := msg.Cols
	cols, rows := m.sessionRestoreLiveSize(msg.CaptureFullPane, captureCols, captureRows)
	initialCols, initialRows := ptyio.SessionSnapshotSize(msg.CaptureFullPane, msg.SnapshotCols, msg.SnapshotRows, cols, rows)
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
			tab.PendingOutput = nil
			ptyio.RestorePaneCapture(tab.Terminal, msg.SessionRestoreCapture, cols, rows)
		} else if createdTerminal || len(tab.Terminal.Scrollback) == 0 {
			ptyio.RestoreScrollbackCapture(tab.Terminal, msg.ScrollbackCapture, captureCols, captureRows, cols, rows)
		} else if m.width > 0 && m.height > 0 {
			ptyio.ResizeTerminalForSessionRestore(tab.Terminal, cols, rows)
		}
	}
	tab.Agent = msg.Agent
	tab.SessionName = msg.Agent.Session
	tab.markAttachedLocked()
	resetChatCursorActivityStateLocked(tab)
	tab.resetActorWriteStateLocked()
	tab.bootstrapActivity = true
	tab.bootstrapLastOutputAt = time.Now()
	tab.mu.Unlock()
	tab.resetActivityANSIState()

	if tab.Terminal != nil && msg.Agent.Terminal != nil {
		agentTerm := msg.Agent.Terminal
		workspaceID := wsID
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

	cmd := m.startPTYReader(wsID, tab)
	return m, common.SafeBatch(cmd, func() tea.Msg {
		return messages.TabReattached{WorkspaceID: wsID, TabID: string(msg.TabID)}
	})
}

// updatePtyTabReattachFailed handles ptyTabReattachFailed.
func (m *Model) updatePtyTabReattachFailed(msg ptyTabReattachFailed) (*Model, tea.Cmd) {
	tab, wsID := m.resolveTabForResult(msg.WorkspaceID, msg.TabID, "reattach failure")
	if tab == nil {
		return m, nil
	}
	if superseded, _ := m.reattachSuperseded(tab, msg.Epoch); superseded {
		// A newer attempt owns the tab now; this one's failure says nothing
		// about it and must not release its lock or toast over it.
		return m, nil
	}
	tab.mu.Lock()
	// A stopped reattach also clears Detached so the tab shows as stopped.
	tab.markReattachFailedLocked(msg.Stopped)
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
		return messages.TabStateChanged{WorkspaceID: wsID, TabID: string(msg.TabID)}
	}, func() tea.Msg {
		return messages.Toast{
			Message: fmt.Sprintf("%s failed: %v", label, msg.Err),
			Level:   messages.ToastWarning,
		}
	})
}

// reattachSuperseded reports whether a reattach outcome belongs to an attempt
// that a newer one has already replaced, along with the tab's current epoch.
//
// Epoch 0 means the outcome predates epoch tracking (or came from a tab that
// never took the lock through beginReattachLocked); such an outcome is applied
// as before rather than dropped, so the guard can only ever reject a message it
// can positively identify as stale.
func (m *Model) reattachSuperseded(tab *Tab, epoch uint64) (bool, uint64) {
	tab.mu.Lock()
	current := tab.reattachEpochLocked()
	tab.mu.Unlock()
	if epoch == 0 {
		return false, current
	}
	return epoch != current, current
}

// SweepStalledReattaches releases reattach locks whose outcome never arrived.
//
// The reattach lock is normally released by ptyTabReattachResult or
// ptyTabReattachFailed. If such a message is dropped, misrouted, or never
// produced, the tab is pinned in the reattaching state forever: the badge never
// changes and every retry — keypress or automatic — no-ops behind the lock it
// is waiting on. This sweep is the backstop for all of those causes at once,
// including ones added later, which is why it is a periodic scan of state
// rather than a timer armed by each reattach path.
//
// It is not a cancellation: the reattach goroutine is untouched and a late
// outcome still applies normally. All the sweep does is stop the tab from
// claiming to be reattaching, so the user can act on it.
func (m *Model) SweepStalledReattaches() tea.Cmd {
	now := time.Now()
	var cmds []tea.Cmd
	var stalled int
	for wsID, tabs := range m.tabs.ByWorkspace {
		for _, tab := range tabs {
			if tab == nil || tab.isClosed() {
				continue
			}
			tab.mu.Lock()
			// A running tab holds no meaningful lock even if the flag lingers,
			// and a zero stamp means the flag was set outside beginReattachLocked
			// — stamp it now rather than releasing something never timed.
			stuck := false
			switch {
			case !tab.reattachInFlight || tab.Running:
			case tab.reattachStartedAt.IsZero():
				tab.reattachStartedAt = now
			case now.Sub(tab.reattachStartedAt) > ptyio.ReattachStallTimeout:
				tab.markReattachFailedLocked(false)
				stuck = true
			}
			tabID := tab.ID
			tab.mu.Unlock()
			if !stuck {
				continue
			}
			stalled++
			logging.Warn("Reattach for tab %s produced no outcome within %s; releasing reattach lock", tabID, ptyio.ReattachStallTimeout)
			cmds = append(cmds, func() tea.Msg {
				return messages.TabStateChanged{WorkspaceID: wsID, TabID: string(tabID)}
			})
		}
	}
	if stalled == 0 {
		return nil
	}
	cmds = append(cmds, func() tea.Msg {
		return messages.Toast{
			// Same spelling the help bar uses for this binding.
			Message: "Reattach timed out; press C-Spc t r to retry",
			Level:   messages.ToastWarning,
		}
	})
	return common.SafeBatch(cmds...)
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
	tab.markStoppedLocked()
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
	common.CopyToClipboardWithLog(msg.clipboard, "clipboard")
	return m, nil
}

// updateSelectionTickRequest handles selectionTickRequest.
func (m *Model) updateSelectionTickRequest(msg selectionTickRequest) (*Model, tea.Cmd) {
	cmd := common.SafeTick(100*time.Millisecond, func(time.Time) tea.Msg {
		return selectionScrollTick{WorkspaceID: msg.workspaceID, TabID: msg.tabID, Gen: msg.gen, Seq: msg.seq}
	})
	return m, cmd
}

// updateTabDiffCmd handles tabDiffCmd.
func (m *Model) updateTabDiffCmd(msg tabDiffCmd) (*Model, tea.Cmd) {
	return m, msg.cmd
}
