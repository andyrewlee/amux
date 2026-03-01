package center

import (
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
)

type ansiActivityState uint8

const (
	ansiActivityText ansiActivityState = iota
	ansiActivityEsc
	ansiActivityEscSequence
	ansiActivityCSI
	ansiActivityOSC
	ansiActivityOSCEsc
	ansiActivityString
	ansiActivityStringEsc
)

func hasVisiblePTYOutput(data []byte, state ansiActivityState) (bool, ansiActivityState) {
	if len(data) == 0 {
		return false, state
	}
	visible := false
	for _, b := range data {
		switch state {
		case ansiActivityText:
			switch b {
			case 0x1b:
				state = ansiActivityEsc
			default:
				if isVisibleByte(b) {
					visible = true
				}
			}

		case ansiActivityEsc:
			switch b {
			case '[':
				state = ansiActivityCSI
			case ']':
				state = ansiActivityOSC
			case 'P', 'X', '^', '_':
				state = ansiActivityString
			default:
				switch {
				// ESC sequences can include intermediates before a final byte.
				// Consume them as control data so bytes like ESC(B don't count as visible text.
				case b >= 0x20 && b <= 0x2f:
					state = ansiActivityEscSequence
				// Two-byte ESC sequence final byte.
				case b >= 0x30 && b <= 0x7e:
					state = ansiActivityText
				default:
					state = ansiActivityText
				}
			}

		case ansiActivityEscSequence:
			// ESC with intermediates terminates on final byte 0x30..0x7E.
			if b >= 0x30 && b <= 0x7e {
				state = ansiActivityText
			} else if b == 0x1b {
				state = ansiActivityEsc
			}

		case ansiActivityCSI:
			// CSI completes on a final byte in 0x40..0x7E.
			if b >= 0x40 && b <= 0x7e {
				state = ansiActivityText
			} else if b == 0x1b {
				state = ansiActivityEsc
			}

		case ansiActivityOSC:
			// OSC terminates with BEL or ST (ESC \).
			if b == 0x07 {
				state = ansiActivityText
			} else if b == 0x1b {
				state = ansiActivityOSCEsc
			}

		case ansiActivityOSCEsc:
			if b == '\\' {
				state = ansiActivityText
			} else if b != 0x1b {
				state = ansiActivityOSC
			}

		case ansiActivityString:
			// DCS/SOS/PM/APC terminate with ST (ESC \).
			if b == 0x1b {
				state = ansiActivityStringEsc
			}

		case ansiActivityStringEsc:
			if b == '\\' {
				state = ansiActivityText
			} else if b != 0x1b {
				state = ansiActivityString
			}
		}
	}
	return visible, state
}

func isVisibleByte(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r':
		return false
	}
	return b >= 0x20 && b != 0x7f
}

