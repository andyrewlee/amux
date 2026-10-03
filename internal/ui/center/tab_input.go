package center

// Terminal input admission + delivery contract.
//
// User input reaches the hosted terminal through one bounded FIFO per terminal
// binding (a (tab, agent-terminal) pair). Producers on the Bubble Tea update
// loop admit synchronously — admission order is the order Update received the
// events — and a dedicated writer goroutine per binding performs the actual
// PTY writes, so Update never blocks on a slow or stuck write and input never
// queues behind the tab actor's output parsing.
//
// Invariants that keep this ordered and safe:
//
//  1. Ordering. Accepted requests go through the writer's FIFO channel only.
//     No fallback path may write user bytes to the PTY directly — a direct
//     write can overtake queued input and reorder e.g. text after Enter.
//  2. Bounded admission. The queue caps requests and queued bytes; a full
//     queue or a failed binding rejects with a visible signal — never a
//     silent direct write, never a detach.
//  3. Lifecycle binding. A writer is bound to the input generation stamped
//     when the binding was attached (tabInput.gen, bumped on every attach).
//     Detach/stop/close/reattach transitions retire the writer: pending
//     requests are discarded, completions and failures from the retired
//     writer are fenced out by the generation, and the worker is joined off
//     tab.mu once the terminal is closed and any in-flight write returns.
//  4. Failure once. A write failure on the current binding latches
//     tabInput.failed and emits exactly one generation-stamped
//     TabInputFailed; the guarded app handler owns the visible detach.
//     Until it runs, further admissions are rejected — the failed writer is
//     never lazily recreated for the same binding.
//
// The separate intentional control paths — per-agent interrupts, mouse
// reporting (tab_actor.go sendMouseToTerminal), and vterm query responses
// (SetResponseWriter) — write outside this queue by design; they carry their
// own ordering/failure contract and are not user-byte producers.

import (
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/safego"
)

var (
	// tabInputQueueRequests bounds pending admitted requests per binding. It
	// is a var (not a const) only as a test seam for small limits — production
	// never overrides it.
	tabInputQueueRequests = 4096
	// tabInputQueueMaxBytes bounds queued + in-flight payload bytes per
	// binding so a slow or stuck PTY cannot accumulate unbounded paste/key
	// input. Test seam, same as above.
	tabInputQueueMaxBytes = 4 << 20
)

// tabInputRequest is one admitted unit of terminal input.
type tabInputRequest struct {
	data  string
	label string
	seq   uint64
	wsID  string
	tabID TabID
	// echo requests the post-delivery local-echo stamping + chat cursor
	// refresh. Producers that historically did not stamp (prefix NUL) admit
	// with echo=false.
	echo bool
}

// tabInputAdmission is the admission verdict for one input attempt.
type tabInputAdmission int

const (
	// tabInputAdmitted: request is queued for the binding's writer.
	tabInputAdmitted tabInputAdmission = iota
	// tabInputNoTerminal: no agent/terminal on the tab — silent no-op.
	tabInputNoTerminal
	// tabInputRejectedFull: the binding's queue hit a bound — visible reject.
	tabInputRejectedFull
	// tabInputRejectedFailed: the binding latched a delivery failure that has
	// not been handled yet — visible reject; the writer is not recreated.
	tabInputRejectedFailed
	// tabInputRejectedStale: the request pinned an input generation that has
	// since been replaced (initial-task producer). Silent drop — the job
	// belonged to a dead stream.
	tabInputRejectedStale
)

// tabInputState is the per-binding input pipeline state on a Tab, guarded by
// tab.mu (the retired list is only drained by joinRetiredInputWriters, off-lock).
type tabInputState struct {
	// gen is the input-binding generation, bumped on every attach transition
	// (markAttachedLocked; a binding installed by construction occupies gen 1
	// so gen 0 always means "never bound"). TabInputFailed carries the gen
	// its writer was bound to so a late failure cannot detach a replacement.
	gen    uint64
	seq    uint64
	writer *tabInputWriter
	// failed latches a delivery failure on the current binding until the
	// emitted TabInputFailed has run the UI failure/detach path. It blocks
	// new writers for the same binding and rejects admissions, so a pending
	// failure cannot be made to look stale by a writer recreation.
	failed bool
	// retired holds writers retired by lifecycle transitions for off-lock join.
	retired []*tabInputWriter
}

