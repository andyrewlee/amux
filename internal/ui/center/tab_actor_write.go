package center

import (
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/safego"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
	"github.com/andyrewlee/amux/internal/vterm"
)

func (m *Model) handleWriteOutput(ev tabEvent) {
	tab := ev.tab
	processedBytes := len(ev.output)
	var (
		tagSessionName string
		filteredLen    int
		filterApplied  bool
		requestFlush   bool
		suppressRedraw bool
		pendingClip    []byte
		pendingBell    bool
		initialTask    string
		taskGen        uint64
	)
	staleWrite := false
	withMu(&tab.mu, func() {
		staleWrite = ev.writeEpoch != tab.actorWriteEpoch
		if !staleWrite && tab.Terminal != nil {
			// The breadcrumb lets reconcileActorEventPanic tell whether this
			// event's apply began (accounting consumed at apply top) or the
			// event died before reaching it (accounting still owned). It
			// stays set through ALL post-apply work below — admission,
			// msgSink, clip drain — and is cleared by this handler's last
			// statement: a panic in the post-apply window must still read
			// "accounting consumed", or reconcile decrements a second time.
			tab.actorApplyInFlight = true
			filteredLen, filterApplied, suppressRedraw, requestFlush, tagSessionName, _, pendingClip, pendingBell, initialTask, taskGen = m.applyActorWriteLocked(tab, ev, processedBytes)
		}
	})
	if staleWrite {
		return
	}
	if tab.testPostApplyHook != nil {
		tab.testPostApplyHook()
	}
	if initialTask != "" {
		// First write after the agent emitted a private-mode set: the input
		// loop is live, so the queued launch task goes through the same input
		// FIFO a user keystroke takes. The admission pins the generation
		// captured beside the task so a reattach since then cannot deliver
		// the old stream's task to the new agent. A full queue is the only
		// retryable rejection — re-arm so a later write pass admits it, and
		// only while the same binding still owns it; a failed, replaced, or
		// missing binding drops it like a detached tab would.
		res, _ := m.admitTabInputBound(tab, ev.workspaceID, taskGen, initialTask+"\r", "InitialTask", true)
		if res == tabInputRejectedFull {
			withMu(&tab.mu, func() {
				if tab.tabInput.gen == taskGen && !tab.isClosed() && !tab.Detached && tab.Terminal != nil {
					tab.pendingInitialTask = initialTask
				}
			})
		}
	}
	if pendingBell && m.msgSink != nil {
		m.msgSink(TabBell{WorkspaceID: ev.workspaceID, TabID: ev.tabID})
	}
	perf.Count("pty_flush_bytes_processed", int64(processedBytes))
	if filterApplied {
		filteredBytes := processedBytes - filteredLen
		if filteredBytes > 0 {
			perf.Count("pty_flush_bytes_filtered", int64(filteredBytes))
		}
	}
	if tagSessionName != "" {
		m.markActivityTagForFlush(tagSessionName)
	}
	if clip, ok := common.OSC52ClipboardText(pendingClip); ok {
		safego.Go("center.osc52_clipboard", func() {
			common.CopyToClipboardWithLog(clip, "agent OSC52")
		})
	}
	if requestFlush && m.msgSink != nil {
		m.msgSink(PTYFlush{WorkspaceID: ev.workspaceID, TabID: ev.tabID, CatchUp: ev.catchUp})
	}
	if !suppressRedraw && m.msgSink != nil && m.shouldPostWriteRedraw(tab) {
		// Only the visible tab needs a redraw after actor-applied terminal writes.
		// Inactive tabs retain their vterm state and activity bookkeeping, but a
		// redraw would rebuild an identical app frame.
		m.msgSink(PTYCursorRefresh{WorkspaceID: ev.workspaceID, TabID: ev.tabID})
	}
	// Last statement: the apply breadcrumb must cover the whole post-apply
	// window, so it clears here under the mutex like its other accesses. NOT
	// a defer: a deferred clear would run during unwind before the recover's
	// reconcile and reopen the double-decrement this flag exists to close.
	// Only this event's apply can have set it — a stale-write return and a
	// Terminal==nil path never reach the set.
	withMu(&tab.mu, func() { tab.actorApplyInFlight = false })
}

