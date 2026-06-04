package center

import (
	"strconv"

	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/safego"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
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
	)
	tab.mu.Lock()
	staleWrite := ev.writeEpoch != tab.actorWriteEpoch
	if !staleWrite && tab.Terminal != nil {
		filteredLen, filterApplied, suppressRedraw, requestFlush, tagSessionName, tagTimestamp = m.applyActorWriteLocked(tab, ev, processedBytes)
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
// whether a follow-up flush is needed, and the activity tag to publish.
func (m *Model) applyActorWriteLocked(tab *Tab, ev tabEvent, processedBytes int) (filteredLen int, filterApplied, suppressRedraw, requestFlush bool, tagSessionName string, tagTimestamp int64) {
	output := common.FilterKnownPTYNoiseStream(ev.output, &tab.ptyNoiseTrailing)
	filteredLen = len(output)
	filterApplied = true
	if len(output) > 0 {
		flushDone := perf.Time("pty_flush")
		tab.Terminal.Write(output)
		flushDone()
		perf.Count("pty_flush_bytes", int64(len(output)))
	}
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
	return filteredLen, filterApplied, suppressRedraw, requestFlush, tagSessionName, tagTimestamp
}

// finalizeActorWriteLocked snapshots the parser carry/noise state after the last
// queued actor write drains, applying a deferred parser reset if one is pending.
// Returns true when a follow-up flush should be requested. Caller holds tab.mu.
func finalizeActorWriteLocked(tab *Tab) (requestFlush bool) {
	tab.actorQueuedCarry = tab.Terminal.ParserCarryState()
	tab.actorQueuedNoiseTrailing = append(tab.actorQueuedNoiseTrailing[:0], tab.ptyNoiseTrailing...)
	if tab.parserResetPending {
		tab.Terminal.ResetParserState()
		tab.activityANSIState = ansiActivityText
		tab.ptyNoiseTrailing = nil
		tab.actorQueuedCarry = tab.Terminal.ParserCarryState()
		tab.actorQueuedNoiseTrailing = tab.actorQueuedNoiseTrailing[:0]
		tab.parserResetPending = false
		requestFlush = true
	}
	return requestFlush
}
