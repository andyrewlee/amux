package sidebar

import (
	"sync/atomic"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/safego"
)

// Sidebar input queue contract
//
// User input is accepted by Update while the dedicated writer goroutine
// performs the PTY write. No Update path may block on a slow or stuck
// ptyFile.Write — a stalled terminal must never wedge Bubble Tea's event
// loop. This mirrors internal/ui/center/tab_input.go's contract, scaled to
// one writer binding per sidebar terminal.
//
// Invariants:
//
//  1. Update performs admission only. Accepted requests go to the binding's
//     single reqs channel — a bounded FIFO — in the order callers hand them
//     over, and the writer drains them one at a time off the queue head,
//     preserving order.
//  2. The queue is bounded by BOTH request count and byte count —
//     sidebarInputQueueRequests and sidebarInputQueueMaxBytes. A byte bound
//     alone under-serves tiny requests and a request bound alone lets large
//     pastes grow the queue unboundedly. queuedBytes is guarded by ts.mu,
//     released by the writer as each request finishes.
//  3. A binding is the (TerminalState, terminal attachment) pair stamped
//     with ts.input.gen. detach, teardown, and terminal replacement retire
//     the binding's writer under ts.mu, mark it stale, and bump gen, so a
//     request queued for a dead terminal is discarded and a late failure
//     message cannot detach the replacement.
//  4. A failed send latches ts.input.failed on the Update goroutine's next
//     look AND posts one SidebarInputFailed. A late failure stamped under a
//     retired gen is dropped rather than detaching the replacement.
//  5. A full queue or a latched failure rejects the new request with a
//     visible signal — never a silent direct write, never a detach for a
//     queue that is merely slow.
//  6. Retired writers are joined ONLY after leaving ts.mu — joining under
//     it would deadlock against a blocked send that needs the terminal
//     close to return.
//
// VTerm query replies share the queue. The response-writer callback runs
// inside VTerm.Write under ts.mu, so it can only push non-blocking; a full
// queue drops the reply (a terminal may display a slightly stale state)
// rather than wedge every handler on the mutex. Ordering vs user input is
// correct by construction: the reply enqueues when the query parses, before
// input admitted afterward.

var (
	// sidebarInputQueueRequests / sidebarInputQueueMaxBytes bound input
	// buffering per sidebar terminal. Vars (not consts) so tests can tighten
	// the bounds; production never rewrites them after init.
	sidebarInputQueueRequests = 4096
	sidebarInputQueueMaxBytes = 4 << 20 // 4 MiB

	// sidebarInputSendFor resolves the writer's send function at install
	// time — a test seam mirroring tabInputSendFor, bound once so a replaced
	// terminal can never receive a stale binding's writes.
	sidebarInputSendFor = func(ts *TerminalState) func(string) error {
		if ts == nil || ts.Terminal == nil {
			return nil
		}
		return ts.Terminal.SendString
	}
)

type sidebarInputAdmission int

const (
	sidebarInputAdmitted sidebarInputAdmission = iota
	sidebarInputNoTerminal
	sidebarInputRejectedFull
	sidebarInputRejectedFailed
)

// sidebarInputState is the tab's input binding: the current writer, the
// generation that fences stale requests/failures, and the latched failure
// bit. Guarded by ts.mu.
type sidebarInputState struct {
	gen    uint64
	writer *sidebarInputWriter
	failed bool
}

// sidebarInputReq is one admitted input write for a sidebar terminal.
type sidebarInputReq struct {
	data  string
	label string
	// gen is the binding generation the request was admitted under — the
	// writer discards a request stamped under a retired gen.
	gen uint64
	// counted marks requests whose bytes were charged to queuedBytes —
	// query replies bypass the byte budget and must not decrement it.
	counted bool
}

// sidebarInputWriter is one binding's writer. reqs is never closed — pushes
// from a retired response-writer closure land on a dead channel and drain
// harmlessly at the cap — stop + stale retire it instead.
type sidebarInputWriter struct {
	reqs     chan sidebarInputReq
	stop     chan struct{} // closed once by retireSidebarInputWriterLocked
	done     chan struct{} // closed when runSidebarInputWriter returns
	stale    atomic.Bool
	inflight atomic.Bool
	send     func(string) error
	gen      uint64
	wsID     string
	tabID    string
	// queuedBytes tracks admitted-but-undelivered payload; guarded by ts.mu.
	queuedBytes int
}

// SidebarInputFailed reports a sidebar terminal input write failure. Gen is
// the input binding generation the failing writer was bound to — a failure
// arriving after the binding was replaced carries a stale gen and must be
// dropped rather than detach the replacement.
type SidebarInputFailed struct {
	WorkspaceID string
	TabID       string
	Err         error
	Gen         uint64
}