// tabInputWriter is the dedicated delivery worker for one input binding.
// Fields after construction: send is bound to the admission-time terminal;
// reqs is closed exactly once via stop(); done closes when run() returns.
type tabInputWriter struct {
	m     *Model
	tab   *Tab
	gen   uint64
	send  func(string) error
	reqs  chan tabInputRequest
	done  chan struct{}
	stale atomic.Bool // set by stop(): discard queued + in-flight bookkeeping
	// inflight marks a send in progress — tests bound assertions on it.
	inflight    atomic.Bool
	queuedBytes int // guarded by tab.mu
	stopOnce    sync.Once
}

// tabInputSendFor resolves the delivery function for a tab's agent. It is a
// package-level seam so input tests can substitute a deterministic sink.
var tabInputSendFor = func(tab *Tab) func(string) error {
	if tab == nil || tab.Agent == nil || tab.Agent.Terminal == nil {
		return nil
	}
	return tab.Agent.Terminal.SendString
}

// admitTabInput synchronously admits one unit of user input to the tab's
// current binding FIFO, lazily creating the binding's writer. Called on the
// update loop; admission order is the call order. The returned generation is
// the binding the verdict applies to (0 when there is no binding), for the
// caller's rejection message. Rejections are returned for the caller to
// surface, never silently downgraded to a direct PTY write.
func (m *Model) admitTabInput(tab *Tab, data, label string, echo bool) (tabInputAdmission, uint64) {
	return m.admitTabInputBound(tab, m.workspaceID(), 0, data, label, echo)
}

