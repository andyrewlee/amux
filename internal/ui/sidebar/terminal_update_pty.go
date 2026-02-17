package sidebar

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/common"
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
	ts.pendingOutput = append(ts.pendingOutput, msg.Data...)
	if len(ts.pendingOutput) > ptyMaxBufferedBytes {
		overflow := len(ts.pendingOutput) - ptyMaxBufferedBytes
		perf.Count("sidebar_pty_drop_bytes", int64(overflow))
		perf.Count("sidebar_pty_drop", 1)
		ts.pendingOutput = append([]byte(nil), ts.pendingOutput[overflow:]...)
	}
	ts.lastOutputAt = time.Now()
	if !ts.flushScheduled {
		ts.flushScheduled = true
		ts.flushPendingSince = ts.lastOutputAt
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
	now := time.Now()
	quietFor := now.Sub(ts.lastOutputAt)
	pendingFor := time.Duration(0)
	if !ts.flushPendingSince.IsZero() {
		pendingFor = now.Sub(ts.flushPendingSince)
	}
	quiet, maxInterval := m.flushTiming()
	if quietFor < quiet && pendingFor < maxInterval {
		delay := quiet - quietFor
		if delay < time.Millisecond {
			delay = time.Millisecond
		}
		ts.flushScheduled = true
		return common.SafeTick(delay, func(t time.Time) tea.Msg {
			return messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: msg.TabID}
		})
	}

	ts.flushScheduled = false
	ts.flushPendingSince = time.Time{}
	if len(ts.pendingOutput) > 0 {
		var wrote bool
		ts.mu.Lock()
		if ts.VTerm != nil {
			flushDone := perf.Time("pty_flush")
			chunkSize := len(ts.pendingOutput)
			if chunkSize > ptyFlushChunkSize {
				chunkSize = ptyFlushChunkSize
			}
			chunk := append([]byte(nil), ts.pendingOutput[:chunkSize]...)
			copy(ts.pendingOutput, ts.pendingOutput[chunkSize:])
			ts.pendingOutput = ts.pendingOutput[:len(ts.pendingOutput)-chunkSize]
			ts.VTerm.Write(chunk)
			flushDone()
			perf.Count("pty_flush_bytes", int64(len(chunk)))
			wrote = true
		}
		ts.mu.Unlock()
		if !wrote {
			return nil
		}
		if len(ts.pendingOutput) == 0 {
			ts.pendingOutput = ts.pendingOutput[:0]
		} else {
			ts.flushScheduled = true
			ts.flushPendingSince = time.Now()
			delay, _ := m.flushTiming()
			if delay < time.Millisecond {
				delay = time.Millisecond
			}
			return common.SafeTick(delay, func(t time.Time) tea.Msg {
				return messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: msg.TabID}
			})
		}
	}
	return nil
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
	m.stopPTYReader(ts)
	if termAlive {
		shouldRestart := true
		var backoff time.Duration
		ts.mu.Lock()
		if ts.ptyRestartSince.IsZero() || time.Since(ts.ptyRestartSince) > ptyRestartWindow {
			ts.ptyRestartSince = time.Now()
			ts.ptyRestartCount = 0
		}
		ts.ptyRestartCount++
		if ts.ptyRestartCount > ptyRestartMax {
			shouldRestart = false
			ts.Running = false
			// Mark as detached (tmux session may still be alive)
			ts.Detached = true
			ts.UserDetached = false
			ts.ptyRestartBackoff = 0
		} else {
			backoff = ts.ptyRestartBackoff
			if backoff <= 0 {
				backoff = 200 * time.Millisecond
			} else {
				backoff *= 2
				if backoff > 5*time.Second {
					backoff = 5 * time.Second
				}
			}
			ts.ptyRestartBackoff = backoff
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
		ts.ptyRestartBackoff = 0
		ts.ptyRestartCount = 0
		ts.ptyRestartSince = time.Time{}
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
		ts.ptyRestartBackoff = 0
		ts.mu.Unlock()
		return nil
	}
	return m.startPTYReader(msg.WorkspaceID, tab.ID)
}
