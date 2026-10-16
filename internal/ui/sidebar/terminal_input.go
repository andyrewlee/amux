package sidebar

// Sidebar terminal input admission + delivery.
//
// The binding protocol itself is shared machinery — ptyio/input_binding.go
// carries the contract comment. This file holds the sidebar's half of the
// parameterized contract:
//
//  1. Queue preserves order, writer drains head-first.
//  2. Queue bounded by BOTH request count and byte count
//     (sidebarInputQueueRequests, sidebarInputQueueMaxBytes). queuedBytes
//     is guarded by ts.mu and RELEASED AT DEQUEUE — the writer decrements
//     inside its locked decide hook, so the byte budget frees as requests
//     leave the channel rather than when the send returns.
//  3. A binding is the (TerminalState, terminal attachment) pair stamped
//     with ts.input.gen. detach/teardown/replacement retires the writer
//     under ts.mu, marks it stale, and BUMPS gen at retire time (the
//     center pane bumps at attach instead) — a request queued for a dead
//     terminal is discarded and a late failure cannot detach the
//     replacement.
//  4. A failed send latches ts.input.failed on the Update goroutine's next
//     look AND posts one SidebarInputFailed. A late failure stamped under
//     a retired gen is dropped rather than detaching the replacement.
//  5. A full queue or a latched failure rejects the new request with a
//     visible toast — never silent direct write, never a detach.
//  6. Retired writers are joined only after leaving ts.mu — joining under
//     it would deadlock against a blocked send that needs the terminal
//     close to return.
//
// VTerm query replies share the queue via admitQueryReplyLocked. The
// response-writer callback runs inside VTerm.Write under ts.mu, so it can
// only push non-blocking; a full queue drops the reply (a terminal may
// display a slightly stale state) rather than wedge every handler on the
// mutex. Replies are not charged to the byte budget (Counted=false) —
// they are small by construction — but they still carry the binding gen
// so a retired binding's reply is discarded at dequeue.

import (
	"fmt"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/safego"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

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

type sidebarInputAdmission = ptyio.InputAdmission

const (
	sidebarInputAdmitted       = ptyio.InputAdmitted
	sidebarInputNoTerminal     = ptyio.InputNoTerminal
	sidebarInputRejectedFull   = ptyio.InputRejectedFull
	sidebarInputRejectedFailed = ptyio.InputRejectedFailed
)

// sidebarInputReq keeps the old request spelling at call sites; the type is
// the shared one.
type sidebarInputReq = ptyio.InputRequest

// sidebarInputWriter is the shared writer — the sidebar keeps no fields of
// its own on it (wsID/tabID are captured by the fail hook at construction).
type sidebarInputWriter = ptyio.InputWriter

// sidebarInputState is the tab's input binding: the current writer, the
// generation that fences stale requests/failures, and the latched failure
// bit. Guarded by ts.mu.
type sidebarInputState struct {
	gen    uint64
	writer *sidebarInputWriter
	failed bool
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
		w = m.newSidebarInputWriter(ts, send, iq.gen, m.workspaceID(), string(tabID))
		iq.writer = w
		safego.Go("sidebar.input_writer", w.Run)
	}
	return ts.admitSidebarInputReqLocked(w, data, label)
}

// newSidebarInputWriter builds the binding's writer with the sidebar's
// dequeue-decide and failure hooks. The decide hook decrements
// queuedBytes for counted requests inside ts.mu (dequeue-time release)
// and applies the sidebar fence — gen mismatch or stale writer. The fail
// hook captures the binding's route so a retired writer can never report
// onto a replacement.
func (m *TerminalModel) newSidebarInputWriter(ts *TerminalState, send func(string) error, gen uint64, wsID, tabID string) *sidebarInputWriter {
	var w *sidebarInputWriter
	w = ptyio.NewInputWriter(sidebarInputQueueRequests, send, gen, ptyio.InputWriterHooks{
		Decide: func(req ptyio.InputRequest) bool {
			return withMuVal(&ts.mu, func() bool {
				if req.Counted {
					w.QueuedBytes -= len(req.Data)
				}
				return req.Gen == w.Gen && !w.Stale.Load()
			})
		},
		Fail: func(req ptyio.InputRequest, err error) {
			failSidebarInput(ts, m, w, req, err, wsID, tabID)
		},
	})
	return w
}

// admitSidebarInputReqLocked pushes a request onto a live writer's queue.
// Caller holds ts.mu. A request that exceeds the byte bound on its own or
// would overflow either bound is rejected — the queued work is left intact
// (drop-new policy, same as the center pane).
func (ts *TerminalState) admitSidebarInputReqLocked(w *sidebarInputWriter, data, label string) sidebarInputAdmission {
	if !ptyio.AdmissionFits(w, len(data), sidebarInputQueueRequests, sidebarInputQueueMaxBytes) {
		return sidebarInputRejectedFull
	}
	if !ptyio.EnqueueInput(w, ptyio.InputRequest{Data: data, Label: label, Gen: w.Gen, Counted: true}) {
		return sidebarInputRejectedFull
	}
	return sidebarInputAdmitted
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
	if !ptyio.EnqueueInput(w, ptyio.InputRequest{Data: string(data), Label: "query reply", Gen: w.Gen}) {
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
	ptyio.RetireInputWriter(w)
	return w
}

// joinSidebarInputWriter waits for a retired input writer to exit. It must
// run outside ts.mu, and after closing the terminal the writer was bound
// to: a send blocked inside ptyFile.Write returns only when the close
// breaks it.
func joinSidebarInputWriter(w *sidebarInputWriter) {
	ptyio.JoinInputWriter(w)
}

// failSidebarInput records a write failure on the binding that produced it
// and notifies Update. If the binding was already retired (writer swapped
// out), the failure is stale — logged and dropped rather than latching
// state the replacement should not inherit.
func failSidebarInput(ts *TerminalState, m *TerminalModel, w *sidebarInputWriter, req sidebarInputReq, err error, wsID, tabID string) {
	current := withMuVal(&ts.mu, func() bool {
		if ts.input.writer == w {
			ts.input.failed = true
			return true
		}
		return false
	})
	err = fmt.Errorf("%s: %w", req.Label, err)
	if !current {
		logging.Debug("Sidebar input write failed on a retired binding (ignored): %v", err)
		return
	}
	logging.Error("Sidebar input write failed: %v", err)
	if m.msgSink != nil {
		m.msgSink(SidebarInputFailed{WorkspaceID: wsID, TabID: tabID, Gen: w.Gen, Err: err})
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
	var (
		clip          []byte
		retiredInput  *sidebarInputWriter
		retiredWriter *sidebarWriterStream
		term          *pty.Terminal
		matched       bool
	)
	withMu(&ts.mu, func() {
		if ts.input.gen != gen {
			return
		}
		matched = true
		// Mirror detachState: the detached VTerm keeps its history, so queued
		// stream requests finish in order before the stream buffers reset.
		ts.invalidateReattachLocked()
		clip = drainSidebarWriterQueueLocked(ts)
		retiredInput = ts.retireSidebarInputWriterLocked()
		retiredWriter = ts.stopSidebarWriterLocked()
		term = ts.Terminal
		ts.Terminal = nil
		ts.Running = false
		ts.Detached = true
		ts.UserDetached = false
		ts.PendingOutput = nil
		ts.pendingBufferedBytes = 0
		ts.NoiseTrailing = nil
	})
	if !matched {
		return
	}
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
