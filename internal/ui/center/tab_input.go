package center

// Terminal input admission + delivery contract.
//
// The binding protocol itself is shared machinery — ptyio/input_binding.go
// carries the contract comment. This file holds the center pane's half of
// the parameterized contract:
//
//  1. Ordering. Accepted requests go through the writer's FIFO channel only.
//     No fallback path may write user bytes to the PTY directly — a direct
//     write can overtake queued input and reorder e.g. text after Enter.
//  2. Bounded admission. The queue caps requests and queued bytes; a full
//     queue or a failed binding rejects with a visible signal — never a
//     silent direct write, never a detach.
//  3. Lifecycle binding. A writer is bound to the input generation stamped
//     when the binding was attached (tabInput.gen, bumped on every attach —
//     the sidebar bumps at retire instead). Detach/stop/close/reattach
//     transitions retire the writer: pending requests are discarded,
//     completions and failures from the retired writer are fenced out by
//     the generation, and the worker is joined off tab.mu once the terminal
//     is closed and any in-flight write returns.
//  4. Failure once. A write failure on the current binding latches
//     tabInput.failed and emits exactly one generation-stamped
//     TabInputFailed; the guarded app handler owns the visible detach.
//     Until it runs, further admissions are rejected — the failed writer is
//     never lazily recreated for the same binding.
//
// Center-only semantics stay on this side: requests carry echo/seq/wsID/
// tabID for the completion hook's local-echo stamping and PTYCursorRefresh,
// and admissions may pin a generation (wantGen) so input composed against
// one attachment is dropped after a reattach. The separate intentional
// control paths — per-agent interrupts, mouse reporting (tab_actor.go
// sendMouseToTerminal), and vterm query responses (SetResponseWriter) —
// write outside this queue by design; they carry their own
// ordering/failure contract and are not user-byte producers.

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/safego"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
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

// tabInputRequest keeps the old request spelling at call sites; the type is
// the shared one. The Echo flag requests the post-delivery local-echo
// stamping + chat cursor refresh; producers that historically did not stamp
// (prefix NUL) admit with Echo=false.
type tabInputRequest = ptyio.InputRequest

// tabInputAdmission is the admission verdict for one input attempt.
type tabInputAdmission = ptyio.InputAdmission

