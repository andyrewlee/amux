package center

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
	"github.com/andyrewlee/amux/internal/vterm"
)

// updatePTYOutput handles PTYOutput.
func (m *Model) updatePTYOutput(msg PTYOutput) tea.Cmd {
	var cmds []tea.Cmd
	// Output read before a workspace rebind but delivered after it carries the
	// old ID; the exact-key lookup would miss and silently drop the payload —
	// a hole in the byte stream. Resolve like PTYFlush does.
	tab, wsID := m.resolveTabForResult(msg.WorkspaceID, msg.TabID, "PTYOutput")
	if tab != nil && !tab.isClosed() {
		msg.WorkspaceID = wsID
		var stale bool
		withMu(&tab.mu, func() {
			stale = tab.State.ReaderGen != msg.Gen
		})
		if stale {
			logging.Debug("Dropping stale PTYOutput for tab %s: a newer reader already runs", msg.TabID)
			return nil
		}
		m.tracePTYOutput(tab, msg.Data)
		if detachCmd, consumed := m.handleBackgroundPTYPressure(msg, tab); consumed {
			return detachCmd
		}
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
					withMu(&tab.mu, func() {
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
					})
				} else {
					withMu(&tab.mu, func() {
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
					})
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
		isChat := m.isChatTab(tab)
		var activityData []byte
		activitySeedFromTrim := false
		if isChat {
			// The activityData slicing exists only for the chat-tab
			// ANSI-activity tracker — skip it for non-chat tabs.
			activityData = data
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
				// The trim boundary is always parser-clean: retained bytes
				// begin in text (TrimOverflow only cuts at text/ESC-safe
				// points, or drops everything and carries the in-flight
				// sequence into the next append). Seed the new-bytes scan
				// from text instead of rescanning the retained prefix (up
				// to the full buffer cap) to reconstruct the activity state
				// at activityData's interior start. When retained old bytes
				// do sit ahead of activityData the true interior state may
				// differ — the error only over-counts visibility
				// (mid-sequence bytes read as text), the safe direction for
				// an activity tracker.
				perf.Count("pty_overflow_carry_seed", 1)
				activitySeedFromTrim = true
			}
		}
		perf.Count("pty_output_bytes", int64(len(msg.Data)))
		now := time.Now()
		tab.LastOutputAt = now
		if isChat {
			withMu(&tab.mu, func() {
				if tab.bootstrapActivity &&
					!tab.bootstrapLastOutputAt.IsZero() &&
					now.Sub(tab.bootstrapLastOutputAt) >= bootstrapQuietGap {
					tab.bootstrapActivity = false
					tab.bootstrapLastOutputAt = time.Time{}
				}
				if activitySeedFromTrim {
					tab.activityANSIState = ansiActivityText
				}
			})
			hasVisibleOutput := tab.consumeActivityVisibility(activityData)
			if hasVisibleOutput {
				withMu(&tab.mu, func() {
					tab.pendingVisibleOutput = true
					tab.pendingVisibleSeq++
				})
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

// handleBackgroundPTYPressure releases a hidden live view when its buffered
// output has remained above the pressure threshold. This is deliberately based
// on backlog rather than whether the agent itself is busy: detaching closes
// only amux's tmux client, while the tmux session and agent keep running.
//
// consumed reports that msg must not be appended. Besides a pressure detach,
// it also discards PTYOutput messages that were already queued when an earlier
// detach closed the reader.
func (m *Model) handleBackgroundPTYPressure(msg PTYOutput, tab *Tab) (detachCmd tea.Cmd, consumed bool) {
	if tab == nil {
		return nil, false
	}

	isActive := m.isActiveTab(msg.WorkspaceID, msg.TabID)
	now := time.Now()
	var (
		projectedBacklog int
		pressureSince    time.Time
		attached         bool
		sessionName      string
	)
	earlyConsumed := false
	earlyPass := false
	withMu(&tab.mu, func() {
		if tab.Detached && tab.discardDetachedPTYOutput {
			earlyConsumed = true
			return
		}
		if isActive {
			tab.backgroundPTYPressureSince = time.Time{}
			earlyPass = true
			return
		}

		projectedBacklog = len(tab.PendingOutput) + len(msg.Data)
		if projectedBacklog < ptyBackgroundDetachThreshold {
			tab.backgroundPTYPressureSince = time.Time{}
			earlyPass = true
			return
		}
		if tab.backgroundPTYPressureSince.IsZero() {
			tab.backgroundPTYPressureSince = now
		}
		pressureSince = tab.backgroundPTYPressureSince
		attached = tab.Running && tab.Agent != nil
		sessionName = tab.SessionName
		if sessionName == "" && tab.Agent != nil {
			sessionName = tab.Agent.Session
		}
	})
	if earlyConsumed {
		return nil, true
	}
	if earlyPass {
		return nil, false
	}

	// A single exceptionally large read must not pass through to the overflow
	// path merely to wait out the grace period.
	emergency := projectedBacklog >= ptyMaxBufferedBytes
	if !attached || sessionName == "" || (!emergency && now.Sub(pressureSince) < ptyBackgroundDetachGrace) {
		return nil, false
	}
	if !m.isChatTab(tab) {
		return nil, false
	}

	for index, candidate := range m.tabs.ByWorkspace[msg.WorkspaceID] {
		if candidate != tab {
			continue
		}
		logging.Warn(
			"detaching background tab %s after sustained PTY backlog: buffered %d bytes (agent continues in tmux session %s)",
			tab.ID,
			projectedBacklog,
			sessionName,
		)
		perf.Count("pty_background_pressure_detach", 1)
		detachCmd := m.detachTab(tab, index)
		if detachCmd == nil {
			return nil, false
		}
		withMu(&tab.mu, func() {
			tab.discardDetachedPTYOutput = true
		})
		return detachCmd, true
	}
	return nil, false
}

// updatePTYFlush handles PTYFlush.
func (m *Model) updatePTYFlush(msg PTYFlush) tea.Cmd {
	var cmds []tea.Cmd
	// A flush tick stamped before a workspace rebind carries the old ID; the
	// exact-key lookup would miss and leave FlushScheduled latched forever.
	tab, wsID := m.resolveTabForResult(msg.WorkspaceID, msg.TabID, "PTYFlush")
	if tab != nil && !tab.isClosed() {
		msg.WorkspaceID = wsID
		isActive := m.isActiveTab(msg.WorkspaceID, msg.TabID)
		var catchUp bool
		withMu(&tab.mu, func() {
			if !isActive {
				tab.clearCatchUpLocked()
			} else {
				tab.expireCatchUpLocked()
			}
			catchUp = isActive && tab.catchUpActiveLocked()
		})
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
			withMu(&tab.mu, func() {
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
			})
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
				withMu(&tab.mu, func() {
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
				})
			}
			if writeOutput && len(chunk) > 0 {
				cmds = append(cmds, m.dispatchFlushChunk(tab, msg, chunk, hasMoreBuffered, visibleSeq, catchUp)...)
			}
			var catchUpStillActive bool
			withMu(&tab.mu, func() {
				catchUpStillActive = tab.catchUpActiveLocked()
			})
			if tab.State.RearmFlush(time.Now(), func() {
				withMu(&tab.mu, func() {
					tab.pendingOutputBytes = 0
				})
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
	if m.isTabActorReady() {
		cmds, tagSessionName, _ = m.dispatchFlushChunkViaActor(tab, msg, chunk, hasMoreBuffered, visibleSeq, catchUp)
	} else {
		var cmd tea.Cmd
		cmd, tagSessionName, _ = m.applyFlushChunkSync(tab, msg.WorkspaceID, chunk, hasMoreBuffered, visibleSeq, false)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if tagSessionName != "" {
		m.markActivityTagForFlush(tagSessionName)
	}
	return cmds
}

// dispatchFlushChunkViaActor enqueues the chunk and sends it to the tab actor.
// A successful send returns no commands (the actor applies it asynchronously).
// A failed send is rolled back; if the rollback leaves the chunk to apply, it is
// applied synchronously here. Returns any cursor-refresh command and the activity
// tag to publish.
func (m *Model) dispatchFlushChunkViaActor(tab *Tab, msg PTYFlush, chunk []byte, hasMoreBuffered bool, visibleSeq uint64, catchUp bool) (cmds []tea.Cmd, tagSessionName string, tagTimestamp int64) {
	prevEpoch, prevCarry, prevNoiseTrailing, filtered, noiseAfter := enqueueActorWrite(tab, chunk)
	if m.sendTabEvent(tabEvent{
		tab:             tab,
		workspaceID:     msg.WorkspaceID,
		tabID:           msg.TabID,
		kind:            tabEventWriteOutput,
		output:          chunk,
		filteredOutput:  filtered,
		noiseAfter:      noiseAfter,
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
	withMu(&tab.mu, func() {
		if tab.Terminal != nil {
			// applyPTYChunkLocked emits the per-flush processed/filtered counters.
			_, suppressRedraw, tagSessionName, tagTimestamp = m.applyPTYChunkLocked(tab, chunk, hasMoreBuffered, visibleSeq)
			if updateActorCarry {
				tab.actorQueuedCarry = tab.Terminal.ParserCarryState()
				tab.actorQueuedNoiseTrailing = append(tab.actorQueuedNoiseTrailing[:0], tab.NoiseTrailing...)
			}
		}
	})
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
	if !hasMoreBuffered {
		// Stream paused at the flush boundary: release a held `name(N)`
		// tail — far more likely a real prompt than a split diagnostic.
		filteredLen += tab.State.FlushNoiseTrailingLocked(tab.Terminal.Write)
	}
	// Activity state intentionally tracks visible terminal mutations only.
	// Noise-only chunks are filtered above and must not update activity tags.
	// We still run this to clear pending visible state when no mutation occurred.
	tagSessionName, tagTimestamp, _ = m.noteVisibleActivityLockedWithOutput(tab, hasMoreBuffered, visibleSeq, filtered)
	catchUpBefore, catchUpAfter := tab.settlePTYBytesLocked(len(chunk))
	suppressRedraw = catchUpBefore && catchUpAfter
	return filteredLen, suppressRedraw, tagSessionName, tagTimestamp
}
