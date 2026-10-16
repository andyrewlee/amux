package ptyio

import (
	"sync/atomic"
)

// The pane input-writer binding protocol, shared by the center agent tabs
// and the sidebar terminals. Both panes used to implement this contract
// separately; the subtle machinery lives here once, with each pane's
// semantics expressed as parameters and hooks.
//
// The contract:
//
//  1. Ordering. Accepted requests go through the writer's FIFO channel
//     only; no fallback path may write user bytes to the PTY directly —
//     a direct write can overtake queued input and reorder e.g. text
//     after Enter.
//  2. Bounded admission. The queue caps request count AND queued bytes;
//     a full queue or a failed binding rejects with a visible signal —
//     never a silent direct write, never a detach for a merely slow one.
//  3. Lifecycle binding. A writer is stamped with the pane's input
//     generation (Gen). Detach/stop/close/reattach transitions retire the
//     writer: pending requests are discarded, completions and failures
//     from the retired writer are fenced out, and the worker is joined
//     OFF the pane lock once the terminal is closed and any in-flight
//     write returns.
//  4. Failure once. A write failure on the current binding latches the
//     pane's failed flag and emits exactly one generation-stamped failure
//     message; a late failure stamped under a retired binding is dropped
//     rather than detaching the replacement.
//  5. Requests are never closed — the writer exits on its Stop channel.
//     A response-writer closure retired mid-window may still push onto
//     Reqs; a closed channel would panic it.
//
// Per-pane policy stays with the pane: when the gen bumps (sidebar bumps
// at retire, center at the next attach), whether queued-byte accounting
// releases at dequeue (sidebar) or at completion (center), and what a
// rejection or failure surfaces as (pane msg types).

// InputAdmission is the admission verdict for one input attempt.
type InputAdmission int

const (
	// InputAdmitted: request is queued for the binding's writer.
	InputAdmitted InputAdmission = iota
	// InputNoTerminal: no terminal on the binding — silent no-op.
	InputNoTerminal
	// InputRejectedFull: the binding's queue hit a bound — visible reject.
	InputRejectedFull
	// InputRejectedFailed: the binding latched a delivery failure that has
	// not been handled — visible reject; the writer is not recreated.
	InputRejectedFailed
	// InputRejectedStale: the request pinned an input generation that has
	// since been replaced. Silent drop — it belonged to a dead stream.
	InputRejectedStale
)

// InputRequest is one admitted unit of terminal input. Echo/Seq/WsID/TabID
// are carried for the pane's completion and reporting hooks — the shared
// machinery never interprets them.
type InputRequest struct {
	Data    string
	Label   string
	Gen     uint64 // stamped binding gen; 0 disables the per-request fence
	Counted bool   // counts against the byte bound (query replies bypass)
	Echo    bool
	Seq     uint64
	WsID    string
	TabID   string
}

// InputWriter is the dedicated delivery worker for one input binding.
// Reqs is never closed — pushes from a retired closure land on a dead
// channel and drain harmlessly at the cap; Stop retires it instead.
// QueuedBytes is guarded by the pane's lock (the pane performs the
// increment inside its admission hold and the decrement inside whichever
// hook its release policy uses).
type InputWriter struct {
	Reqs        chan InputRequest
	Stop        chan struct{} // closed once by RetireInputWriter
	done        chan struct{} // closed when Run returns
	Stale       atomic.Bool   // set by RetireInputWriter: discard queued + in-flight bookkeeping
	Inflight    atomic.Bool   // marks a send in progress — bounds/tests read it
	QueuedBytes int
	Gen         uint64

	send     func(string) error
	decide   func(req InputRequest) bool
	before   func(req InputRequest)
	complete func(req InputRequest)
	fail     func(req InputRequest, err error)
	stopOnce atomic.Bool
}

