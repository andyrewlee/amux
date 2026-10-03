package sidebar

import (
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/safego"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

// Sidebar write stream contract.
//
// PTY output reaches the sidebar VTerm through one writer goroutine per
// stream. A stream is the (TerminalState, writer instance) pair installed
// under ts.mu; detach, teardown, and accepted terminal replacement stop the
// current stream and bump ts.writeEpoch, so every unit of work — queued
// request, worker, posted SidebarTabWritten — is fenced to the stream that
// produced it.
//
// Invariants:
//
//  1. Dequeue and apply are one mutex-owned operation. A request pops from
//     the stream queue and applies to the parser inside the same ts.mu hold,
//     so no path can hold a removed request while waiting for the mutex —
//     that is the race that let a full-queue fallback parse newer chunks
//     ahead of an older one the worker had already dequeued.
//  2. The worker waits on wake/stop outside the mutex; it never blocks
//     waiting for a request while holding it, and never holds a dequeued
//     request while waiting for it.
//  3. The worker never reads PendingOutput: AppendOutput mutates that buffer
//     with the mutex released (ptyio ownership contract). The UI publishes
//     ts.pendingBufferedBytes under ts.mu; the worker consults it and its
//     own queue depth for the idle-boundary noise release.
//  4. A stream's epoch is stamped on each request and on the posted
//     completion. A stopped stream's queued requests die with it; a late
//     completion stamped with a replaced epoch has no clipboard side
//     effects.
//  5. The queue cap of eight and the synchronous deep-flood fallback are
//     intentional: byte-stream integrity takes precedence over Update
//     latency at that limit. Both paths funnel through
//     applySidebarWriteLocked under the same mutex so their orders cannot
//     diverge.

// sidebarWriteReq is one unit of writer work for a sidebar tab. The stream
// queue is the serialization point: all VTerm mutations funnel through the
// writer goroutine (or the bounded synchronous fallback) in enqueue order.
type sidebarWriteReq struct {
	chunk []byte
	// drainTrailing flushes NoiseTrailing verbatim (PTY stopped — the stream
	// is over, so a held-back trailing is a real prompt tail, not noise).
	drainTrailing bool
	// Route captured at enqueue — a workspace rebind before the write applies
	// must not misroute the completion message.
	workspaceID string
	tabID       string
	// epoch is the writer stream's epoch at enqueue, stamped so a request
	// that outlives its stream identifies itself as obsolete.
	epoch uint64
}

// sidebarWriterStream is one writer stream's private state. reqs appends and
// pops under ts.mu; the worker waits on wake/stop outside it and closes done
// when it exits.
type sidebarWriterStream struct {
	reqs []sidebarWriteReq
	wake chan struct{} // capacity 1; enqueue signals nonblocking
	stop chan struct{} // closed once by stopSidebarWriterLocked
	done chan struct{} // closed when runSidebarWriter returns
	// stopped is set under ts.mu before stop closes; a queued request is
	// obsolete the moment its stream stops — it is never applied.
	stopped bool
	// epoch is ts.writeEpoch at install, stamped onto posted completions.
	epoch uint64
	// testPreLock, when set, runs immediately before each ts.mu acquisition
	// on the worker — the integrity tests' pause point, outside the mutex.
	// Only tests on a stream they installed set it; production leaves nil.
	testPreLock func()
}

// sidebarWriteQueueCap bounds enqueue buffering per tab. A full queue means
// the writer is a full flush window behind; the caller falls back to the
// synchronous write rather than drop bytes (byte-stream integrity wins over
// latency in the deep-flood case — the same trade the buffer cap makes).
const sidebarWriteQueueCap = 8

// SidebarTabWritten reports a completed writer pass back to the Update
// goroutine so the clipboard drain stays message-ordered. Epoch is the
// stream the completion belongs to; Update drops a completion whose epoch
// no longer matches the tab's current stream.
type SidebarTabWritten struct {
	WorkspaceID string
	TabID       string
	clip        []byte
	epoch       uint64
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
// the tab's critical section (the flush handler's single hold). Lazily
// installs the stream: the install bumps ts.writeEpoch so completions from
// any previous stream can never alias the new one.
func (m *TerminalModel) enqueueSidebarWriteLocked(ts *TerminalState, req sidebarWriteReq) bool {
	w := ts.writer
	if w == nil {
		ts.writeEpoch++
		w = &sidebarWriterStream{
			wake:  make(chan struct{}, 1),
			stop:  make(chan struct{}),
			done:  make(chan struct{}),
			epoch: ts.writeEpoch,
		}
		ts.writer = w
		safego.Go("sidebar.vterm_writer", func() { m.runSidebarWriter(ts, w) })
	}
	if w.stopped || len(w.reqs) >= sidebarWriteQueueCap {
		return false
	}
	req.epoch = w.epoch
	w.reqs = append(w.reqs, req)
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return true
}

// stopSidebarWriterLocked retires the tab's current writer stream: the
// stream is unlinked from the tab, its epoch is fenced by bumping
// ts.writeEpoch, and the worker is signaled to exit — discarding queued
// requests, which are obsolete the moment the stream ends. Call under ts.mu;
// returns the stream so the caller can join it AFTER releasing the mutex
// (joinSidebarWriter). Safe when no writer exists.
func (ts *TerminalState) stopSidebarWriterLocked() *sidebarWriterStream {
	w := ts.writer
	if w == nil {
		return nil
	}
	ts.writer = nil
	ts.writeEpoch++
	w.stopped = true
	close(w.stop)
	return w
}

// joinSidebarWriter waits for a retired stream's worker to exit. It must run
// outside ts.mu: the worker acquires that mutex for every apply, so waiting
// under it would deadlock against a queue the worker still wants to check.
func joinSidebarWriter(w *sidebarWriterStream) {
	if w == nil {
		return
	}
	<-w.done
}

// runSidebarWriter owns ts.VTerm.Write for one stream: pops each request and
// applies it inside a single ts.mu hold so dequeue order can never diverge
// from apply order, and posts the captured clipboard payload back to Update
// stamped with the stream's epoch.
func (m *TerminalModel) runSidebarWriter(ts *TerminalState, w *sidebarWriterStream) {
	defer close(w.done)
	for {
		if w.testPreLock != nil {
			w.testPreLock()
		}
		ts.mu.Lock()
		if w.stopped {
			// The stream ended: queued requests die with it rather than
			// applying to whatever state follows.
			ts.mu.Unlock()
			return
		}
		if len(w.reqs) == 0 {
			ts.mu.Unlock()
			select {
			case <-w.wake:
			case <-w.stop:
				return
			}
			continue
		}
		req := w.reqs[0]
		w.reqs[0] = sidebarWriteReq{} // release the chunk backing array
		w.reqs = w.reqs[1:]
		if req.epoch != w.epoch {
			// A request stamped under a different stream epoch is obsolete:
			// discard it before it reaches the parser.
			ts.mu.Unlock()
			continue
		}
		more := len(w.reqs) > 0 || ts.pendingBufferedBytes > 0
		clip, wrote := applySidebarWriteLocked(ts, req, more)
		ts.mu.Unlock()
		if wrote && m.msgSink != nil {
			m.msgSink(SidebarTabWritten{WorkspaceID: req.workspaceID, TabID: req.tabID, clip: clip, epoch: w.epoch})
		}
		perf.Count("sidebar_actor_write", 1)
	}
}

// drainSidebarWriterQueueLocked applies every request still queued on the
// stream in order. Lifecycle paths use it to finish retained old-history
// work before resetting stream state (a bounded drain — the queue caps at
// sidebarWriteQueueCap). Caller holds ts.mu; returns the freshest non-nil
// clipboard payload across the drained writes.
func drainSidebarWriterQueueLocked(ts *TerminalState) (clip []byte) {
	w := ts.writer
	if w == nil {
		return nil
	}
	for len(w.reqs) > 0 {
		req := w.reqs[0]
		w.reqs[0] = sidebarWriteReq{}
		w.reqs = w.reqs[1:]
		more := len(w.reqs) > 0 || ts.pendingBufferedBytes > 0
		if c, _ := applySidebarWriteLocked(ts, req, more); c != nil {
			clip = c
		}
	}
	return clip
}

// drainSidebarWriteQueueLocked is the queue-full fallback: it applies every
// buffered request in dequeue order, then the request that failed to
// enqueue. Dequeue and apply both happen under the caller's ts.mu hold —
// identical to the worker's serialization — so the fallback can never parse
// a newer chunk ahead of an older queued one. Bytes are never dropped; the
// fallback costs on-loop parse work — the pre-actor behavior — only while
// the writer is a full queue behind. Caller holds ts.mu. Returns the
// freshest non-nil clipboard payload seen across the drained writes.
func drainSidebarWriteQueueLocked(ts *TerminalState, failed sidebarWriteReq) (clip []byte) {
	w := ts.writer
	if w != nil {
		for len(w.reqs) > 0 {
			req := w.reqs[0]
			w.reqs[0] = sidebarWriteReq{}
			w.reqs = w.reqs[1:]
			// The failed request still waits to apply — held noise must not
			// flush at an idle boundary that is not actually idle yet.
			more := len(w.reqs) > 0 || len(failed.chunk) > 0 || failed.drainTrailing || ts.pendingBufferedBytes > 0
			if c, _ := applySidebarWriteLocked(ts, req, more); c != nil {
				clip = c
			}
		}
	}
	more := ts.pendingBufferedBytes > 0
	if c, _ := applySidebarWriteLocked(ts, failed, more); c != nil {
		clip = c
	}
	return clip
}

// applySidebarWriteLocked performs one writer pass under ts.mu — used by the
// writer goroutine and every in-lock drain path, so no two apply paths can
// diverge. more reports whether known work still follows this request (a
// later queued request or published buffered bytes): only a truly idle
// boundary may release the held noise trailing. Returns the captured
// clipboard payload and whether a write happened (VTerm present).
func applySidebarWriteLocked(ts *TerminalState, req sidebarWriteReq, more bool) (clip []byte, wrote bool) {
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
		// likely a real prompt than a split diagnostic — release it, but only
		// when nothing else is known to follow (a later queued request or
		// published buffered bytes mean the pause has not happened yet).
		if !more {
			ts.State.FlushNoiseTrailingLocked(ts.VTerm.Write)
		}
	}
	return ts.VTerm.TakePendingClipboard(), true
}