// applyActorWriteLocked writes an actor-queued output chunk to the terminal,
// updating visible-activity state, the actor pending/queued accounting, and
// catch-up settlement, and emits the inside-lock flush counters. The caller
// holds tab.mu and has verified tab.Terminal != nil. It returns the filtered
// byte count, whether the filter ran, whether the redraw should be suppressed,
// whether a follow-up flush is needed, the activity tag to publish, and any
// clipboard payload captured from an OSC 52 write (to be drained off the lock)
// plus the pending-bell flag for the same off-lock drain.
func (m *Model) applyActorWriteLocked(tab *Tab, ev tabEvent, processedBytes int) (filteredLen int, filterApplied, suppressRedraw, requestFlush bool, tagSessionName string, tagTimestamp int64, pendingClip []byte, pendingBell bool, initialTask string, taskGen uint64) {
	// Consume the enqueue accounting first: once the apply starts, this event
	// owns nothing — reconcileActorEventPanic treats "apply started" as
	// settled, so a panic mid-write cannot leak a phantom pending count.
	if tab.actorWritesPending > 0 {
		tab.actorWritesPending--
	}
	if tab.actorQueuedBytes >= processedBytes {
		tab.actorQueuedBytes -= processedBytes
	} else {
		tab.actorQueuedBytes = 0
	}
	if tab.testApplyHook != nil {
		tab.testApplyHook()
	}
	// The enqueue preview already ran the noise filter against the queued
	// carry chain, and every non-actor mutation of NoiseTrailing coincides with
	// an actorWriteEpoch bump (stale-drop above) or happens while no write is
	// pending — so the carried filtered bytes and post-filter trailing are
	// exactly what re-filtering here would produce. Committing them directly
	// removes a second full filter pass over the same chunk.
	output := ev.filteredOutput
	tab.NoiseTrailing = ev.noiseAfter
	filteredLen = len(output)
	filterApplied = true
	if len(output) > 0 {
		flushDone := perf.Time("pty_flush")
		tab.Terminal.Write(output)
		flushDone()
		perf.Count("pty_flush_bytes", int64(len(output)))
	}
	pendingClip = tab.Terminal.TakePendingClipboard()
	pendingBell = tab.Terminal.TakePendingBell()
	if tab.pendingInitialTask != "" && tab.Terminal.PrivateModesSeen() {
		initialTask = tab.pendingInitialTask
		// The binding's input generation is captured beside the task so the
		// admission off this lock can pin it: a reattach between here and the
		// admit call must not deliver this stream's task to the new agent.
		taskGen = tab.tabInput.gen
		tab.pendingInitialTask = ""
	}
	// Activity state intentionally tracks visible terminal mutations only.
	// Noise-only chunks are filtered above and must not update activity tags.
	tagSessionName, tagTimestamp, _ = m.noteVisibleActivityLockedWithOutput(tab, ev.hasMoreBuffered, ev.visibleSeq, output)
	catchUpBefore, catchUpAfter := tab.settlePTYBytesLocked(processedBytes)
	suppressRedraw = catchUpBefore && catchUpAfter
	if tab.actorWritesPending == 0 {
		requestFlush = finalizeActorWriteLocked(tab)
	}
	return filteredLen, filterApplied, suppressRedraw, requestFlush, tagSessionName, tagTimestamp, pendingClip, pendingBell, initialTask, taskGen
}

// enqueueActorWrite optimistically advances the actor-write accounting for a
// chunk about to be dispatched to the tab actor: it captures the prior epoch,
// queued carry, and queued noise trailing (returned so a failed send can roll
// back), advances the queued parser carry/noise across the chunk, and bumps the
// pending/queued counters. It also returns the filtered bytes and post-filter
// noise trailing so the caller can carry them on the event — the apply then
// commits them instead of filtering the chunk a second time. previewFiltered
// may alias chunk; previewTrailing is owned by the caller (the queued copy is
// stored separately). It manages tab.mu itself and makes no mutation when
// tab.Terminal == nil (the symmetric apply side bails the same way).
func enqueueActorWrite(tab *Tab, chunk []byte) (prevEpoch uint64, prevCarry vterm.ParserCarryState, prevNoiseTrailing, previewFiltered, previewTrailing []byte) {
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if tab.Terminal == nil {
		return 0, vterm.ParserCarryState{}, nil, nil, nil
	}
	prevPending := tab.actorWritesPending
	prevEpoch = tab.actorWriteEpoch
	prevCarry = tab.actorQueuedCarry
	prevNoiseTrailing = append(prevNoiseTrailing, tab.actorQueuedNoiseTrailing...)
	seedCarry := tab.Terminal.ParserCarryState()
	if prevPending > 0 {
		seedCarry = prevCarry
	}
	previewTrailing = append([]byte(nil), tab.NoiseTrailing...)
	if prevPending > 0 {
		previewTrailing = append(previewTrailing[:0], tab.actorQueuedNoiseTrailing...)
	}
	previewFiltered = ptyio.FilterKnownPTYNoiseStream(chunk, &previewTrailing)
	nextCarry := vterm.AdvanceParserCarryState(seedCarry, previewFiltered)
	tab.actorWritesPending = prevPending + 1
	tab.actorQueuedBytes += len(chunk)
	tab.actorQueuedCarry = nextCarry
	tab.actorQueuedNoiseTrailing = append(tab.actorQueuedNoiseTrailing[:0], previewTrailing...)
	return prevEpoch, prevCarry, prevNoiseTrailing, previewFiltered, previewTrailing
}