// InputWriterHooks parameterizes the drain loop for each pane. All three
// run wherever the pane's lock discipline puts them — the loop holds no
// pane lock itself, so a hook that needs tab.mu/ts.mu takes it inside.
type InputWriterHooks struct {
	// Decide runs at dequeue: true delivers, false skips. Nil gets the
	// shared fence (writer stale, or a nonzero request gen mismatching the
	// writer's). Panes with dequeue-time byte accounting override it to
	// decrement QueuedBytes inside their own lock.
	Decide func(req InputRequest) bool
	// Before runs once a request is committed to delivery, just before the
	// send (pane tracing). May be nil.
	Before func(req InputRequest)
	// Complete runs after a successful send (pane bookkeeping: byte
	// release, local-echo stamping, cursor refresh). May be nil.
	Complete func(req InputRequest)
	// Fail runs once on a send error; the pane latches its binding's
	// failure flag and emits its own gen-stamped report. Required.
	Fail func(req InputRequest, err error)
}

// NewInputWriter constructs a stopped writer bound to gen. The pane starts
// it with its own supervisor (safego.Go(w.Run)) at install time, inside
// the same lock hold that links it into the binding.
func NewInputWriter(capacity int, send func(string) error, gen uint64, hooks InputWriterHooks) *InputWriter {
	return &InputWriter{
		Reqs:     make(chan InputRequest, capacity),
		Stop:     make(chan struct{}),
		done:     make(chan struct{}),
		Gen:      gen,
		send:     send,
		decide:   hooks.Decide,
		before:   hooks.Before,
		complete: hooks.Complete,
		fail:     hooks.Fail,
	}
}

// Done reports when the writer's Run has exited.
func (w *InputWriter) Done() <-chan struct{} { return w.done }

// Run is the writer goroutine: one FIFO drain per binding, exiting on a
// send failure (the pane's Fail hook owns reporting and the latch) or on
// the retired Stop channel.
func (w *InputWriter) Run() {
	defer close(w.done)
	for {
		select {
		case req := <-w.Reqs:
			if w.decide != nil {
				if !w.decide(req) {
					continue
				}
			} else if w.Stale.Load() || (req.Gen != 0 && req.Gen != w.Gen) {
				continue
			}
			if !w.DeliverOne(req) {
				return
			}
		case <-w.Stop:
			return
		}
	}
}

// DeliverOne performs a single request's send plus its completion
// bookkeeping — separated from Run so pane tests can drive delivery
// deterministically. False means a send error ended the writer (the pane's
// Fail hook already ran).
func (w *InputWriter) DeliverOne(req InputRequest) bool {
	if w.before != nil {
		w.before(req)
	}
	w.Inflight.Store(true)
	err := w.send(req.Data)
	w.Inflight.Store(false)
	if err != nil {
		w.fail(req, err)
		return false
	}
	if w.complete != nil {
		w.complete(req)
	}
	return true
}

// RetireInputWriter detaches a writer from its binding: marks it stale so
// queued and in-flight work discards and signals exit via Stop. The pane
// performs this inside its binding lock alongside its own binding-pointer
// clear and gen bump; the off-lock join comes later via JoinInputWriter.
func RetireInputWriter(w *InputWriter) {
	if w == nil {
		return
	}
	w.Stale.Store(true)
	if w.stopOnce.CompareAndSwap(false, true) {
		close(w.Stop)
	}
}

// JoinInputWriter blocks until the writer exits. It must never run under
// the pane lock: a blocked write only unblocks when the lifecycle closes
// the terminal, which callers arrange before joining.
func JoinInputWriter(w *InputWriter) {
	if w == nil {
		return
	}
	<-w.done
}

// AdmissionFits reports whether dataLen bytes fit the writer's bounds:
// per-request oversize, pending count (queued + in-flight), and the byte
// budget. Caller holds the pane lock that guards QueuedBytes.
func AdmissionFits(w *InputWriter, dataLen, maxRequests, maxBytes int) bool {
	if dataLen > maxBytes {
		return false
	}
	pending := len(w.Reqs)
	if w.Inflight.Load() {
		pending++
	}
	return pending < maxRequests && w.QueuedBytes+dataLen <= maxBytes
}

// EnqueueInput pushes req non-blocking onto the writer's queue, charging
// counted requests to the byte budget. False on a full channel — the
// pane's visible-reject policy owns what happens next.
func EnqueueInput(w *InputWriter, req InputRequest) bool {
	select {
	case w.Reqs <- req:
		if req.Counted {
			w.QueuedBytes += len(req.Data)
		}
		return true
	default:
		return false
	}
}
