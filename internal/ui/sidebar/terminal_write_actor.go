package sidebar

import (
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/safego"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

// sidebarWriteReq is one unit of writer work for a sidebar tab. The queue is
// the serialization point: all VTerm mutations funnel through the writer
// goroutine in enqueue order, off the Update goroutine.
type sidebarWriteReq struct {
	chunk []byte
	// drainTrailing flushes NoiseTrailing verbatim (PTY stopped — the stream
	// is over, so a held-back trailing is a real prompt tail, not noise).
	drainTrailing bool
	// Route captured at enqueue — a workspace rebind before the write applies
	// must not misroute the completion message.
	workspaceID string
	tabID       string
}

// sidebarWriteQueueCap bounds enqueue buffering per tab. A full queue means
// the writer is a full flush window behind; the caller falls back to the
// synchronous write rather than drop bytes (byte-stream integrity wins over
// latency in the deep-flood case — the same trade the buffer cap makes).
const sidebarWriteQueueCap = 8

// SidebarTabWritten reports a completed writer pass back to the Update
// goroutine so the clipboard drain stays message-ordered.
type SidebarTabWritten struct {
	WorkspaceID string
	TabID       string
	clip        []byte
}

// enqueueSidebarWrite hands a flush chunk to the tab's writer goroutine,
// starting it lazily. Returns false when the queue is full — callers then
// drain-apply synchronously. Caller does NOT hold ts.mu (this method manages
// it).
func (m *TerminalModel) enqueueSidebarWrite(ts *TerminalState, req sidebarWriteReq) bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return m.enqueueSidebarWriteLocked(ts, req)
}

// enqueueSidebarWriteLocked is enqueueSidebarWrite for callers already inside
// the tab's critical section (the flush handler's single hold).
func (m *TerminalModel) enqueueSidebarWriteLocked(ts *TerminalState, req sidebarWriteReq) bool {
	if ts.writeQ == nil {
		ts.writeQ = make(chan sidebarWriteReq, sidebarWriteQueueCap)
		q := ts.writeQ
		safego.Go("sidebar.vterm_writer", func() { m.runSidebarWriter(ts, q) })
	}
	// The send stays under ts.mu: teardown closes writeQ under the same lock,
	// so the select can never send on a closed channel.
	select {
	case ts.writeQ <- req:
		return true
	default:
		return false
	}
}

// stopSidebarWriter tears down the tab's writer. Call under ts.mu; safe to
// call when no writer exists.
func (ts *TerminalState) stopSidebarWriterLocked() {
	if ts.writeQ != nil {
		close(ts.writeQ)
		ts.writeQ = nil
	}
}

// runSidebarWriter owns ts.VTerm.Write for one tab: drains the queue in
// order, applying each chunk through the noise-filter chain under ts.mu so
// the filter state and parser state stay consistent, and posts the captured
// clipboard payload back to Update.
func (m *TerminalModel) runSidebarWriter(ts *TerminalState, q <-chan sidebarWriteReq) {
	for req := range q {
		var clip []byte
		wrote := false
		ts.mu.Lock()
		clip, wrote = applySidebarWriteLocked(ts, req)
		ts.mu.Unlock()
		if wrote && m.msgSink != nil {
			m.msgSink(SidebarTabWritten{WorkspaceID: req.workspaceID, TabID: req.tabID, clip: clip})
		}
		perf.Count("sidebar_actor_write", 1)
	}
}

// drainSidebarWriteQueueLocked is the queue-full fallback: it applies every
// buffered request in dequeue order (dequeue order is apply order whether the
// worker or the fallback performs the write — both serialize under ts.mu),
// then applies the chunk that failed to enqueue. Bytes are never dropped; the
// fallback costs on-loop parse work — the pre-actor behavior — only while the
// writer is a full queue behind. Caller holds ts.mu. Returns the freshest
// non-nil clipboard payload seen across the drained writes.
func drainSidebarWriteQueueLocked(ts *TerminalState, chunk []byte) (clip []byte) {
drain:
	for {
		select {
		case req, ok := <-ts.writeQ:
			if !ok { // defensive: close requires ts.mu, which we hold
				break drain
			}
			if c, _ := applySidebarWriteLocked(ts, req); c != nil {
				clip = c
			}
		default:
			break drain
		}
	}
	if c, _ := applySidebarWriteLocked(ts, sidebarWriteReq{chunk: chunk}); c != nil {
		clip = c
	}
	return clip
}

// applySidebarWriteLocked performs one writer pass under ts.mu — used by both
// the writer goroutine and the queue-full synchronous fallback, so the two
// paths cannot diverge. Returns the captured clipboard payload and whether a
// write happened (VTerm present).
func applySidebarWriteLocked(ts *TerminalState, req sidebarWriteReq) (clip []byte, wrote bool) {
	if ts.VTerm == nil {
		return nil, false
	}
	if req.drainTrailing {
		trailing := ptyio.DrainKnownPTYNoiseTrailing(&ts.NoiseTrailing)
		if len(trailing) > 0 {
			flushDone := perf.Time("pty_flush")
			ts.VTerm.Write(trailing)
			flushDone()
			perf.Count("pty_flush_bytes", int64(len(trailing)))
		}
	} else if len(req.chunk) > 0 {
		flushDone := perf.Time("pty_flush")
		_ = ts.State.WriteFilteredChunkLocked(ts.VTerm.Write, req.chunk)
		flushDone()
		perf.Count("pty_flush_bytes", int64(len(req.chunk)))
		// Stream paused at the flush boundary: a held `name(N)` tail is more
		// likely a real prompt than a split diagnostic — release it (same
		// condition the pre-actor inline path used).
		if len(ts.PendingOutput) == 0 {
			ts.State.FlushNoiseTrailingLocked(ts.VTerm.Write)
		}
	}
	return ts.VTerm.TakePendingClipboard(), true
}
