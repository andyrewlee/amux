package sidebar

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/safego"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
	"github.com/andyrewlee/amux/internal/vterm"
)

// handlePTYOutput buffers incoming PTY data and schedules a flush.
func (m *TerminalModel) handlePTYOutput(msg messages.SidebarPTYOutput) tea.Cmd {
	tabID := TerminalTabID(msg.TabID)
	// Output read before a workspace rebind but delivered after it carries the
	// old ID; the exact-key lookup would miss and silently drop the payload —
	// a hole in the byte stream. Resolve like the flush handler does.
	tab, wsID := m.resolveTabForResult(msg.WorkspaceID, tabID, "sidebar PTY output")
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
	// from under the same lock; FlushScheduled/FlushPendingSince also sit
	// under ts.mu now — the writer goroutine mutates sibling State fields
	// there, so Update-goroutine access needs the same discipline.
	now := time.Now()
	ts.mu.Lock()
	ts.LastOutputAt = now
	if !ts.FlushScheduled {
		ts.FlushScheduled = true
		ts.FlushPendingSince = now
		quiet, _ := m.flushTimingForLocked(ts)
		ts.mu.Unlock()
		return common.SafeTick(quiet, func(t time.Time) tea.Msg {
			return messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: msg.TabID}
		})
	}
	ts.mu.Unlock()
	return nil
}

// handlePTYFlush writes buffered PTY data to the vterm when the quiet period expires.
func (m *TerminalModel) handlePTYFlush(msg messages.SidebarPTYFlush) tea.Cmd {
	tabID := TerminalTabID(msg.TabID)
	// A flush tick stamped before a workspace rebind carries the old ID; the
	// exact-key lookup would miss and leave FlushScheduled latched forever.
	tab, wsID := m.resolveTabForResult(msg.WorkspaceID, tabID, "sidebar PTY flush")
	if tab == nil || tab.State == nil {
		return nil
	}
	ts := tab.State
	now := time.Now()
	var pendingClip []byte
	var rearm bool
	ts.mu.Lock()
	quiet, maxInterval := m.flushTimingForLocked(ts)
	delay, deferred := ts.State.FlushGate(now, quiet, maxInterval)
	if !deferred && len(ts.PendingOutput) > 0 && ts.VTerm != nil {
		chunk := ts.State.TakeFlushChunkLocked(ptyFlushChunkSize)
		if !m.enqueueSidebarWriteLocked(ts, sidebarWriteReq{chunk: chunk, workspaceID: wsID, tabID: string(tabID)}) {
			pendingClip = drainSidebarWriteQueueLocked(ts, chunk)
		}
		rearm = ts.State.RearmFlush(now, nil)
	}
	ts.mu.Unlock()
	if deferred {
		return common.SafeTick(delay, func(t time.Time) tea.Msg {
			return messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: msg.TabID}
		})
	}
	if clip, ok := common.OSC52ClipboardText(pendingClip); ok {
		safego.Go("sidebar.osc52_clipboard", func() {
			common.CopyToClipboardWithLog(clip, "agent OSC52 (sidebar)")
		})
	}
	if !rearm {
		return nil
	}
	delay, _ = m.flushTimingFor(ts)
	if delay < time.Millisecond {
		delay = time.Millisecond
	}
	return common.SafeTick(delay, func(t time.Time) tea.Msg {
		return messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: msg.TabID}
	})
}

// handlePTYStopped handles PTY reader exit, restarting with backoff or marking detached.
func (m *TerminalModel) handlePTYStopped(msg messages.SidebarPTYStopped) tea.Cmd {
	tabID := TerminalTabID(msg.TabID)
	tab, wsID := m.resolveTabForResult(msg.WorkspaceID, tabID, "sidebar PTY stopped")
	if tab == nil || tab.State == nil {
		return nil
	}
	ts := tab.State
	termAlive := ts.Terminal != nil && !ts.Terminal.IsClosed()
	ts.mu.Lock()
	if ts.VTerm != nil && len(ts.NoiseTrailing) > 0 {
		// Synchronous on purpose: this drain belongs to the dead stream and
		// must be ordered before a restart's VTerm swap, so it doesn't go
		// through the writer queue.
		applySidebarWriteLocked(ts, sidebarWriteReq{drainTrailing: true})
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
		restartWt := wsID
		logging.Warn("Sidebar PTY stopped for workspace %s tab %s; restarting in %s: %v", wsID, tabID, backoff, msg.Err)
		return common.SafeTick(backoff, func(time.Time) tea.Msg {
			return messages.SidebarPTYRestart{WorkspaceID: restartWt, TabID: restartTab}
		})
	}
	if termAlive {
		logging.Error("Sidebar PTY stopped for workspace %s tab %s; restart limit reached, marking detached: %v", wsID, tabID, msg.Err)
	} else {
		logging.Info("Sidebar PTY stopped for workspace %s tab %s, marking detached: %v", wsID, tabID, msg.Err)
	}
	return nil
}

// handlePTYRestart re-starts the PTY reader after a backoff delay.
func (m *TerminalModel) handlePTYRestart(msg messages.SidebarPTYRestart) tea.Cmd {
	tab, wsID := m.resolveTabForResult(msg.WorkspaceID, TerminalTabID(msg.TabID), "sidebar PTY restart")
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
	return m.startPTYReader(wsID, tab.ID)
}
