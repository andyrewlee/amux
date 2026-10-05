package sidebar

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
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
	ts.mu.Lock()
	stale := ts.State.ReaderGen != msg.Gen
	ts.mu.Unlock()
	if stale {
		logging.Debug("Dropping stale sidebar PTY output for workspace %s tab %s: a newer reader already runs", wsID, tabID)
		return nil
	}
	ts.mu.Lock()
	// Conservative reservation published before the unlocked append: the
	// writer must never observe an empty stream while bytes are in flight,
	// so pendingBufferedBytes covers the buffer plus the incoming chunk for
	// the whole AppendOutput window. The exact length is republished after.
	ts.pendingBufferedBytes = len(ts.PendingOutput) + len(msg.Data)
	ts.mu.Unlock()
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
	// Exact count after append/trim — the reservation above was conservative.
	ts.pendingBufferedBytes = len(ts.PendingOutput)
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
		req := sidebarWriteReq{chunk: chunk, workspaceID: wsID, tabID: string(tabID)}
		if !m.enqueueSidebarWriteLocked(ts, req) {
			pendingClip = drainSidebarWriteQueueLocked(ts, req)
		}
		rearm = ts.State.RearmFlush(now, nil)
		// The take above (and RearmFlush's drain truncation) shrank the
		// buffer — republish the worker-visible count under the same hold.
		ts.pendingBufferedBytes = len(ts.PendingOutput)
	}
	ts.mu.Unlock()
	if deferred {
		return common.SafeTick(delay, func(t time.Time) tea.Msg {
			return messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: msg.TabID}
		})
	}
	m.drainSidebarClipboard(pendingClip)
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
	ts.mu.Lock()
	stale := ts.State.ReaderGen != msg.Gen
	ts.mu.Unlock()
	if stale {
		logging.Debug("Dropping stale sidebar PTY stopped for workspace %s tab %s: a newer reader already runs", wsID, tabID)
		return nil
	}
	termAlive := ts.Terminal != nil && !ts.Terminal.IsClosed()
	ts.mu.Lock()
	// Stream-end ordering: every still-queued writer request precedes the
	// buffered chunks, and the held noise tail follows all of them — the
	// tail must not parse ahead of stream bytes. Synchronous on purpose:
	// this drain belongs to the dead stream and must be ordered before a
	// restart's VTerm swap, so it does not go through the writer queue.
	drainSidebarWriterQueueLocked(ts)
	for len(ts.PendingOutput) > 0 {
		chunk := ts.State.TakeFlushChunkLocked(ptyFlushChunkSize)
		if len(chunk) == 0 {
			break
		}
		applySidebarWriteLocked(ts, sidebarWriteReq{chunk: chunk, workspaceID: wsID, tabID: string(tabID)}, true)
	}
	ts.pendingBufferedBytes = len(ts.PendingOutput)
	if ts.VTerm != nil && len(ts.NoiseTrailing) > 0 {
		applySidebarWriteLocked(ts, sidebarWriteReq{drainTrailing: true}, false)
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
			return messages.SidebarPTYRestart{WorkspaceID: restartWt, TabID: restartTab, Gen: msg.Gen}
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
	ts.mu.Lock()
	stale := ts.State.ReaderGen != msg.Gen
	ts.mu.Unlock()
	if stale {
		logging.Debug("Dropping stale sidebar PTY restart for workspace %s tab %s: a newer reader already runs", wsID, msg.TabID)
		return nil
	}
	if ts.Terminal == nil || ts.Terminal.IsClosed() {
		ts.mu.Lock()
		ts.RestartBackoff = 0
		ts.mu.Unlock()
		return nil
	}
	return m.startPTYReader(wsID, tab.ID)
}
