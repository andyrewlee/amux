package sidebar

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
	"github.com/andyrewlee/amux/internal/vterm"
)

// overflowLogThrottle bounds how often a sustained PTY overflow logs.
const overflowLogThrottle = 2 * time.Second

// noteOverflowDropLocked accumulates dropped overflow bytes and reports whether a
// throttled overflow Warn should be emitted now (caller logs outside the lock),
// returning the aggregated dropped-byte total. The caller must hold ts.mu.
func (ts *TerminalState) noteOverflowDropLocked(droppedBytes int) (logNow bool, total int) {
	ts.OverflowDroppedSinceLog += droppedBytes
	now := time.Now()
	if ts.LastOverflowLogAt.IsZero() || now.Sub(ts.LastOverflowLogAt) >= overflowLogThrottle {
		total = ts.OverflowDroppedSinceLog
		ts.OverflowDroppedSinceLog = 0
		ts.LastOverflowLogAt = now
		return true, total
	}
	return false, 0
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
	data := msg.Data
	ts.mu.Lock()
	if ts.OverflowTrimCarry != (vterm.ParserCarryState{}) {
		data, ts.OverflowTrimCarry = ptyio.TrimPTYOverflowPrefix(data, 0, ts.OverflowTrimCarry)
	}
	ts.mu.Unlock()
	prevPendingLen := len(ts.PendingOutput)
	ts.PendingOutput = append(ts.PendingOutput, data...)
	if len(ts.PendingOutput) > ptyMaxBufferedBytes {
		overflow := len(ts.PendingOutput) - ptyMaxBufferedBytes
		perf.Count("sidebar_pty_drop_bytes", int64(overflow))
		perf.Count("sidebar_pty_drop", 1)
		seed := vterm.ParserCarryState{}
		combinedLen := len(ts.PendingOutput)
		ts.mu.Lock()
		if ts.VTerm != nil {
			seed = ts.VTerm.ParserCarryState()
			ts.VTerm.ResetParserState()
		}
		ts.mu.Unlock()
		retained, overflowCarry := ptyio.TrimPTYOverflowPrefix(ts.PendingOutput, overflow, seed)
		retainedStart := combinedLen - len(retained)
		ts.PendingOutput = append([]byte(nil), retained...)
		ts.mu.Lock()
		ts.OverflowTrimCarry = overflowCarry
		if retainedStart > prevPendingLen {
			ts.NoiseTrailing = nil
		}
		overflowLogNow, overflowDroppedTotal := ts.noteOverflowDropLocked(retainedStart)
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
	now := time.Now()
	quietFor := now.Sub(ts.LastOutputAt)
	pendingFor := time.Duration(0)
	if !ts.FlushPendingSince.IsZero() {
		pendingFor = now.Sub(ts.FlushPendingSince)
	}
	quiet, maxInterval := m.flushTiming()
	if quietFor < quiet && pendingFor < maxInterval {
		delay := quiet - quietFor
		if delay < time.Millisecond {
			delay = time.Millisecond
		}
		ts.FlushScheduled = true
		return common.SafeTick(delay, func(t time.Time) tea.Msg {
			return messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: msg.TabID}
		})
	}

	ts.FlushScheduled = false
	ts.FlushPendingSince = time.Time{}
	if len(ts.PendingOutput) == 0 || !flushSidebarPendingOutput(ts) {
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

func flushSidebarPendingOutput(ts *TerminalState) bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.VTerm == nil {
		return false
	}
	chunkSize := len(ts.PendingOutput)
	if chunkSize > ptyFlushChunkSize {
		chunkSize = ptyFlushChunkSize
	}
	chunk := append([]byte(nil), ts.PendingOutput[:chunkSize]...)
	copy(ts.PendingOutput, ts.PendingOutput[chunkSize:])
	ts.PendingOutput = ts.PendingOutput[:len(ts.PendingOutput)-chunkSize]
	processedBytes := len(chunk)
	filtered := ptyio.FilterKnownPTYNoiseStream(chunk, &ts.NoiseTrailing)
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
	}
	return true
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