// admitTabInputBound admits input stamped with an explicit route and pinned
// input generation. wantGen == 0 means "whatever binding is current"; a
// nonzero wantGen admits only while the binding still occupies that
// generation — the readiness-triggered initial task uses this so a reattach
// between capture and admission cannot deliver the old stream's task to the
// new agent.
func (m *Model) admitTabInputBound(tab *Tab, wsID string, wantGen uint64, data, label string, echo bool) (tabInputAdmission, uint64) {
	if tab == nil || data == "" {
		return tabInputNoTerminal, 0
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()
	st := &tab.tabInput
	if tab.isClosed() || tab.Agent == nil || tab.Agent.Terminal == nil {
		return tabInputNoTerminal, st.gen
	}
	if wantGen != 0 && st.gen != wantGen {
		return tabInputRejectedStale, st.gen
	}
	if st.failed {
		return tabInputRejectedFailed, st.gen
	}
	w := st.writer
	if w == nil {
		send := tabInputSendFor(tab)
		if send == nil {
			return tabInputNoTerminal, st.gen
		}
		w = &tabInputWriter{
			m:    m,
			tab:  tab,
			gen:  st.gen,
			send: send,
			reqs: make(chan tabInputRequest, tabInputQueueRequests),
			done: make(chan struct{}),
		}
		st.writer = w
		safego.Go("center.tab_input_writer", w.run)
	}
	// The request bound counts queued + in-flight work — the send in progress
	// holds no slot in reqs but is still admitted, undelivered input.
	pending := len(w.reqs)
	if w.inflight.Load() {
		pending++
	}
	if len(data) > tabInputQueueMaxBytes ||
		pending >= tabInputQueueRequests ||
		w.queuedBytes+len(data) > tabInputQueueMaxBytes {
		return tabInputRejectedFull, st.gen
	}
	st.seq++
	req := tabInputRequest{
		data:  data,
		label: label,
		seq:   st.seq,
		wsID:  wsID,
		tabID: tab.ID,
		echo:  echo,
	}
	select {
	case w.reqs <- req:
		w.queuedBytes += len(data)
		return tabInputAdmitted, st.gen
	default:
		return tabInputRejectedFull, st.gen
	}
}

// rejectedTabInputCmd renders an admission rejection for the user as a
// TabInputRejected routed like the failure it sits beside: a warning the app
// toasts without detaching or persisting. No-terminal and stale verdicts are
// not user-visible rejections — they return nil.
func (m *Model) rejectedTabInputCmd(tab *Tab, res tabInputAdmission, gen uint64) tea.Cmd {
	var reason string
	switch res {
	case tabInputRejectedFull:
		reason = "input queue full"
	case tabInputRejectedFailed:
		reason = "input binding failed"
	default:
		return nil
	}
	if tab == nil {
		return nil
	}
	wsID := m.workspaceID()
	tabID := tab.ID
	return func() tea.Msg {
		return TabInputRejected{
			WorkspaceID: wsID,
			TabID:       tabID,
			Gen:         gen,
			Reason:      reason,
		}
	}
}

// run is the writer goroutine: one FIFO drain per binding. Requests whose
// binding was retired mid-queue are skipped (stale); a delivery failure ends
// the worker — the failure path owns reporting and the binding's latch.
func (w *tabInputWriter) run() {
	defer close(w.done)
	for req := range w.reqs {
		if w.stale.Load() {
			continue
		}
		w.inflight.Store(true)
		ok := w.deliver(req)
		w.inflight.Store(false)
		if !ok {
			return
		}
	}
}

// deliver performs a single PTY write plus its completion bookkeeping.
// Separated from run() so tests can drive delivery deterministically.
func (w *tabInputWriter) deliver(req tabInputRequest) bool {
	w.m.tracePTYInput(w.tab, []byte(req.data))
	if err := w.send(req.data); err != nil {
		w.fail(req, err)
		return false
	}
	w.complete(req)
	return true
}

// complete finishes a successful delivery under one tab.mu acquisition: the
// binding check and the post-delivery mutations are atomic so a retired
// writer can never stamp local echo onto a replacement binding.
func (w *tabInputWriter) complete(req tabInputRequest) {
	tab := w.tab
	refresh := false
	tab.mu.Lock()
	if tab.tabInput.writer == w {
		w.queuedBytes -= len(req.data)
		if req.echo {
			recordLocalInputEchoWindowLocked(tab, req.data, time.Now())
			refresh = w.m.isChatTabLocked(tab)
		}
	}
	tab.mu.Unlock()
	if refresh && w.m.msgSink != nil {
		w.m.msgSink(PTYCursorRefresh{WorkspaceID: req.wsID, TabID: req.tabID, InputGeneration: w.gen})
	}
}

// fail handles a delivery error. A failure on a binding that is still current
// latches the binding's failure flag and emits exactly one generation-stamped
// TabInputFailed — the guarded UI failure path owns the visible detach, so a
// tab stays healthy-looking (and its failure stays actionable) until the
// handler runs. A failure on a retired binding is a late signal: it is logged
// and dropped, and must not detach, toast, or stamp the replacement binding.
func (w *tabInputWriter) fail(req tabInputRequest, err error) {
	tab := w.tab
	tab.mu.Lock()
	current := tab.tabInput.writer == w
	if current {
		tab.tabInput.failed = true
	}
	tab.mu.Unlock()
	if !current {
		logging.Debug("dropping stale %s failure for tab %s (input gen %d): %v", req.label, tab.ID, w.gen, err)
		return
	}
	logging.Error("%s failed for tab %s: %v", req.label, tab.ID, err)
	if w.m.msgSink != nil {
		w.m.msgSink(TabInputFailed{TabID: req.tabID, WorkspaceID: req.wsID, Err: err, Gen: w.gen})
	}
}

// stop closes the request queue once. After close the worker drains and
// discards any queued-but-undelivered requests (each checks stale) then exits.
func (w *tabInputWriter) stop() {
	w.stopOnce.Do(func() { close(w.reqs) })
}

// join blocks until the worker has exited. It must never be called under
// tab.mu: a blocked write only unblocks when the lifecycle closes the
// terminal, which callers arrange before joining.
func (w *tabInputWriter) join() {
	if w == nil {
		return
	}
	<-w.done
}

// retireTabInputWriterLocked detaches the current writer from the binding:
// marks it stale so queued and in-flight work discards, closes its queue, and
// moves it to the retired list for an off-lock join. Lifecycle transitions
// call this inside tab.mu; the caller joins via joinRetiredInputWriters after
// releasing the lock (and after closing the agent/terminal so an in-flight
// write can return). The generation does not change here — gen is bumped by
// the next attach, which is what makes a pre-attach failure stale.
func (t *Tab) retireTabInputWriterLocked() {
	if t == nil {
		return
	}
	w := t.tabInput.writer
	if w == nil {
		return
	}
	t.tabInput.writer = nil
	w.stale.Store(true)
	w.stop()
	t.tabInput.retired = append(t.tabInput.retired, w)
}

// joinRetiredInputWriters drains the retired-writer list and joins each
// worker. Safe to call repeatedly; callers run it after the binding's
// terminal has been closed (or detached) so a blocked write has returned.
func (t *Tab) joinRetiredInputWriters() {
	if t == nil {
		return
	}
	t.mu.Lock()
	retired := t.tabInput.retired
	t.tabInput.retired = nil
	t.mu.Unlock()
	for _, w := range retired {
		w.join()
	}
}

// inputGenLocked reports the current input-binding generation. The reattach
// and create sites stamp it onto the response-writer closure so a query-write
// failure fences itself like a queued-input failure.
func (t *Tab) inputGenLocked() uint64 {
	return t.tabInput.gen
}
