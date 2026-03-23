package center

import (
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/vterm"
)

// updatePTYOutput handles PTYOutput.
func (m *Model) updatePTYOutput(msg PTYOutput) tea.Cmd {
	var cmds []tea.Cmd
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab != nil && !tab.isClosed() {
		m.tracePTYOutput(tab, msg.Data)
		data := msg.Data
		tab.mu.Lock()
		if tab.overflowTrimCarry != (vterm.ParserCarryState{}) {
			data, tab.overflowTrimCarry = common.TrimPTYOverflowPrefix(data, 0, tab.overflowTrimCarry)
			tab.activityANSIState = ansiActivityText
		}
		tab.mu.Unlock()
		prevPendingLen := len(tab.pendingOutput)
		activityData := data
		activityState := ansiActivityText
		activityStateSet := false
		tab.pendingOutput = append(tab.pendingOutput, data...)
		tab.mu.Lock()
		tab.pendingOutputBytes = len(tab.pendingOutput)
		tab.ptyBytesReceived += uint64(len(data))
		tab.mu.Unlock()
		if len(tab.pendingOutput) > ptyMaxBufferedBytes {
			overflow := len(tab.pendingOutput) - ptyMaxBufferedBytes
			perf.Count("pty_output_drop_bytes", int64(overflow))
			perf.Count("pty_output_drop", 1)
			seed := vterm.ParserCarryState{}
			combinedLen := len(tab.pendingOutput)
			resetNow := false
			if m.isTabActorReady() {
				tab.mu.Lock()
				if tab.Terminal != nil {
					if tab.actorWritesPending > 0 {
						if tab.parserResetPending {
							seed = vterm.ParserCarryState{}
						} else {
							seed = tab.actorQueuedCarry
							tab.parserResetPending = true
						}
					} else {
						seed = tab.Terminal.ParserCarryState()
						tab.Terminal.ResetParserState()
						resetNow = true
					}
				}
				tab.mu.Unlock()
			} else {
				tab.mu.Lock()
				if tab.Terminal != nil {
					if tab.actorWritesPending > 0 {
						seed = tab.Terminal.ParserCarryState()
						tab.settlePTYBytesLocked(tab.actorQueuedBytes)
						tab.actorQueuedBytes = 0
						tab.actorWriteEpoch++
						tab.actorWritesPending = 0
						tab.parserResetPending = false
						tab.actorQueuedCarry = vterm.ParserCarryState{}
						tab.actorQueuedNoiseTrailing = tab.actorQueuedNoiseTrailing[:0]
						tab.ptyNoiseTrailing = nil
					} else {
						seed = tab.Terminal.ParserCarryState()
					}
					tab.Terminal.ResetParserState()
					resetNow = true
				}
				tab.mu.Unlock()
			}
			retained, overflowCarry := common.TrimPTYOverflowPrefix(tab.pendingOutput, overflow, seed)
			retainedStart := combinedLen - len(retained)
			chunkStart := prevPendingLen
			if retainedStart > chunkStart {
				dropFromMsg := retainedStart - chunkStart
				if dropFromMsg >= len(data) {
					activityData = nil
				} else {
					activityData = data[dropFromMsg:]
				}
			}
			tab.pendingOutput = append([]byte(nil), retained...)
			activityPrefixLen := len(retained) - len(activityData)
			if activityPrefixLen < 0 {
				activityPrefixLen = 0
			}
			_, activityState = hasVisiblePTYOutput(retained[:activityPrefixLen], ansiActivityText)
			activityStateSet = true
			tab.mu.Lock()
			tab.pendingOutputBytes = len(tab.pendingOutput)
			tab.settlePTYBytesLocked(overflow)
			tab.overflowTrimCarry = overflowCarry
			if resetNow && retainedStart > chunkStart {
				tab.ptyNoiseTrailing = nil
				tab.actorQueuedNoiseTrailing = tab.actorQueuedNoiseTrailing[:0]
			}
			tab.mu.Unlock()
		}
		perf.Count("pty_output_bytes", int64(len(msg.Data)))
		now := time.Now()
		tab.lastOutputAt = now
		if m.isChatTab(tab) {
			tab.mu.Lock()
			if tab.bootstrapActivity &&
				!tab.bootstrapLastOutputAt.IsZero() &&
				now.Sub(tab.bootstrapLastOutputAt) >= bootstrapQuietGap {
				tab.bootstrapActivity = false
				tab.bootstrapLastOutputAt = time.Time{}
			}
			if activityStateSet {
				tab.activityANSIState = activityState
			}
			tab.mu.Unlock()
			hasVisibleOutput := tab.consumeActivityVisibility(activityData)
			if hasVisibleOutput {
				tab.mu.Lock()
				tab.pendingVisibleOutput = true
				tab.pendingVisibleSeq++
				tab.mu.Unlock()
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
		isActive := m.isActiveTab(msg.WorkspaceID, msg.TabID)
		tab.mu.Lock()
		if !isActive {
			tab.clearCatchUpLocked()
		} else {
			tab.expireCatchUpLocked()
		}
		catchUp := isActive && tab.catchUpActiveLocked()
		tab.mu.Unlock()
		now := time.Now()
		quietFor := now.Sub(tab.lastOutputAt)
		pendingFor := time.Duration(0)
		if !tab.flushPendingSince.IsZero() {
			pendingFor = now.Sub(tab.flushPendingSince)
		}
		quiet, maxInterval := m.flushTiming(tab, isActive)
		if quietFor < quiet && pendingFor < maxInterval {
			delay := quiet - quietFor
			if delay < time.Millisecond {
				delay = time.Millisecond
			}
			tabID := msg.TabID
			tab.flushScheduled = true
			cmds = append(cmds, common.SafeTick(delay, func(t time.Time) tea.Msg {
				return PTYFlush{WorkspaceID: msg.WorkspaceID, TabID: tabID, CatchUp: catchUp}
			}))
			return common.SafeBatch(cmds...)
		}

		tab.flushScheduled = false
		tab.flushPendingSince = time.Time{}
		if len(tab.pendingOutput) > 0 {
			var chunk []byte
			writeOutput := false
			hasMoreBuffered := false
			visibleSeq := uint64(0)
			tagSessionName := ""
			var tagTimestamp int64
			parserResetPending := false
			actorWritesPending := 0
			tab.mu.Lock()
			if tab.Terminal != nil {
				parserResetPending = tab.parserResetPending
				actorWritesPending = tab.actorWritesPending
				chunkSize := len(tab.pendingOutput)
				maxChunk := ptyFlushChunkSize
				if isActive {
					maxChunk = ptyFlushChunkSizeActive
				}
				if catchUp && m.isTabActorReady() {
					if ptyFlushChunkSizeCatchUp > maxChunk {
						maxChunk = ptyFlushChunkSizeCatchUp
					}
				}
				if !parserResetPending && chunkSize > maxChunk {
					chunkSize = maxChunk
				}
				if !parserResetPending && chunkSize > 0 {
					chunk = append(chunk, tab.pendingOutput[:chunkSize]...)
					copy(tab.pendingOutput, tab.pendingOutput[chunkSize:])
					tab.pendingOutput = tab.pendingOutput[:len(tab.pendingOutput)-chunkSize]
					tab.pendingOutputBytes = len(tab.pendingOutput)
					hasMoreBuffered = len(tab.pendingOutput) > 0
					visibleSeq = tab.pendingVisibleSeq
					writeOutput = true
				}
			}
			tab.mu.Unlock()
			if parserResetPending {
				if actorWritesPending > 0 && m.isTabActorReady() {
					tab.flushScheduled = true
					tab.flushPendingSince = time.Now()
					delay, _ := m.flushTiming(tab, m.isActiveTab(msg.WorkspaceID, msg.TabID))
					if delay < time.Millisecond {
						delay = time.Millisecond
					}
					tabID := msg.TabID
					cmds = append(cmds, common.SafeTick(delay, func(t time.Time) tea.Msg {
						return PTYFlush{WorkspaceID: msg.WorkspaceID, TabID: tabID, CatchUp: catchUp}
					}))
					return common.SafeBatch(cmds...)
				}
				tab.mu.Lock()
				if tab.Terminal != nil {
					if actorWritesPending > 0 {
						tab.settlePTYBytesLocked(tab.actorQueuedBytes)
						tab.actorQueuedBytes = 0
						tab.actorWriteEpoch++
						tab.ptyNoiseTrailing = nil
					}
					tab.Terminal.ResetParserState()
					tab.activityANSIState = ansiActivityText
				}
				tab.parserResetPending = false
				tab.actorWritesPending = 0
				tab.actorQueuedCarry = vterm.ParserCarryState{}
				tab.actorQueuedNoiseTrailing = tab.actorQueuedNoiseTrailing[:0]
				tab.mu.Unlock()
			}
			if writeOutput && len(chunk) > 0 {
				if m.isTabActorReady() {
					prevPending := 0
					prevEpoch := uint64(0)
					prevCarry := vterm.ParserCarryState{}
					var nextCarry vterm.ParserCarryState
					prevNoiseTrailing := []byte(nil)
					nextNoiseTrailing := []byte(nil)
					tab.mu.Lock()
					if tab.Terminal != nil {
						prevPending = tab.actorWritesPending
						prevEpoch = tab.actorWriteEpoch
						prevCarry = tab.actorQueuedCarry
						prevNoiseTrailing = append(prevNoiseTrailing, tab.actorQueuedNoiseTrailing...)
						seedCarry := tab.Terminal.ParserCarryState()
						if prevPending > 0 {
							seedCarry = prevCarry
						}
						previewTrailing := append([]byte(nil), tab.ptyNoiseTrailing...)
						if prevPending > 0 {
							previewTrailing = append(previewTrailing[:0], tab.actorQueuedNoiseTrailing...)
						}
						filteredPreview := common.FilterKnownPTYNoiseStream(chunk, &previewTrailing)
						nextCarry = vterm.AdvanceParserCarryState(seedCarry, filteredPreview)
						nextNoiseTrailing = append(nextNoiseTrailing, previewTrailing...)
						tab.actorWritesPending = prevPending + 1
						tab.actorQueuedBytes += len(chunk)
						tab.actorQueuedCarry = nextCarry
						tab.actorQueuedNoiseTrailing = append(tab.actorQueuedNoiseTrailing[:0], nextNoiseTrailing...)
					}
					tab.mu.Unlock()
					if !m.sendTabEvent(tabEvent{
						tab:             tab,
						workspaceID:     msg.WorkspaceID,
						tabID:           msg.TabID,
						kind:            tabEventWriteOutput,
						output:          chunk,
						writeEpoch:      prevEpoch,
						catchUp:         catchUp,
						hasMoreBuffered: hasMoreBuffered,
						visibleSeq:      visibleSeq,
					}) {
						rebuffered := false
						dropWrite := false
						syncFallbackChunkSize := 0
						tab.mu.Lock()
						if tab.actorWriteEpoch == prevEpoch && tab.actorWritesPending > 0 {
							tab.actorWritesPending--
							if tab.actorQueuedBytes >= len(chunk) {
								tab.actorQueuedBytes -= len(chunk)
							} else {
								tab.actorQueuedBytes = 0
							}
							if tab.isClosed() {
								dropWrite = true
							} else if tab.actorWritesPending > 0 {
								rebuffered = true
								tab.actorQueuedCarry = prevCarry
								tab.actorQueuedNoiseTrailing = append(tab.actorQueuedNoiseTrailing[:0], prevNoiseTrailing...)
								restored := make([]byte, 0, len(chunk)+len(tab.pendingOutput))
								restored = append(restored, chunk...)
								restored = append(restored, tab.pendingOutput...)
								tab.pendingOutput = restored
								tab.pendingOutputBytes = len(tab.pendingOutput)
							} else if catchUp && len(chunk) > ptyFlushChunkSizeActive {
								syncFallbackChunkSize = ptyFlushChunkSizeActive
								tab.actorQueuedCarry = prevCarry
								tab.actorQueuedNoiseTrailing = append(tab.actorQueuedNoiseTrailing[:0], prevNoiseTrailing...)
								restored := make([]byte, 0, len(chunk)-syncFallbackChunkSize+len(tab.pendingOutput))
								restored = append(restored, chunk[syncFallbackChunkSize:]...)
								restored = append(restored, tab.pendingOutput...)
								tab.pendingOutput = restored
								tab.pendingOutputBytes = len(tab.pendingOutput)
								hasMoreBuffered = len(tab.pendingOutput) > 0
							}
						} else if tab.actorWriteEpoch != prevEpoch || tab.isClosed() {
							dropWrite = true
						}
						tab.mu.Unlock()
						if syncFallbackChunkSize > 0 {
							chunk = chunk[:syncFallbackChunkSize]
						}
						if !rebuffered && !dropWrite {
							processedBytes := len(chunk)
							filteredLen := 0
							filterApplied := false
							suppressRedraw := false
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
								}
								// Activity state intentionally tracks visible terminal mutations only.
								// Noise-only chunks are filtered above and must not update activity tags.
								// We still run this to clear pending visible state when no mutation occurred.
								tagSessionName, tagTimestamp, _ = m.noteVisibleActivityLockedWithOutput(tab, hasMoreBuffered, visibleSeq, filtered)
								catchUpBefore, catchUpAfter := tab.settlePTYBytesLocked(processedBytes)
								suppressRedraw = catchUpBefore && catchUpAfter
								tab.actorQueuedCarry = tab.Terminal.ParserCarryState()
								tab.actorQueuedNoiseTrailing = append(tab.actorQueuedNoiseTrailing[:0], tab.ptyNoiseTrailing...)
							}
							tab.mu.Unlock()
							if !suppressRedraw {
								cmds = append(cmds, m.scheduleChatCursorRefresh(tab, msg.WorkspaceID, time.Now()))
							}
							perf.Count("pty_flush_bytes_processed", int64(processedBytes))
							if filterApplied {
								filteredBytes := processedBytes - filteredLen
								if filteredBytes > 0 {
									perf.Count("pty_flush_bytes_filtered", int64(filteredBytes))
								}
							}
						}
					}
				} else {
					processedBytes := len(chunk)
					filteredLen := 0
					filterApplied := false
					suppressRedraw := false
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
						}
						// Activity state intentionally tracks visible terminal mutations only.
						// Noise-only chunks are filtered above and must not update activity tags.
						// We still run this to clear pending visible state when no mutation occurred.
						tagSessionName, tagTimestamp, _ = m.noteVisibleActivityLockedWithOutput(tab, hasMoreBuffered, visibleSeq, filtered)
						catchUpBefore, catchUpAfter := tab.settlePTYBytesLocked(processedBytes)
						suppressRedraw = catchUpBefore && catchUpAfter
					}
					tab.mu.Unlock()
					if !suppressRedraw {
						cmds = append(cmds, m.scheduleChatCursorRefresh(tab, msg.WorkspaceID, time.Now()))
					}
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
			catchUpStillActive := tab.catchUpActiveLocked()
			tab.mu.Unlock()
			if len(tab.pendingOutput) == 0 {
				tab.pendingOutput = tab.pendingOutput[:0]
				tab.mu.Lock()
				tab.pendingOutputBytes = 0
				tab.mu.Unlock()
			} else {
				tab.flushScheduled = true
				tab.flushPendingSince = time.Now()
				tabID := msg.TabID
				quietNext, _ := m.flushTiming(tab, isActive)
				delay := quietNext
				if delay < time.Millisecond {
					delay = time.Millisecond
				}
				cmds = append(cmds, common.SafeTick(delay, func(t time.Time) tea.Msg {
					return PTYFlush{WorkspaceID: msg.WorkspaceID, TabID: tabID, CatchUp: catchUpStillActive}
				}))
			}
		}
	}
	return common.SafeBatch(cmds...)
}