// recoverFailedActorSend rolls back an actor-write enqueue when sendTabEvent
// could not deliver the chunk. It undoes the pending/queued bump and decides what
// the caller should do: rebuffer the chunk ahead of pendingOutput (another write
// is still in flight), drop it (epoch advanced or tab closed), or fall back to a
// synchronous apply (optionally truncated to the active chunk size in catch-up).
// It manages tab.mu itself and returns the chunk to apply synchronously, the
// (possibly updated) hasMoreBuffered, and whether the write was rebuffered or
// dropped (either of which means the caller must not apply it).
func recoverFailedActorSend(
	tab *Tab,
	chunk []byte,
	prevEpoch uint64,
	prevCarry vterm.ParserCarryState,
	prevNoiseTrailing []byte,
	catchUp bool,
	hasMoreBuffered bool,
) (chunkToApply []byte, hasMore, rebuffered, dropWrite bool) {
	hasMore = hasMoreBuffered
	syncFallbackChunkSize := 0
	withMu(&tab.mu, func() {
		switch {
		case tab.actorWriteEpoch == prevEpoch && tab.actorWritesPending > 0:
			tab.actorWritesPending--
			if tab.actorQueuedBytes >= len(chunk) {
				tab.actorQueuedBytes -= len(chunk)
			} else {
				tab.actorQueuedBytes = 0
			}
			switch {
			case tab.isClosed():
				dropWrite = true
			case tab.actorWritesPending > 0:
				rebuffered = true
				tab.restoreActorCarryLocked(prevCarry, prevNoiseTrailing)
				tab.prependPendingOutputLocked(chunk)
			case catchUp && len(chunk) > ptyFlushChunkSizeActive:
				syncFallbackChunkSize = ptyFlushChunkSizeActive
				tab.restoreActorCarryLocked(prevCarry, prevNoiseTrailing)
				tab.prependPendingOutputLocked(chunk[syncFallbackChunkSize:])
				hasMore = len(tab.PendingOutput) > 0
			}
		case tab.actorWriteEpoch != prevEpoch || tab.isClosed():
			dropWrite = true
		}
	})
	chunkToApply = chunk
	if syncFallbackChunkSize > 0 {
		chunkToApply = chunk[:syncFallbackChunkSize]
	}
	return chunkToApply, hasMore, rebuffered, dropWrite
}

// reconcileActorEventPanic undoes the enqueue accounting of a write event that
// died to a panic before its apply could consume it. An epoch-bumped (stale)
// write was already settled by the bump, and an apply that started consumed
// the accounting at apply top — actorApplyInFlight marks that transition and
// stays set through the whole post-apply window (cleared by the handler's
// last statement), so a post-apply panic also reads "accounting consumed"
// rather than decrementing a second time. Runs on the actor goroutine from
// the per-event recover.
func (m *Model) reconcileActorEventPanic(ev tabEvent) {
	if ev.kind != tabEventWriteOutput || ev.tab == nil {
		return
	}
	withMu(&ev.tab.mu, func() {
		if ev.tab.actorApplyInFlight {
			// The apply started: it consumed this event's accounting (and died
			// holding the flag, since a clean return clears it).
			ev.tab.actorApplyInFlight = false
			return
		}
		if ev.writeEpoch != ev.tab.actorWriteEpoch || ev.tab.Terminal == nil {
			return
		}
		if ev.tab.actorWritesPending > 0 {
			ev.tab.actorWritesPending--
		}
		if ev.tab.actorQueuedBytes >= len(ev.output) {
			ev.tab.actorQueuedBytes -= len(ev.output)
		} else {
			ev.tab.actorQueuedBytes = 0
		}
	})
}

// restoreActorCarryLocked restores the actor queued parser carry / noise trailing
// to a previously captured state (used when rolling back a failed actor send).
// Caller holds t.mu.
func (t *Tab) restoreActorCarryLocked(prevCarry vterm.ParserCarryState, prevNoiseTrailing []byte) {
	t.actorQueuedCarry = prevCarry
	t.actorQueuedNoiseTrailing = append(t.actorQueuedNoiseTrailing[:0], prevNoiseTrailing...)
}

// prependPendingOutputLocked restores chunk to the front of pendingOutput so a
// rolled-back write is re-flushed before newer buffered output. Caller holds
// t.mu.
func (t *Tab) prependPendingOutputLocked(chunk []byte) {
	restored := make([]byte, 0, len(chunk)+len(t.PendingOutput))
	restored = append(restored, chunk...)
	restored = append(restored, t.PendingOutput...)
	t.PendingOutput = restored
	t.pendingOutputBytes = len(t.PendingOutput)
}

// finalizeActorWriteLocked snapshots the parser carry/noise state after the last
// queued actor write drains, applying a deferred parser reset if one is pending.
// Returns true when a follow-up flush should be requested. Caller holds tab.mu.
func finalizeActorWriteLocked(tab *Tab) (requestFlush bool) {
	tab.actorQueuedCarry = tab.Terminal.ParserCarryState()
	tab.actorQueuedNoiseTrailing = append(tab.actorQueuedNoiseTrailing[:0], tab.NoiseTrailing...)
	if tab.parserResetPending {
		tab.Terminal.ResetParserState()
		tab.activityANSIState = ansiActivityText
		tab.NoiseTrailing = nil
		tab.actorQueuedCarry = tab.Terminal.ParserCarryState()
		tab.actorQueuedNoiseTrailing = tab.actorQueuedNoiseTrailing[:0]
		tab.parserResetPending = false
		requestFlush = true
	}
	return requestFlush
}
