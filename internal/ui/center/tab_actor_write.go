package center

import (
	"strconv"

	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/safego"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
	"github.com/andyrewlee/amux/internal/vterm"
)

func (m *Model) handleWriteOutput(ev tabEvent) {
	tab := ev.tab
	processedBytes := len(ev.output)
	var (
		tagSessionName string
		tagTimestamp   int64
		filteredLen    int
		filterApplied  bool
		requestFlush   bool
		suppressRedraw bool
		pendingClip    []byte
	)
	tab.mu.Lock()
	staleWrite := ev.writeEpoch != tab.actorWriteEpoch
	if !staleWrite && tab.Terminal != nil {
		filteredLen, filterApplied, suppressRedraw, requestFlush, tagSessionName, tagTimestamp, pendingClip = m.applyActorWriteLocked(tab, ev, processedBytes)
	}
	tab.mu.Unlock()
	if staleWrite {
		return
	}
	perf.Count("pty_flush_bytes_processed", int64(processedBytes))
	if filterApplied {
		filteredBytes := processedBytes - filteredLen
		if filteredBytes > 0 {
			perf.Count("pty_flush_bytes_filtered", int64(filteredBytes))
		}
	}
	if tagSessionName != "" {
		opts := m.tmuxOpts
		sessionName := tagSessionName
		timestamp := strconv.FormatInt(tagTimestamp, 10)
		safego.Go("center.tmux_tag_write", func() {
			_ = tmux.SetSessionTagValue(sessionName, tmux.TagLastOutputAt, timestamp, opts)
		})
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
		// Visible tabs need one redraw after actor-applied terminal writes.
		m.msgSink(PTYCursorRefresh{WorkspaceID: ev.workspaceID, TabID: ev.tabID})
	}
}

// applyActorWriteLocked writes an actor-queued output chunk to the terminal,
// updating visible-activity state, the actor pending/queued accounting, and
// catch-up settlement, and emits the inside-lock flush counters. The caller
// holds tab.mu and has verified tab.Terminal != nil. It returns the filtered
// byte count, whether the filter ran, whether the redraw should be suppressed,
// whether a follow-up flush is needed, the activity tag to publish, and any
// clipboard payload captured from an OSC 52 write (to be drained off the lock).
func (m *Model) applyActorWriteLocked(tab *Tab, ev tabEvent, processedBytes int) (filteredLen int, filterApplied, suppressRedraw, requestFlush bool, tagSessionName string, tagTimestamp int64, pendingClip []byte) {
	output := ptyio.FilterKnownPTYNoiseStream(ev.output, &tab.NoiseTrailing)
	filteredLen = len(output)
	filterApplied = true
	if len(output) > 0 {
		flushDone := perf.Time("pty_flush")
		tab.Terminal.Write(output)
		flushDone()
		perf.Count("pty_flush_bytes", int64(len(output)))
	}
	pendingClip = tab.Terminal.TakePendingClipboard()
	// Activity state intentionally tracks visible terminal mutations only.
	// Noise-only chunks are filtered above and must not update activity tags.
	tagSessionName, tagTimestamp, _ = m.noteVisibleActivityLockedWithOutput(tab, ev.hasMoreBuffered, ev.visibleSeq, output)
	if tab.actorWritesPending > 0 {
		tab.actorWritesPending--
	}
	if tab.actorQueuedBytes >= processedBytes {
		tab.actorQueuedBytes -= processedBytes
	} else {
		tab.actorQueuedBytes = 0
	}
	catchUpBefore, catchUpAfter := tab.settlePTYBytesLocked(processedBytes)
	suppressRedraw = catchUpBefore && catchUpAfter
	if tab.actorWritesPending == 0 {
		requestFlush = finalizeActorWriteLocked(tab)
	}
	return filteredLen, filterApplied, suppressRedraw, requestFlush, tagSessionName, tagTimestamp, pendingClip
}

// enqueueActorWrite optimistically advances the actor-write accounting for a
// chunk about to be dispatched to the tab actor: it captures the prior epoch,
// queued carry, and queued noise trailing (returned so a failed send can roll
// back), advances the queued parser carry/noise across the chunk, and bumps the
// pending/queued counters. It manages tab.mu itself and makes no mutation when
// tab.Terminal == nil (the symmetric apply side bails the same way).
func enqueueActorWrite(tab *Tab, chunk []byte) (prevEpoch uint64, prevCarry vterm.ParserCarryState, prevNoiseTrailing []byte) {
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if tab.Terminal == nil {
		return 0, vterm.ParserCarryState{}, nil
	}
	prevPending := tab.actorWritesPending
	prevEpoch = tab.actorWriteEpoch
	prevCarry = tab.actorQueuedCarry
	prevNoiseTrailing = append(prevNoiseTrailing, tab.actorQueuedNoiseTrailing...)
	seedCarry := tab.Terminal.ParserCarryState()
	if prevPending > 0 {
		seedCarry = prevCarry
	}
	previewTrailing := append([]byte(nil), tab.NoiseTrailing...)
	if prevPending > 0 {
		previewTrailing = append(previewTrailing[:0], tab.actorQueuedNoiseTrailing...)
	}
	filteredPreview := ptyio.FilterKnownPTYNoiseStream(chunk, &previewTrailing)
	nextCarry := vterm.AdvanceParserCarryState(seedCarry, filteredPreview)
	nextNoiseTrailing := append([]byte(nil), previewTrailing...)
	tab.actorWritesPending = prevPending + 1
	tab.actorQueuedBytes += len(chunk)
	tab.actorQueuedCarry = nextCarry
	tab.actorQueuedNoiseTrailing = append(tab.actorQueuedNoiseTrailing[:0], nextNoiseTrailing...)
	return prevEpoch, prevCarry, prevNoiseTrailing
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
	tab.mu.Lock()
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
	tab.mu.Unlock()
	chunkToApply = chunk
	if syncFallbackChunkSize > 0 {
		chunkToApply = chunk[:syncFallbackChunkSize]
	}
	return chunkToApply, hasMore, rebuffered, dropWrite
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
