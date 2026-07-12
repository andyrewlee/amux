package center

import (
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
	"github.com/andyrewlee/amux/internal/vterm"
)

// updatePTYOutput handles PTYOutput.
func (m *Model) updatePTYOutput(msg PTYOutput) tea.Cmd {
	var cmds []tea.Cmd
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab != nil && !tab.isClosed() {
		m.tracePTYOutput(tab, msg.Data)
		// resetNow bridges the actor-aware trim seed (SeedForTrim) to the
		// overflow noise-reset accounting (OnOverflowLocked): both run inside
		// AppendOutput and both need to know whether the terminal parser was
		// reset while computing the seed.
		resetNow := false
		res := tab.State.AppendOutput(&tab.mu, msg.Data, ptyMaxBufferedBytes, ptyio.OutputHooks{
			OnCarryConsumed: func() {
				tab.activityANSIState = ansiActivityText
			},
			AfterAppendLocked: func(appendedLen int) {
				tab.pendingOutputBytes = len(tab.PendingOutput)
				tab.ptyBytesReceived += uint64(appendedLen)
			},
			SeedForTrim: func() vterm.ParserCarryState {
				seed := vterm.ParserCarryState{}
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
							tab.NoiseTrailing = nil
						} else {
							seed = tab.Terminal.ParserCarryState()
						}
						tab.Terminal.ResetParserState()
						resetNow = true
					}
					tab.mu.Unlock()
				}
				return seed
			},
			OnOverflowLocked: func(overflow, retainedStart, prevPendingLen int) {
				tab.pendingOutputBytes = len(tab.PendingOutput)
				tab.settlePTYBytesLocked(overflow)
				if resetNow && retainedStart > prevPendingLen {
					tab.NoiseTrailing = nil
					tab.actorQueuedNoiseTrailing = tab.actorQueuedNoiseTrailing[:0]
				}
			},
			LogOverflow: func(droppedTotal int) {
				logging.Warn("PTY output overflow for tab %s: dropped %d bytes (buffer cap %d)", tab.ID, droppedTotal, ptyMaxBufferedBytes)
			},
			DropBytesCounter: "pty_output_drop_bytes",
			DropCounter:      "pty_output_drop",
		})
		data := res.Data
		activityData := data
		activityState := ansiActivityText
		activityStateSet := false
		if res.Overflowed {
			chunkStart := res.PrevPendingLen
			if res.RetainedStart > chunkStart {
				dropFromMsg := res.RetainedStart - chunkStart
				if dropFromMsg >= len(data) {
					activityData = nil
				} else {
					activityData = data[dropFromMsg:]
				}
			}
			activityPrefixLen := len(tab.PendingOutput) - len(activityData)
			if activityPrefixLen < 0 {
				activityPrefixLen = 0
			}
			_, activityState = hasVisiblePTYOutput(tab.PendingOutput[:activityPrefixLen], ansiActivityText)
			activityStateSet = true
		}
		perf.Count("pty_output_bytes", int64(len(msg.Data)))
		now := time.Now()
		tab.LastOutputAt = now
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
		if !tab.FlushScheduled {
			tab.FlushScheduled = true
			tab.FlushPendingSince = tab.LastOutputAt
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
		quiet, maxInterval := m.flushTiming(tab, isActive)
		if delay, deferred := tab.State.FlushGate(time.Now(), quiet, maxInterval); deferred {
			tabID := msg.TabID
			cmds = append(cmds, common.SafeTick(delay, func(t time.Time) tea.Msg {
				return PTYFlush{WorkspaceID: msg.WorkspaceID, TabID: tabID, CatchUp: catchUp}
			}))
			return common.SafeBatch(cmds...)
		}

		if len(tab.PendingOutput) > 0 {
			var chunk []byte
			writeOutput := false
			hasMoreBuffered := false
			visibleSeq := uint64(0)
			parserResetPending := false
			actorWritesPending := 0
			tab.mu.Lock()
			if tab.Terminal != nil {
				parserResetPending = tab.parserResetPending
				actorWritesPending = tab.actorWritesPending
				maxChunk := ptyFlushChunkSize
				if isActive {
					maxChunk = ptyFlushChunkSizeActive
				}
				if catchUp && m.isTabActorReady() {
					if ptyFlushChunkSizeCatchUp > maxChunk {
						maxChunk = ptyFlushChunkSizeCatchUp
					}
				}
				if !parserResetPending {
					chunk = tab.State.TakeFlushChunkLocked(maxChunk)
					if len(chunk) > 0 {
						tab.pendingOutputBytes = len(tab.PendingOutput)
						hasMoreBuffered = len(tab.PendingOutput) > 0
						visibleSeq = tab.pendingVisibleSeq
						writeOutput = true
					}
				}
			}
			tab.mu.Unlock()
			if parserResetPending {
				if actorWritesPending > 0 && m.isTabActorReady() {
					tab.FlushScheduled = true
					tab.FlushPendingSince = time.Now()
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
						tab.NoiseTrailing = nil
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
				cmds = append(cmds, m.dispatchFlushChunk(tab, msg, chunk, hasMoreBuffered, visibleSeq, catchUp)...)
			}
			tab.mu.Lock()
			catchUpStillActive := tab.catchUpActiveLocked()
			tab.mu.Unlock()
			if tab.State.RearmFlush(time.Now(), func() {
				tab.mu.Lock()
				tab.pendingOutputBytes = 0
				tab.mu.Unlock()
			}) {
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

// dispatchFlushChunk delivers a flush chunk either through the tab actor (the
// fast path) or via a synchronous apply, handling actor enqueue, send-failure
// rollback, the synchronous apply, and publishing the last-output activity tag.
// It returns the commands to batch (cursor refresh + tag write).
func (m *Model) dispatchFlushChunk(tab *Tab, msg PTYFlush, chunk []byte, hasMoreBuffered bool, visibleSeq uint64, catchUp bool) []tea.Cmd {
	var cmds []tea.Cmd
	tagSessionName := ""
	var tagTimestamp int64
	if m.isTabActorReady() {
		cmds, tagSessionName, tagTimestamp = m.dispatchFlushChunkViaActor(tab, msg, chunk, hasMoreBuffered, visibleSeq, catchUp)
	} else {
		var cmd tea.Cmd
		cmd, tagSessionName, tagTimestamp = m.applyFlushChunkSync(tab, msg.WorkspaceID, chunk, hasMoreBuffered, visibleSeq, false)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if tagSessionName != "" {
		opts := m.tmuxOpts
		sessionName := tagSessionName
		timestamp := strconv.FormatInt(tagTimestamp, 10)
		cmds = append(cmds, func() tea.Msg {
			_ = tmux.SetSessionTagValue(sessionName, tmux.TagLastOutputAt, timestamp, opts)
			return nil
		})
	}
	return cmds
}

// dispatchFlushChunkViaActor enqueues the chunk and sends it to the tab actor.
// A successful send returns no commands (the actor applies it asynchronously).
// A failed send is rolled back; if the rollback leaves the chunk to apply, it is
// applied synchronously here. Returns any cursor-refresh command and the activity
// tag to publish.
func (m *Model) dispatchFlushChunkViaActor(tab *Tab, msg PTYFlush, chunk []byte, hasMoreBuffered bool, visibleSeq uint64, catchUp bool) (cmds []tea.Cmd, tagSessionName string, tagTimestamp int64) {
	prevEpoch, prevCarry, prevNoiseTrailing := enqueueActorWrite(tab, chunk)
	if m.sendTabEvent(tabEvent{
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
		return nil, "", 0
	}
	var rebuffered, dropWrite bool
	chunk, hasMoreBuffered, rebuffered, dropWrite = recoverFailedActorSend(
		tab, chunk, prevEpoch, prevCarry, prevNoiseTrailing, catchUp, hasMoreBuffered,
	)
	if rebuffered || dropWrite {
		return nil, "", 0
	}
	cmd, tagSessionName, tagTimestamp := m.applyFlushChunkSync(tab, msg.WorkspaceID, chunk, hasMoreBuffered, visibleSeq, true)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}
	return cmds, tagSessionName, tagTimestamp
}

// applyFlushChunkSync writes a flush chunk to the terminal on the UI goroutine
// (the synchronous fallback used when the actor is not ready, or after an actor
// send-failure rollback that leaves the chunk to apply directly). When
// updateActorCarry is set (post-rollback actor path) it also snapshots the actor
// queued carry/noise so a later actor write resumes from the applied state. It
// returns an optional cursor-refresh command and the activity tag to publish.
func (m *Model) applyFlushChunkSync(tab *Tab, workspaceID string, chunk []byte, hasMoreBuffered bool, visibleSeq uint64, updateActorCarry bool) (cmd tea.Cmd, tagSessionName string, tagTimestamp int64) {
	suppressRedraw := false
	tab.mu.Lock()
	if tab.Terminal != nil {
		// applyPTYChunkLocked emits the per-flush processed/filtered counters.
		_, suppressRedraw, tagSessionName, tagTimestamp = m.applyPTYChunkLocked(tab, chunk, hasMoreBuffered, visibleSeq)
		if updateActorCarry {
			tab.actorQueuedCarry = tab.Terminal.ParserCarryState()
			tab.actorQueuedNoiseTrailing = append(tab.actorQueuedNoiseTrailing[:0], tab.NoiseTrailing...)
		}
	}
	tab.mu.Unlock()
	if !suppressRedraw {
		cmd = m.scheduleChatCursorRefresh(tab, workspaceID, time.Now())
	}
	return cmd, tagSessionName, tagTimestamp
}

// applyPTYChunkLocked filters chunk for known PTY noise, writes it to the
// terminal, updates visible-activity state and catch-up accounting, and emits
// the per-flush perf counters. The caller must hold tab.mu and have verified
// tab.Terminal != nil. It returns the filtered byte count, whether the
// post-write redraw should be suppressed, and the activity tag to publish.
func (m *Model) applyPTYChunkLocked(tab *Tab, chunk []byte, hasMoreBuffered bool, visibleSeq uint64) (filteredLen int, suppressRedraw bool, tagSessionName string, tagTimestamp int64) {
	filtered := tab.State.WriteFilteredChunkLocked(tab.Terminal.Write, chunk)
	filteredLen = len(filtered)
	// Activity state intentionally tracks visible terminal mutations only.
	// Noise-only chunks are filtered above and must not update activity tags.
	// We still run this to clear pending visible state when no mutation occurred.
	tagSessionName, tagTimestamp, _ = m.noteVisibleActivityLockedWithOutput(tab, hasMoreBuffered, visibleSeq, filtered)
	catchUpBefore, catchUpAfter := tab.settlePTYBytesLocked(len(chunk))
	suppressRedraw = catchUpBefore && catchUpAfter
	return filteredLen, suppressRedraw, tagSessionName, tagTimestamp
}
