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
			perf.Count("pty_output_drop", 1)
			tab.pendingOutput = append([]byte(nil), tab.pendingOutput[overflow:]...)
		}
		perf.Count("pty_output_bytes", int64(len(msg.Data)))
		now := time.Now()
		tab.lastOutputAt = now
		if m.isChatTab(tab) {
			sessionName := tab.SessionName
			if sessionName == "" && tab.Agent != nil {
				sessionName = tab.Agent.Session
			}
			hasVisibleOutput := tab.consumeActivityVisibility(msg.Data)
			if hasVisibleOutput {
				tab.lastVisibleOutput = now
			}
			if sessionName != "" && hasVisibleOutput && tab.lastOutputAt.Sub(tab.lastActivityTagAt) >= activityTagThrottle {
				tab.lastActivityTagAt = now
				opts := m.getTmuxOptions()
				timestamp := now.UnixMilli()
				cmds = append(cmds, func() tea.Msg {
					_ = tmux.SetSessionTagValue(sessionName, tmux.TagLastOutputAt, strconv.FormatInt(timestamp, 10), opts)
					_ = tmux.SetSessionTagValue(sessionName, tmux.TagSessionLeaseAt, strconv.FormatInt(timestamp, 10), opts)
					return nil
				})
			}
		}
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
			isActive := m.isActiveTab(msg.WorkspaceID, msg.TabID)
			tab.mu.Lock()
			if tab.Terminal != nil {
				chunkSize := len(tab.pendingOutput)
				maxChunk := ptyFlushChunkSize
				if isActive {
					maxChunk = ptyFlushChunkSizeActive
				}
				if chunkSize > maxChunk {
					chunkSize = maxChunk
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
