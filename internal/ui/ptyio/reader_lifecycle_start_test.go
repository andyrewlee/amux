package ptyio

import (
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// startReaderHarness wires StartReaderOptions around a scripted reader so
// StartReader's spawned goroutines run a real (in-process) read loop and drain
// path without any tmux/PTY/Bubble Tea dependency. The Forward closure records
// every message and signals forwardDone when the channel closes, which proves
// the reader and forwarder both ran to completion.
type startReaderHarness struct {
	opts        StartReaderOptions
	forwardDone chan struct{}

	mu      sync.Mutex
	outputs [][]byte
	stops   []error
}

func newStartReaderHarness(term io.Reader) *startReaderHarness {
	h := &startReaderHarness{forwardDone: make(chan struct{})}
	factory := PTYMsgFactory{
		Output: func(data []byte) tea.Msg {
			cp := make([]byte, len(data))
			copy(cp, data)
			return testOutputMsg{data: cp}
		},
		Stopped: func(err error) tea.Msg { return testStoppedMsg{err: err} },
	}
	merger := OutputMerger{
		ExtractData: func(msg tea.Msg) ([]byte, bool) {
			o, ok := msg.(testOutputMsg)
			if !ok {
				return nil, false
			}
			return o.data, true
		},
		CanMerge:   func(_, _ tea.Msg) bool { return true },
		Build:      func(_ tea.Msg, data []byte) tea.Msg { return testOutputMsg{data: data} },
		MaxPending: 1 << 20,
	}
	h.opts = StartReaderOptions{
		AcquireTerm:  func() io.Reader { return term },
		Config:       baseReaderCfg(),
		Factory:      factory,
		ReaderLabel:  "test-reader",
		ForwardLabel: "test-forward",
		Forward: func(ch <-chan tea.Msg) {
			ForwardPTYMsgs(ch, func(m tea.Msg) {
				h.mu.Lock()
				defer h.mu.Unlock()
				switch v := m.(type) {
				case testOutputMsg:
					h.outputs = append(h.outputs, v.data)
				case testStoppedMsg:
					h.stops = append(h.stops, v.err)
				}
			}, merger)
			close(h.forwardDone)
		},
	}
	return h
}

// waitForward blocks until the Forward goroutine returns (msgCh closed) or the
// timeout elapses, failing the test on timeout.
func (h *startReaderHarness) waitForward(t *testing.T) {
	t.Helper()
	select {
	case <-h.forwardDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Forward did not return within 2s (reader never closed msgCh)")
	}
}

func (h *startReaderHarness) outputBytes() []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	var b []byte
	for _, o := range h.outputs {
		b = append(b, o...)
	}
	return b
}

func (h *startReaderHarness) stopErrs() []error {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]error, len(h.stops))
	copy(out, h.stops)
	return out
}

func TestStartReaderRunsAndMarksStopped(t *testing.T) {
	var mu sync.Mutex
	st := &State{}
	h := newStartReaderHarness(&scriptedReader{chunks: [][]byte{[]byte("hello")}})

	st.StartReader(&mu, h.opts)

	// The reader starts active with a fresh heartbeat and a live cancel channel.
	mu.Lock()
	if !st.ReaderActive {
		mu.Unlock()
		t.Fatal("ReaderActive = false immediately after StartReader, want true")
	}
	if st.MsgCh == nil {
		mu.Unlock()
		t.Fatal("MsgCh = nil after StartReader, want a channel")
	}
	if st.ReaderCancel == nil {
		mu.Unlock()
		t.Fatal("ReaderCancel = nil after StartReader, want a channel")
	}
	mu.Unlock()

	h.waitForward(t)

	if got := string(h.outputBytes()); got != "hello" {
		t.Fatalf("forwarded output = %q, want %q", got, "hello")
	}
	if errs := h.stopErrs(); len(errs) != 1 || errs[0] != io.EOF {
		t.Fatalf("stop errs = %v, want exactly one io.EOF", errs)
	}

	// MarkReaderStopped ran on reader exit: state cleared, heartbeat zeroed.
	mu.Lock()
	defer mu.Unlock()
	if st.ReaderActive {
		t.Fatal("ReaderActive = true after reader exit, want false")
	}
	if st.MsgCh != nil {
		t.Fatal("MsgCh != nil after reader exit, want nil")
	}
	if hb := atomic.LoadInt64(&st.Heartbeat); hb != 0 {
		t.Fatalf("Heartbeat = %d after reader exit, want 0", hb)
	}
}

