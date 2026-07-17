package sidebar

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/safego"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
	"github.com/andyrewlee/amux/internal/vterm"
)

// handlePTYOutput buffers incoming PTY data and schedules a flush.
func (m *TerminalModel) handlePTYOutput(msg messages.SidebarPTYOutput) tea.Cmd {
	wsID := msg.WorkspaceID
	tabID := TerminalTabID(msg.TabID)
	tab := m.getTabByID(wsID, tabID)
	if tab == nil || tab.State == nil {
		return nil
	}
	ts := tab.State
	ts.State.AppendOutput(&ts.mu, msg.Data, ptyMaxBufferedBytes, ptyio.OutputHooks{
		SeedForTrim: func() vterm.ParserCarryState {
			seed := vterm.ParserCarryState{}
			ts.mu.Lock()
			if ts.VTerm != nil {
				seed = ts.VTerm.ParserCarryState()
				ts.VTerm.ResetParserState()
			}
			ts.mu.Unlock()
			return seed
		},
		OnOverflowLocked: func(_, retainedStart, prevPendingLen int) {
			if retainedStart > prevPendingLen {
				ts.NoiseTrailing = nil
			}
		},
		LogOverflow: func(droppedTotal int) {
			logging.Warn("Sidebar PTY output overflow for workspace %s tab %s: dropped %d bytes (buffer cap %d)", wsID, tabID, droppedTotal, ptyMaxBufferedBytes)
		},
		DropBytesCounter: "sidebar_pty_drop_bytes",
		DropCounter:      "sidebar_pty_drop",
	})
	// Written under ts.mu because EnforceAttachedTerminalTabLimit reads it
	// from under the same lock.
	now := time.Now()
	ts.mu.Lock()
	ts.LastOutputAt = now
	ts.mu.Unlock()
	if !ts.FlushScheduled {
		ts.FlushScheduled = true
		ts.FlushPendingSince = now
		quiet, _ := m.flushTiming()
		return common.SafeTick(quiet, func(t time.Time) tea.Msg {
			return messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: msg.TabID}
		})
	}
	return nil
}

// handlePTYFlush writes buffered PTY data to the vterm when the quiet period expires.
func (m *TerminalModel) handlePTYFlush(msg messages.SidebarPTYFlush) tea.Cmd {
	wsID := msg.WorkspaceID
	tabID := TerminalTabID(msg.TabID)
	tab := m.getTabByID(wsID, tabID)
	if tab == nil || tab.State == nil {
		return nil
	}
	ts := tab.State
	quiet, maxInterval := m.flushTiming()
	if delay, deferred := ts.State.FlushGate(time.Now(), quiet, maxInterval); deferred {
		return common.SafeTick(delay, func(t time.Time) tea.Msg {
			return messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: msg.TabID}
		})
	}
	if len(ts.PendingOutput) == 0 {
		return nil
	}
	var consumed bool
	var pendingClip []byte
	ts.mu.Lock()
	if ts.VTerm != nil {
		chunk := ts.State.TakeFlushChunkLocked(ptyFlushChunkSize)
		_ = ts.State.WriteFilteredChunkLocked(ts.VTerm.Write, chunk)
		pendingClip = ts.VTerm.TakePendingClipboard()
		consumed = true
	}
	ts.mu.Unlock()
	if clip, ok := common.OSC52ClipboardText(pendingClip); ok {
		safego.Go("sidebar.osc52_clipboard", func() {
			common.CopyToClipboardWithLog(clip, "agent OSC52 (sidebar)")
		})
	}
	if !consumed {
		return nil
	}
	if !ts.State.RearmFlush(time.Now(), nil) {
		return nil
	}
	delay, _ := m.flushTiming()
	if delay < time.Millisecond {
		delay = time.Millisecond
	}
	return common.SafeTick(delay, func(t time.Time) tea.Msg {
		return messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: msg.TabID}
	})
}

// handlePTYStopped handles PTY reader exit, restarting with backoff or marking detached.
func (m *TerminalModel) handlePTYStopped(msg messages.SidebarPTYStopped) tea.Cmd {
	wsID := msg.WorkspaceID
	tabID := TerminalTabID(msg.TabID)
	tab := m.getTabByID(wsID, tabID)
	if tab == nil || tab.State == nil {
		return nil
	}
	ts := tab.State
	termAlive := ts.Terminal != nil && !ts.Terminal.IsClosed()
	ts.mu.Lock()
	if ts.VTerm != nil && len(ts.NoiseTrailing) > 0 {
		trailing := ptyio.DrainKnownPTYNoiseTrailing(&ts.NoiseTrailing)
		flushDone := perf.Time("pty_flush")
		ts.VTerm.Write(trailing)
		flushDone()
		perf.Count("pty_flush_bytes", int64(len(trailing)))
	}
	ts.mu.Unlock()
	m.stopPTYReader(ts)
	ts.mu.Lock()
	shouldRestart, backoff := ts.State.DecidePTYRestartLocked(termAlive, ptyRestartWindow, ptyRestartMax)
	if !shouldRestart {
		ts.Running = false
		// Mark as detached (tmux session may still be alive)
		ts.Detached = true
		ts.UserDetached = false
	}
	ts.mu.Unlock()
	if shouldRestart {
		restartTab := msg.TabID
		restartWt := msg.WorkspaceID
		logging.Warn("Sidebar PTY stopped for workspace %s tab %s; restarting in %s: %v", wsID, tabID, backoff, msg.Err)
		return common.SafeTick(backoff, func(time.Time) tea.Msg {
			return messages.SidebarPTYRestart{WorkspaceID: restartWt, TabID: restartTab}
		})
	}
	if termAlive {
		logging.Warn("Sidebar PTY stopped for workspace %s tab %s; restart limit reached, marking detached: %v", wsID, tabID, msg.Err)
	} else {
		logging.Info("Sidebar PTY stopped for workspace %s tab %s, marking detached: %v", wsID, tabID, msg.Err)
	}
	return nil
}

// handlePTYRestart re-starts the PTY reader after a backoff delay.
func (m *TerminalModel) handlePTYRestart(msg messages.SidebarPTYRestart) tea.Cmd {
	tab := m.getTabByID(msg.WorkspaceID, TerminalTabID(msg.TabID))
	if tab == nil || tab.State == nil {
		return nil
	}
	ts := tab.State
	if ts.Terminal == nil || ts.Terminal.IsClosed() {
		ts.mu.Lock()
		ts.RestartBackoff = 0
		ts.mu.Unlock()
		return nil
	}
	return m.startPTYReader(msg.WorkspaceID, tab.ID)
}
