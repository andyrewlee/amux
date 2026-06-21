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
	data := msg.Data
	ts.mu.Lock()
	data, _ = ts.State.ConsumeOverflowCarryLocked(data)
	ts.mu.Unlock()
	prevPendingLen := len(ts.PendingOutput)
	ts.PendingOutput = append(ts.PendingOutput, data...)
	if len(ts.PendingOutput) > ptyMaxBufferedBytes {
		overflow := len(ts.PendingOutput) - ptyMaxBufferedBytes
		perf.Count("sidebar_pty_drop_bytes", int64(overflow))
		perf.Count("sidebar_pty_drop", 1)
		seed := vterm.ParserCarryState{}
		ts.mu.Lock()
		if ts.VTerm != nil {
			seed = ts.VTerm.ParserCarryState()
			ts.VTerm.ResetParserState()
		}
		ts.mu.Unlock()
		retained, overflowCarry, retainedStart := ptyio.TrimOverflow(ts.PendingOutput, ptyMaxBufferedBytes, seed)
		ts.PendingOutput = retained
		ts.mu.Lock()
		ts.OverflowTrimCarry = overflowCarry
		if retainedStart > prevPendingLen {
			ts.NoiseTrailing = nil
		}
		overflowLogNow, overflowDroppedTotal := ts.NoteOverflowDropLocked(retainedStart)
		ts.mu.Unlock()
		if overflowLogNow {
			logging.Warn("Sidebar PTY output overflow for workspace %s tab %s: dropped %d bytes (buffer cap %d)", wsID, tabID, overflowDroppedTotal, ptyMaxBufferedBytes)
		}
	}
	ts.LastOutputAt = time.Now()
	if !ts.FlushScheduled {
		ts.FlushScheduled = true
		ts.FlushPendingSince = ts.LastOutputAt
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
	if delay, deferred := ts.State.FlushDelay(time.Now(), quiet, maxInterval); deferred {
		ts.FlushScheduled = true
		return common.SafeTick(delay, func(t time.Time) tea.Msg {
			return messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: msg.TabID}
		})
	}

	ts.FlushScheduled = false
	ts.FlushPendingSince = time.Time{}
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
	if len(pendingClip) > 0 {
		clip := string(pendingClip)
		safego.Go("sidebar.osc52_clipboard", func() {
			common.CopyToClipboardWithLog(clip, "agent OSC52 (sidebar)")
		})
	}
	if !consumed {
		return nil
	}
	if len(ts.PendingOutput) == 0 {
		ts.PendingOutput = ts.PendingOutput[:0]
		return nil
	}
	ts.FlushScheduled = true
	ts.FlushPendingSince = time.Now()
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
	if termAlive {
		ts.mu.Lock()
		backoff, shouldRestart := ts.State.NextRestartBackoffLocked(ptyRestartWindow, ptyRestartMax)
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
		logging.Warn("Sidebar PTY stopped for workspace %s tab %s; restart limit reached, marking detached: %v", wsID, tabID, msg.Err)
	} else {
		ts.mu.Lock()
		ts.Running = false
		// Mark as detached - tmux session may still be alive
		ts.Detached = true
		ts.UserDetached = false
		ts.State.ResetRestartBackoffLocked()
		ts.mu.Unlock()
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
