package center

import (
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/common"
)

func (m *Model) popPTYFlushChunk(tab *Tab, active bool) (chunk []byte, hasMoreBuffered bool, visibleSeq uint64) {
	if tab == nil {
		return nil, false, 0
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if tab.Terminal == nil || tab.pendingOutput.Len() == 0 {
		return nil, false, 0
	}
	chunkSize := tab.pendingOutput.Len()
	maxChunk := ptyFlushChunkSize
	if active {
		maxChunk = ptyFlushChunkSizeActive
	}
	if chunkSize > maxChunk {
		chunkSize = maxChunk
	}
	chunk = tab.pendingOutput.Pop(chunkSize)
	hasMoreBuffered = tab.pendingOutput.Len() > 0
	visibleSeq = tab.pendingVisibleSeq
	return chunk, hasMoreBuffered, visibleSeq
}

func (m *Model) writePTYFlushChunk(
	tab *Tab,
	workspaceID string,
	tabID TabID,
	chunk []byte,
	hasMoreBuffered bool,
	visibleSeq uint64,
) (string, int64) {
	if tab == nil || len(chunk) == 0 {
		return "", 0
	}

	if m.isTabActorReady() {
		if m.sendTabEvent(tabEvent{
			tab:             tab,
			workspaceID:     workspaceID,
			tabID:           tabID,
			kind:            tabEventWriteOutput,
			output:          chunk,
			hasMoreBuffered: hasMoreBuffered,
			visibleSeq:      visibleSeq,
		}) {
			return "", 0
		}
	}

	processedBytes := len(chunk)
	filteredLen := 0
	filterApplied := false
	tagSessionName := ""
	var tagTimestamp int64

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
	return tagSessionName, tagTimestamp
}