const (
	// tabInputAdmitted: request is queued for the binding's writer.
	tabInputAdmitted = ptyio.InputAdmitted
	// tabInputNoTerminal: no agent/terminal on the tab — silent no-op.
	tabInputNoTerminal = ptyio.InputNoTerminal
	// tabInputRejectedFull: the binding's queue hit a bound — visible reject.
	tabInputRejectedFull = ptyio.InputRejectedFull
	// tabInputRejectedFailed: the binding latched a delivery failure that has
	// not been handled yet — visible reject; the writer is not recreated.
	tabInputRejectedFailed = ptyio.InputRejectedFailed
	// tabInputRejectedStale: the request pinned an input generation that has
	// since been replaced (initial-task producer). Silent drop — the job
	// belonged to a dead stream.
	tabInputRejectedStale = ptyio.InputRejectedStale
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

// tabInputWriter is the shared writer — the center keeps no fields of its
// own on it (m/tab are captured by the hook closures at construction; send
// is bound once so a replaced terminal can never receive a stale binding's
// writes).
type tabInputWriter = ptyio.InputWriter

// tabInputSendFor resolves the delivery function for a tab's agent. It is a
// package-level seam so input tests can substitute a deterministic sink.
var tabInputSendFor = func(tab *Tab) func(string) error {
	if tab == nil || tab.Agent == nil || tab.Agent.Terminal == nil {
		return nil
	}
	return tab.Agent.Terminal.SendString
}

// newTabInputWriter builds the binding's writer with the center's hooks: no
// Decide override (the shared stale/gen fence is exactly the center's
// semantics — requests carry no gen so only writer staleness fences them),
// Before for the per-input trace, Complete for completion-time byte release
// + echo stamping + cursor refresh, Fail for the failure latch.
func (m *Model) newTabInputWriter(tab *Tab, send func(string) error, gen uint64) *tabInputWriter {
	var w *tabInputWriter
	w = ptyio.NewInputWriter(tabInputQueueRequests, send, gen, ptyio.InputWriterHooks{
		Before: func(req ptyio.InputRequest) {
			m.tracePTYInput(tab, []byte(req.Data))
		},
		Complete: func(req ptyio.InputRequest) {
			m.completeTabInput(tab, w, req)
		},
		Fail: func(req ptyio.InputRequest, err error) {
			m.failTabInput(tab, w, req, err)
		},
	})
	return w
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
		w = m.newTabInputWriter(tab, send, st.gen)
		st.writer = w
		safego.Go("center.tab_input_writer", w.Run)
	}
	// The request bound counts queued + in-flight work — the send in progress
	// holds no slot in reqs but is still admitted, undelivered input.
	if !ptyio.AdmissionFits(w, len(data), tabInputQueueRequests, tabInputQueueMaxBytes) {
		return tabInputRejectedFull, st.gen
	}
	st.seq++
	req := ptyio.InputRequest{
		Data:    data,
		Label:   label,
		Seq:     st.seq,
		WsID:    wsID,
		TabID:   string(tab.ID),
		Echo:    echo,
		Counted: true,
	}
	if !ptyio.EnqueueInput(w, req) {
		return tabInputRejectedFull, st.gen
	}
	return tabInputAdmitted, st.gen
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

// completeTabInput finishes a successful delivery under one tab.mu
// acquisition: the binding check and the post-delivery mutations are atomic
// so a retired writer can never stamp local echo onto a replacement binding.
// The byte budget releases here, at completion — a slow send holds the
// budget for the duration of its write (the sidebar releases at dequeue
// instead).
func (m *Model) completeTabInput(tab *Tab, w *tabInputWriter, req tabInputRequest) {
	refresh := false
	withMu(&tab.mu, func() {
		if tab.tabInput.writer == w {
			w.QueuedBytes -= len(req.Data)
			if req.Echo {
				recordLocalInputEchoWindowLocked(tab, req.Data, time.Now())
				refresh = m.isChatTabLocked(tab)
			}
		}
	})
	if refresh && m.msgSink != nil {
		m.msgSink(PTYCursorRefresh{WorkspaceID: req.WsID, TabID: TabID(req.TabID), InputGeneration: w.Gen})
	}
}

// failTabInput handles a delivery error. A failure on a binding that is
// still current latches the binding's failure flag and emits exactly one
// generation-stamped TabInputFailed — the guarded UI failure path owns the
// visible detach, so a tab stays healthy-looking (and its failure stays
// actionable) until the handler runs. A failure on a retired binding is a
// late signal: it is logged and dropped, and must not detach, toast, or
// stamp the replacement binding.
func (m *Model) failTabInput(tab *Tab, w *tabInputWriter, req tabInputRequest, err error) {
	current := withMuVal(&tab.mu, func() bool {
		if tab.tabInput.writer == w {
			tab.tabInput.failed = true
			return true
		}
		return false
	})
	if !current {
		logging.Debug("dropping stale %s failure for tab %s (input gen %d): %v", req.Label, tab.ID, w.Gen, err)
		return
	}
	logging.Error("%s failed for tab %s: %v", req.Label, tab.ID, err)
	if m.msgSink != nil {
		m.msgSink(TabInputFailed{TabID: TabID(req.TabID), WorkspaceID: req.WsID, Err: err, Gen: w.Gen})
	}
}

// retireTabInputWriterLocked detaches the current writer from the binding:
// marks it stale so queued and in-flight work discards, signals its exit,
// and moves it to the retired list for an off-lock join. Lifecycle
// transitions call this inside tab.mu; the caller joins via
// joinRetiredInputWriters after releasing the lock (and after closing the
// agent/terminal so an in-flight write can return). The generation does
// not change here — gen is bumped by the next attach, which is what makes
// a pre-attach failure stale.
func (t *Tab) retireTabInputWriterLocked() {
	if t == nil {
		return
	}
	w := t.tabInput.writer
	if w == nil {
		return
	}
	t.tabInput.writer = nil
	ptyio.RetireInputWriter(w)
	t.tabInput.retired = append(t.tabInput.retired, w)
}

// joinRetiredInputWriters drains the retired-writer list and joins each
// worker. Safe to call repeatedly; callers run it after the binding's
// terminal has been closed (or detached) so a blocked write has returned.
func (t *Tab) joinRetiredInputWriters() {
	if t == nil {
		return
	}
	retired := withMuVal(&t.mu, func() []*tabInputWriter {
		retired := t.tabInput.retired
		t.tabInput.retired = nil
		return retired
	})
	for _, w := range retired {
		ptyio.JoinInputWriter(w)
	}
}

// inputGenLocked reports the current input-binding generation. The reattach
// and create sites stamp it onto the response-writer closure so a query-write
// failure fences itself like a queued-input failure.
func (t *Tab) inputGenLocked() uint64 {
	return t.tabInput.gen
}
