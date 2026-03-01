package sidebar

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/safego"
	"github.com/andyrewlee/amux/internal/ui/common"
)

var sidebarDirectFlushRetryInterval = ptyFlushMaxInterval

func resetPTYOutputStateLocked(ts *TerminalState) {
	if ts == nil {
		return
	}
	ts.pendingOutput.Clear()
	ts.ptyNoiseTrailing = nil
	ts.flushScheduled = false
	ts.directFlushRetryArmed = false
	ts.ptyOutputClosed = true
	ts.lastOutputAt = time.Time{}
	ts.flushPendingSince = time.Time{}
}

func appendSidebarPTYOutput(ts *TerminalState, data []byte, now time.Time) bool {
	if ts == nil || len(data) == 0 {
		return false
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.ptyOutputClosed {
		return false
	}

	ts.pendingOutput.Append(data)
	if ts.pendingOutput.Len() > ptyMaxBufferedBytes {
		overflow := ts.pendingOutput.Len() - ptyMaxBufferedBytes
		dropped := ts.pendingOutput.DropOldest(overflow)
		perf.Count("sidebar_pty_drop_bytes", int64(dropped))
		perf.Count("sidebar_pty_drop", 1)
	}
	ts.lastOutputAt = now
	if ts.flushScheduled {
		return false
	}
	ts.flushScheduled = true
	ts.flushPendingSince = now
	return true
}

func (m *TerminalModel) emitDirectPTYFlush(wsID string, tab *TerminalTab) {
	if tab == nil || tab.State == nil || m.msgSink == nil {
		return
	}
	m.msgSink(messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: string(tab.ID)})

	ts := tab.State
	ts.mu.Lock()
	if ts.directFlushRetryArmed {
		ts.mu.Unlock()
		return
	}
	ts.directFlushRetryArmed = true
	ts.mu.Unlock()

	safego.Go("sidebar.pty_direct_flush_retry", func() {
		ticker := time.NewTicker(sidebarDirectFlushRetryInterval)
		defer ticker.Stop()
		for range ticker.C {
			ts.mu.Lock()
			closed := ts.ptyOutputClosed
			pending := ts.pendingOutput.Len() > 0
			scheduled := ts.flushScheduled
			if closed || !pending || !scheduled {
				ts.directFlushRetryArmed = false
				ts.mu.Unlock()
				return
			}
			tabID := tab.ID
			ts.mu.Unlock()

			if m.msgSink != nil {
				m.msgSink(messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: string(tabID)})
			}
		}
	})
}

func (m *TerminalModel) handleDirectPTYOutputChunk(wsID string, tab *TerminalTab, data []byte) bool {
	if len(data) == 0 {
		return true
	}
	if tab == nil || tab.State == nil {
		return true
	}
	if !appendSidebarPTYOutput(tab.State, data, time.Now()) {
		return true
	}
	m.emitDirectPTYFlush(wsID, tab)
	return true
}

// handlePTYOutput buffers incoming PTY data and schedules a flush.
func (m *TerminalModel) handlePTYOutput(msg messages.SidebarPTYOutput) tea.Cmd {
	wsID := msg.WorkspaceID
	tabID := TerminalTabID(msg.TabID)
	tab := m.getTabByID(wsID, tabID)
	if tab == nil || tab.State == nil {
		return nil
	}
	ts := tab.State
	if appendSidebarPTYOutput(ts, msg.Data, time.Now()) {
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
	ts.mu.Lock()
	lastOutputAt := ts.lastOutputAt
	flushPendingSince := ts.flushPendingSince
	ts.mu.Unlock()
	quietFor := now.Sub(lastOutputAt)
	pendingFor := time.Duration(0)
	if !flushPendingSince.IsZero() {
		pendingFor = now.Sub(flushPendingSince)
	}
	quiet, maxInterval := m.flushTiming()
	if quietFor < quiet && pendingFor < maxInterval {
		delay := quiet - quietFor
		if delay < time.Millisecond {
			delay = time.Millisecond
		}
		ts.mu.Lock()
		ts.flushScheduled = true
		ts.mu.Unlock()
		return common.SafeTick(delay, func(t time.Time) tea.Msg {
			return messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: msg.TabID}
		})
	}

	var consumed bool
	ts.mu.Lock()
	ts.flushScheduled = false
	ts.flushPendingSince = time.Time{}
	if ts.VTerm != nil && ts.pendingOutput.Len() > 0 {
		chunkSize := ts.pendingOutput.Len()
		if chunkSize > ptyFlushChunkSize {
			chunkSize = ptyFlushChunkSize
		}
		chunk := ts.pendingOutput.Pop(chunkSize)
		processedBytes := len(chunk)
		filtered := common.FilterKnownPTYNoiseStream(chunk, &ts.ptyNoiseTrailing)
		filteredBytes := processedBytes - len(filtered)
		perf.Count("pty_flush_bytes_processed", int64(processedBytes))
		if filteredBytes > 0 {
			perf.Count("pty_flush_bytes_filtered", int64(filteredBytes))
		}
		if len(filtered) > 0 {
			flushDone := perf.Time("pty_flush")
			ts.VTerm.Write(filtered)
			flushDone()
			perf.Count("pty_flush_bytes", int64(len(filtered)))
			refreshSidebarSnapshotLocked(ts)
		}
		consumed = true
	}
	remaining := ts.pendingOutput.Len()
	if remaining == 0 {
		ts.pendingOutput.Clear()
	} else {
		ts.flushScheduled = true
		ts.flushPendingSince = time.Now()
	}
	ts.mu.Unlock()
	if !consumed {
		return nil
	}
	if remaining > 0 {
		delay, _ := m.flushTiming()
		if delay < time.Millisecond {
			delay = time.Millisecond
		}
		return common.SafeTick(delay, func(t time.Time) tea.Msg {
			return messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: msg.TabID}
		})
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
	ts.mu.Lock()
	if ts.VTerm != nil && len(ts.ptyNoiseTrailing) > 0 {
		trailing := common.DrainKnownPTYNoiseTrailing(&ts.ptyNoiseTrailing)
		flushDone := perf.Time("pty_flush")
		ts.VTerm.Write(trailing)
		flushDone()
		perf.Count("pty_flush_bytes", int64(len(trailing)))
		refreshSidebarSnapshotLocked(ts)
	}
	ts.mu.Unlock()
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