func (m *Model) appendPTYOutput(tab *Tab, data []byte, now time.Time) bool {
	if tab == nil || len(data) == 0 {
		return false
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()

	tab.pendingOutput.Append(data)
	if tab.pendingOutput.Len() > ptyMaxBufferedBytes {
		overflow := tab.pendingOutput.Len() - ptyMaxBufferedBytes
		dropped := tab.pendingOutput.DropOldest(overflow)
		perf.Count("pty_output_drop_bytes", int64(dropped))
		perf.Count("pty_output_drop", 1)
	}
	tab.lastOutputAt = now
	if m.isChatTab(tab) {
		if tab.bootstrapActivity &&
			!tab.bootstrapLastOutputAt.IsZero() &&
			now.Sub(tab.bootstrapLastOutputAt) >= bootstrapQuietGap {
			tab.bootstrapActivity = false
			tab.bootstrapLastOutputAt = time.Time{}
		}
		hasVisibleOutput, next := hasVisiblePTYOutput(data, tab.activityANSIState)
		tab.activityANSIState = next
		if hasVisibleOutput {
			tab.pendingVisibleOutput = true
			tab.pendingVisibleSeq++
		}
	}
	if tab.flushScheduled {
		return false
	}
	tab.flushScheduled = true
	tab.flushPendingSince = now
	return true
}

func (m *Model) handleDirectPTYOutputChunk(workspaceID string, tab *Tab, data []byte) bool {
	if len(data) == 0 {
		return true
	}
	if tab == nil || tab.isClosed() {
		return true
	}
	m.tracePTYOutput(tab, data)
	perf.Count("pty_output_bytes", int64(len(data)))
	if !m.appendPTYOutput(tab, data, time.Now()) {
		return true
	}
	m.emitDirectPTYFlush(workspaceID, tab)
	return true
}

// updatePTYOutput handles PTYOutput.
func (m *Model) updatePTYOutput(msg PTYOutput) tea.Cmd {
	var cmds []tea.Cmd
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab != nil && !tab.isClosed() {
		m.tracePTYOutput(tab, msg.Data)
		perf.Count("pty_output_bytes", int64(len(msg.Data)))
		now := time.Now()
		if m.appendPTYOutput(tab, msg.Data, now) {
			quiet, _ := m.flushTiming(tab, m.isActiveTab(msg.WorkspaceID, msg.TabID))
			tabID := msg.TabID // Capture for closure
			cmds = append(cmds, common.SafeTick(quiet, func(t time.Time) tea.Msg {
				return PTYFlush{WorkspaceID: msg.WorkspaceID, TabID: tabID}
			}))
		}
		if m.isChatTab(tab) {
			refreshDelay := cursorSuppressWindow + 20*time.Millisecond
			tab.mu.Lock()
			tab.cursorRefreshDueAt = now.Add(refreshDelay)
			// Bubble Tea processes Update messages serially; this flag-gated
			// scheduling assumes a single writer for this tab state.
			scheduleCursorRefresh := !tab.cursorRefreshScheduled
			if scheduleCursorRefresh {
				tab.cursorRefreshScheduled = true
			}
			tab.mu.Unlock()
			if scheduleCursorRefresh {
				workspaceID := msg.WorkspaceID
				tabID := msg.TabID
				cmds = append(cmds, common.SafeTick(refreshDelay, func(time.Time) tea.Msg {
					return PTYCursorRefresh{WorkspaceID: workspaceID, TabID: tabID}
				}))
			}
		}
	}
	return common.SafeBatch(cmds...)
}

func (m *Model) updatePTYCursorRefresh(msg PTYCursorRefresh) tea.Cmd {
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab == nil || tab.isClosed() {
		return nil
	}
	tab.mu.Lock()
	if !tab.cursorRefreshScheduled {
		tab.mu.Unlock()
		return nil
	}
	remaining := time.Until(tab.cursorRefreshDueAt)
	if remaining <= 0 {
		tab.cursorRefreshScheduled = false
		tab.cursorRefreshDueAt = time.Time{}
		tab.mu.Unlock()
		return nil
	}
	if remaining < time.Millisecond {
		remaining = time.Millisecond
	}
	workspaceID := msg.WorkspaceID
	tabID := msg.TabID
	tab.mu.Unlock()
	return common.SafeTick(remaining, func(time.Time) tea.Msg {
		return PTYCursorRefresh{WorkspaceID: workspaceID, TabID: tabID}
	})
}

// updatePTYFlush handles PTYFlush.
func (m *Model) updatePTYFlush(msg PTYFlush) tea.Cmd {
	var cmds []tea.Cmd
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab != nil && !tab.isClosed() {
		now := time.Now()
		tab.mu.Lock()
		lastOutputAt := tab.lastOutputAt
		flushPendingSince := tab.flushPendingSince
		tab.mu.Unlock()
		quietFor := now.Sub(lastOutputAt)
		pendingFor := time.Duration(0)
		if !flushPendingSince.IsZero() {
			pendingFor = now.Sub(flushPendingSince)
		}
		quiet, maxInterval := m.flushTiming(tab, m.isActiveTab(msg.WorkspaceID, msg.TabID))
		if quietFor < quiet && pendingFor < maxInterval {
			delay := quiet - quietFor
			if delay < time.Millisecond {
				delay = time.Millisecond
			}
			tabID := msg.TabID
			tab.mu.Lock()
			tab.flushScheduled = true
			tab.mu.Unlock()
			cmds = append(cmds, common.SafeTick(delay, func(t time.Time) tea.Msg {
				return PTYFlush{WorkspaceID: msg.WorkspaceID, TabID: tabID}
			}))
			return common.SafeBatch(cmds...)
		}

		var chunk []byte
		writeOutput := false
		hasMoreBuffered := false
		visibleSeq := uint64(0)
		tagSessionName := ""
		var tagTimestamp int64
		isActive := m.isActiveTab(msg.WorkspaceID, msg.TabID)
		tab.mu.Lock()
		tab.flushScheduled = false
		tab.flushPendingSince = time.Time{}
		if tab.Terminal != nil && tab.pendingOutput.Len() > 0 {
			chunkSize := tab.pendingOutput.Len()
			maxChunk := ptyFlushChunkSize
			if isActive {
				maxChunk = ptyFlushChunkSizeActive
			}
			if chunkSize > maxChunk {
				chunkSize = maxChunk
			}
			chunk = tab.pendingOutput.Pop(chunkSize)
			hasMoreBuffered = tab.pendingOutput.Len() > 0
			visibleSeq = tab.pendingVisibleSeq
			writeOutput = true
		}
		tab.mu.Unlock()
		if writeOutput && len(chunk) > 0 {
			if m.isTabActorReady() {
				if !m.sendTabEvent(tabEvent{
					tab:             tab,
					workspaceID:     msg.WorkspaceID,
					tabID:           msg.TabID,
					kind:            tabEventWriteOutput,
					output:          chunk,
					hasMoreBuffered: hasMoreBuffered,
					visibleSeq:      visibleSeq,
				}) {
					processedBytes := len(chunk)
					filteredLen := 0
					filterApplied := false
					tab.mu.Lock()
					if tab.Terminal != nil {
						filtered := common.FilterKnownPTYNoiseStream(chunk, &tab.ptyNoiseTrailing)
						filteredLen = len(filtered)
						filterApplied = true
						if len(filtered) > 0 {
							flushDone := perf.Time("pty_flush")
							tab.Terminal.Write(filtered)
							flushDone()
							perf.Count("pty_flush_bytes", int64(len(filtered)))
							m.refreshTabSnapshotLocked(tab)
						}
						// Activity state intentionally tracks visible terminal mutations only.
						// Noise-only chunks are filtered above and must not update activity tags.
						// We still run this to clear pending visible state when no mutation occurred.
						tagSessionName, tagTimestamp, _ = m.noteVisibleActivityLocked(tab, hasMoreBuffered, visibleSeq)
					}
					tab.mu.Unlock()
					perf.Count("pty_flush_bytes_processed", int64(processedBytes))
					if filterApplied {
						filteredBytes := processedBytes - filteredLen
						if filteredBytes > 0 {
							perf.Count("pty_flush_bytes_filtered", int64(filteredBytes))
						}
					}
				}
			} else {
				processedBytes := len(chunk)
				filteredLen := 0
				filterApplied := false
				tab.mu.Lock()
				if tab.Terminal != nil {
					filtered := common.FilterKnownPTYNoiseStream(chunk, &tab.ptyNoiseTrailing)
					filteredLen = len(filtered)
					filterApplied = true
					if len(filtered) > 0 {
						flushDone := perf.Time("pty_flush")
						tab.Terminal.Write(filtered)
						flushDone()
						perf.Count("pty_flush_bytes", int64(len(filtered)))
						m.refreshTabSnapshotLocked(tab)
					}
					// Activity state intentionally tracks visible terminal mutations only.
					// Noise-only chunks are filtered above and must not update activity tags.
					// We still run this to clear pending visible state when no mutation occurred.
					tagSessionName, tagTimestamp, _ = m.noteVisibleActivityLocked(tab, hasMoreBuffered, visibleSeq)
				}
				tab.mu.Unlock()
				perf.Count("pty_flush_bytes_processed", int64(processedBytes))
				if filterApplied {
					filteredBytes := processedBytes - filteredLen
					if filteredBytes > 0 {
						perf.Count("pty_flush_bytes_filtered", int64(filteredBytes))
					}
				}
			}
			if tagSessionName != "" {
				opts := m.getTmuxOptions()
				sessionName := tagSessionName
				timestamp := strconv.FormatInt(tagTimestamp, 10)
				cmds = append(cmds, func() tea.Msg {
					_ = tmux.SetSessionTagValue(sessionName, tmux.TagLastOutputAt, timestamp, opts)
					return nil
				})
			}
		}
		tab.mu.Lock()
		remaining := tab.pendingOutput.Len()
		if remaining == 0 {
			tab.pendingOutput.Clear()
		} else {
			tab.flushScheduled = true
			tab.flushPendingSince = time.Now()
		}
		tab.mu.Unlock()
		if remaining > 0 {
			tabID := msg.TabID
			quietNext, _ := m.flushTiming(tab, m.isActiveTab(msg.WorkspaceID, msg.TabID))
			delay := quietNext
			if delay < time.Millisecond {
				delay = time.Millisecond
			}
			cmds = append(cmds, common.SafeTick(delay, func(t time.Time) tea.Msg {
				return PTYFlush{WorkspaceID: msg.WorkspaceID, TabID: tabID}
			}))
		}
	}
	return common.SafeBatch(cmds...)
}

// updatePTYStopped handles PTYStopped.
func (m *Model) updatePTYStopped(msg PTYStopped) tea.Cmd {
	var cmds []tea.Cmd
	var tagSessionName string
	var tagTimestamp int64
	// Terminal closed - mark tab as not running, but keep it visible
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab != nil {
		termAlive := tab.Agent != nil && tab.Agent.Terminal != nil && !tab.Agent.Terminal.IsClosed()
		m.stopPTYReader(tab)
		tab.mu.Lock()
		if tab.Terminal != nil && len(tab.ptyNoiseTrailing) > 0 {
			trailing := common.DrainKnownPTYNoiseTrailing(&tab.ptyNoiseTrailing)
			flushDone := perf.Time("pty_flush")
			tab.Terminal.Write(trailing)
			flushDone()
			perf.Count("pty_flush_bytes", int64(len(trailing)))
			m.refreshTabSnapshotLocked(tab)
			// Reconcile pending activity state for terminal-visible output.
			tagSessionName, tagTimestamp, _ = m.noteVisibleActivityLocked(tab, false, tab.pendingVisibleSeq)
		}
		tab.mu.Unlock()
		if tagSessionName != "" {
			opts := m.getTmuxOptions()
			sessionName := tagSessionName
			timestamp := strconv.FormatInt(tagTimestamp, 10)
			cmds = append(cmds, func() tea.Msg {
				_ = tmux.SetSessionTagValue(sessionName, tmux.TagLastOutputAt, timestamp, opts)
				return nil
			})
		}
		tab.resetActivityANSIState()
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
	tab.resetActivityANSIState()
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