func TestStartReaderResetsBackoffOnStart(t *testing.T) {
	var mu sync.Mutex
	st := &State{RestartBackoff: 3 * time.Second}
	h := newStartReaderHarness(&scriptedReader{chunks: [][]byte{[]byte("x")}})

	st.StartReader(&mu, h.opts)
	h.waitForward(t)

	// StartReader clears RestartBackoff to 0 before launching the reader.
	mu.Lock()
	defer mu.Unlock()
	if st.RestartBackoff != 0 {
		t.Fatalf("RestartBackoff = %v after StartReader, want 0", st.RestartBackoff)
	}
}

func TestStartReaderNilTermDoesNotStart(t *testing.T) {
	var mu sync.Mutex
	st := &State{}
	opts := newStartReaderHarness(nil).opts
	opts.AcquireTerm = func() io.Reader { return nil }

	st.StartReader(&mu, opts)

	mu.Lock()
	defer mu.Unlock()
	if st.ReaderActive {
		t.Fatal("ReaderActive = true when AcquireTerm returned nil, want false")
	}
	if st.MsgCh != nil {
		t.Fatal("MsgCh != nil when AcquireTerm returned nil, want nil")
	}
	if st.ReaderCancel != nil {
		t.Fatal("ReaderCancel != nil when AcquireTerm returned nil, want nil")
	}
}

func TestStartReaderSkipsWhenAlreadyActive(t *testing.T) {
	var mu sync.Mutex
	existingCancel := make(chan struct{})
	existingCh := make(chan tea.Msg)
	st := &State{
		ReaderActive: true,
		ReaderCancel: existingCancel,
		MsgCh:        existingCh,
	}
	acquired := false
	opts := newStartReaderHarness(&scriptedReader{}).opts
	opts.AcquireTerm = func() io.Reader {
		acquired = true
		return &scriptedReader{}
	}

	st.StartReader(&mu, opts)

	if acquired {
		t.Fatal("AcquireTerm was called for an already-active reader, want skipped")
	}
	mu.Lock()
	defer mu.Unlock()
	if st.ReaderCancel != existingCancel {
		t.Fatal("ReaderCancel was replaced while a reader was active")
	}
	if st.MsgCh != existingCh {
		t.Fatal("MsgCh was replaced while a reader was active")
	}
}

func TestStartReaderRearmsInconsistentActiveState(t *testing.T) {
	var mu sync.Mutex
	// ReaderActive is set but the bookkeeping channels are nil: a leftover
	// inconsistent state that StartReader must treat as "not running" and rearm.
	st := &State{ReaderActive: true, ReaderCancel: nil, MsgCh: nil}
	h := newStartReaderHarness(&scriptedReader{chunks: [][]byte{[]byte("rearm")}})

	st.StartReader(&mu, h.opts)
	h.waitForward(t)

	if got := string(h.outputBytes()); got != "rearm" {
		t.Fatalf("output = %q, want %q (reader should have rearmed)", got, "rearm")
	}
}

func TestStartReaderClosesLeftoverCancel(t *testing.T) {
	var mu sync.Mutex
	// Realistic post-MarkReaderStopped shape: the previous reader exited on its
	// own, so ReaderActive is false and MsgCh is nil, but ReaderCancel was left
	// in place (an open channel) for the next StartReader to close. StartReader
	// must close that leftover channel and swap in a fresh, distinct one.
	leftoverCancel := make(chan struct{})
	st := &State{ReaderActive: false, ReaderCancel: leftoverCancel, MsgCh: nil}
	h := newStartReaderHarness(&scriptedReader{chunks: [][]byte{[]byte("swap")}})

	st.StartReader(&mu, h.opts)
	h.waitForward(t)

	// The leftover cancel channel was closed (a receive succeeds, not blocks).
	select {
	case <-leftoverCancel:
	default:
		t.Fatal("leftover ReaderCancel was not closed by StartReader")
	}

	mu.Lock()
	defer mu.Unlock()
	if st.ReaderCancel == nil {
		t.Fatal("ReaderCancel = nil after StartReader, want a fresh channel")
	}
	if st.ReaderCancel == leftoverCancel {
		t.Fatal("ReaderCancel still points at the leftover channel, want a fresh distinct one")
	}
}