// MarkCriticalExternalMsg marks SidebarInputFailed as critical: dropping it
// would skip the detach cleanup the failure path performs.
func (SidebarInputFailed) MarkCriticalExternalMsg() {}

// admitSidebarInput enqueues one input write. Runs on the Update goroutine
// and never blocks: every branch returns promptly, and the channel push is
// a bounded non-blocking select. Caller does NOT hold ts.mu.
func (m *TerminalModel) admitSidebarInput(ts *TerminalState, tabID TerminalTabID, data, label string) sidebarInputAdmission {
	if ts == nil || data == "" {
		return sidebarInputNoTerminal
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.Terminal == nil {
		return sidebarInputNoTerminal
	}
	iq := &ts.input
	if iq.failed {
		return sidebarInputRejectedFailed
	}
	w := iq.writer
	if w == nil {
		send := sidebarInputSendFor(ts)
		if send == nil {
			return sidebarInputNoTerminal
		}
		w = &sidebarInputWriter{
			reqs:  make(chan sidebarInputReq, sidebarInputQueueRequests),
			stop:  make(chan struct{}),
			done:  make(chan struct{}),
			send:  send,
			gen:   iq.gen,
			wsID:  m.workspaceID(),
			tabID: string(tabID),
		}
		iq.writer = w
		safego.Go("sidebar.input_writer", func() { m.runSidebarInputWriter(ts, w) })
	}
	return ts.admitSidebarInputReqLocked(w, data, label)
}

// admitSidebarInputReqLocked pushes a request onto a live writer's queue.
// Caller holds ts.mu. A request that exceeds the byte bound on its own or
// would overflow either bound is rejected — the queued work is left intact
// (drop-new policy, same as the center pane).
func (ts *TerminalState) admitSidebarInputReqLocked(w *sidebarInputWriter, data, label string) sidebarInputAdmission {
	if len(data) > sidebarInputQueueMaxBytes {
		return sidebarInputRejectedFull
	}
	pending := len(w.reqs)
	if w.inflight.Load() {
		pending++
	}
	if pending >= sidebarInputQueueRequests || w.queuedBytes+len(data) > sidebarInputQueueMaxBytes {
		return sidebarInputRejectedFull
	}
	select {
	case w.reqs <- sidebarInputReq{data: data, label: label, gen: w.gen, counted: true}:
		w.queuedBytes += len(data)
		return sidebarInputAdmitted
	default:
		return sidebarInputRejectedFull
	}
}

// admitQueryReplyLocked pushes a VTerm query reply into the input FIFO.
// Runs inside a VTerm parse where ts.mu is already held — it must never
// block, so a missing/full queue drops the reply (a recoverable staleness)
// rather than wedging every handler on the mutex. Replies are not counted
// against the byte bound: they are small by construction (DSR/DA/osc
// replies are tens of bytes) and the channel's request cap still applies.
func (ts *TerminalState) admitQueryReplyLocked(data []byte) {
	if len(data) == 0 {
		return
	}
	w := ts.input.writer
	if w == nil {
		return
	}
	select {
	case w.reqs <- sidebarInputReq{data: string(data), label: "query reply", gen: w.gen}:
	default:
		logging.Debug("Sidebar query reply dropped: input queue full")
	}
}

// retireSidebarInputWriterLocked retires the tab's current input writer:
// unlinked from the tab, marked stale, signaled to exit, and the binding
// gen bumped so queued requests and late failures identify themselves as
// obsolete. Call under ts.mu; returns the writer so the caller can join it
// AFTER releasing the mutex — and after closing the terminal it was bound
// to, if a send may still be blocked inside it. Safe when no writer exists.
func (ts *TerminalState) retireSidebarInputWriterLocked() *sidebarInputWriter {
	w := ts.input.writer
	if w == nil {
		return nil
	}
	ts.input.writer = nil
	ts.input.gen++
	// The failure latch belongs to the retired binding — a reattach that
	// installs a fresh writer must admit input again.
	ts.input.failed = false
	w.stale.Store(true)
	close(w.stop)
	return w
}

// joinSidebarInputWriter waits for a retired input writer to exit. It must
// run outside ts.mu, and after closing the terminal the writer was bound
// to: a send blocked inside ptyFile.Write returns only when the close
// breaks it.
func joinSidebarInputWriter(w *sidebarInputWriter) {
	if w == nil {
		return
	}
	<-w.done
}

// runSidebarInputWriter drains one binding's queue, writing each request to
// the PTY in order. A failed send latches ts.input.failed (when the binding
// is still current), posts one gen-stamped SidebarInputFailed, and retires
// the writer — Update learns through the message whether the failure still
// belongs to the live binding.
func (m *TerminalModel) runSidebarInputWriter(ts *TerminalState, w *sidebarInputWriter) {
	defer close(w.done)
	for {
		select {
		case req := <-w.reqs:
			ts.mu.Lock()
			if req.counted {
				w.queuedBytes -= len(req.data)
			}
			stale := req.gen != w.gen || w.stale.Load()
			ts.mu.Unlock()
			if stale {
				continue
			}
			w.inflight.Store(true)
			err := w.send(req.data)
			w.inflight.Store(false)
			if err != nil {
				w.failSidebarInput(ts, m, req, err)
				return
			}
		case <-w.stop:
			return
		}
	}
}

// failSidebarInput records a write failure on the binding that produced it
// and notifies Update. If the binding was already retired (writer swapped
// out), the failure is stale — logged and dropped rather than latching
// state the replacement should not inherit.
func (w *sidebarInputWriter) failSidebarInput(ts *TerminalState, m *TerminalModel, req sidebarInputReq, err error) {
	ts.mu.Lock()
	current := ts.input.writer == w
	if current {
		ts.input.failed = true
	}
	ts.mu.Unlock()
	if !current {
		logging.Debug("Sidebar input write failed on a retired binding (ignored): %v", err)
		return
	}
	logging.Error("Sidebar input write failed: %v", err)
	if m.msgSink != nil {
		m.msgSink(SidebarInputFailed{WorkspaceID: w.wsID, TabID: w.tabID, Gen: w.gen, Err: err})
	}
}

// surfaceSidebarInputRejection emits the visible warning for a refused user
// input — the rejection is decided synchronously on Update, so a toast is
// posted straight back through the sink; the binding stays healthy.
func (m *TerminalModel) surfaceSidebarInputRejection(ts *TerminalState, tabID TerminalTabID, reason string) {
	if m.msgSink == nil {
		return
	}
	m.msgSink(messages.Toast{Level: messages.ToastWarning, Message: "Sidebar terminal input dropped: " + reason})
}

// handleSidebarInputFailed is the Update-goroutine endpoint for a writer's
// failure report: detach the terminal only if the failure's gen still
// matches the live binding — a stale failure must not detach the
// replacement terminal.
func (m *TerminalModel) handleSidebarInputFailed(msg SidebarInputFailed) {
	tab, _ := m.resolveTabForResult(msg.WorkspaceID, TerminalTabID(msg.TabID), "sidebar input failure")
	if tab == nil || tab.State == nil {
		return
	}
	m.detachStateForInputGen(tab.State, msg.Gen)
}

// detachStateForInputGen performs the same retire-and-detach as
// detachState, but only while ts.input.gen still equals gen — fencing the
// async failure report to the binding that produced it.
func (m *TerminalModel) detachStateForInputGen(ts *TerminalState, gen uint64) {
	if ts == nil {
		return
	}
	// The gen check and the detach commit must be one ts.mu hold: a stale
	// failure touches nothing, and once the check passes the detachment is
	// inevitable — invalidateReattachLocked fences any in-flight attach so
	// nothing after the unlock can resurrect this binding's reader.
	ts.mu.Lock()
	if ts.input.gen != gen {
		ts.mu.Unlock()
		return
	}
	// Mirror detachState: the detached VTerm keeps its history, so queued
	// stream requests finish in order before the stream buffers reset.
	ts.invalidateReattachLocked()
	clip := drainSidebarWriterQueueLocked(ts)
	retiredInput := ts.retireSidebarInputWriterLocked()
	retiredWriter := ts.stopSidebarWriterLocked()
	term := ts.Terminal
	ts.Terminal = nil
	ts.Running = false
	ts.Detached = true
	ts.UserDetached = false
	ts.PendingOutput = nil
	ts.pendingBufferedBytes = 0
	ts.NoiseTrailing = nil
	ts.mu.Unlock()
	// The reader belongs to the dead binding — stopping it after the commit
	// can only quiesce the terminal that is already detached.
	m.stopPTYReader(ts)
	joinSidebarWriter(retiredWriter)
	if term != nil {
		closeTerminalForSidebar(term, "input write failure")
	}
	// A send blocked inside the closed terminal unblocks only now — join
	// the retired writer after the close, never under ts.mu.
	joinSidebarInputWriter(retiredInput)
	if len(clip) > 0 {
		m.drainSidebarClipboard(clip)
	}
}
