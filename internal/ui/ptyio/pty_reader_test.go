package ptyio

import (
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// scriptedReader drives RunPTYReader through deterministic exit paths: it
// returns each chunk in order, then (optionally blocking on block) returns
// finalErr, defaulting to io.EOF.
type scriptedReader struct {
	chunks   [][]byte
	idx      int
	block    <-chan struct{}
	finalErr error
}

func (s *scriptedReader) Read(p []byte) (int, error) {
	if s.idx < len(s.chunks) {
		n := copy(p, s.chunks[s.idx])
		s.idx++
		return n, nil
	}
	if s.block != nil {
		<-s.block
	}
	if s.finalErr != nil {
		return 0, s.finalErr
	}
	return 0, io.EOF
}

type testOutputMsg struct{ data []byte }

type testStoppedMsg struct{ err error }

type timeoutErr struct{}

func (timeoutErr) Error() string { return "deadline exceeded" }
func (timeoutErr) Timeout() bool { return true }

type deadlinePollingReader struct {
	deadlineSet chan struct{}
	cleared     chan struct{}
	onceSet     sync.Once
	onceCleared sync.Once
}

func newDeadlinePollingReader() *deadlinePollingReader {
	return &deadlinePollingReader{
		deadlineSet: make(chan struct{}),
		cleared:     make(chan struct{}),
	}
}

func (r *deadlinePollingReader) SetReadDeadline(deadline time.Time) error {
	if deadline.IsZero() {
		r.onceCleared.Do(func() { close(r.cleared) })
		return nil
	}
	r.onceSet.Do(func() { close(r.deadlineSet) })
	return nil
}

func (r *deadlinePollingReader) Read([]byte) (int, error) {
	return 0, timeoutErr{}
}

type collectedPTY struct {
	outputs []testOutputMsg
	stops   []testStoppedMsg
}

func (c collectedPTY) outputBytes() []byte {
	var b []byte
	for _, o := range c.outputs {
		b = append(b, o.data...)
	}
	return b
}

// runReaderAndForward wires RunPTYReader to ForwardPTYMsgs over an unbuffered
// msgCh (so ForwardPTYMsgs is forced to drain), runs both to completion, and
// fails if either fails to return within the timeout. ForwardPTYMsgs only
// returns when msgCh is closed, so a clean return proves msgCh was closed
// exactly once: a missing close would hang, a double close would panic (there
// is no longer a recover() to swallow it).
func runReaderAndForward(t *testing.T, r io.Reader, cancel <-chan struct{}, cfg PTYReaderConfig) collectedPTY {
	t.Helper()

	msgCh := make(chan tea.Msg)
	var hb int64
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

	var mu sync.Mutex
	var got collectedPTY
	forwardDone := make(chan struct{})
	go func() {
		ForwardPTYMsgs(msgCh, func(m tea.Msg) {
			mu.Lock()
			defer mu.Unlock()
			switch v := m.(type) {
			case testOutputMsg:
				got.outputs = append(got.outputs, v)
			case testStoppedMsg:
				got.stops = append(got.stops, v)
			}
		}, merger)
		close(forwardDone)
	}()

	readerDone := make(chan struct{})
	go func() {
		RunPTYReader(r, msgCh, cancel, &hb, cfg, factory)
		close(readerDone)
	}()

	select {
	case <-readerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("RunPTYReader did not return within 2s (msgCh likely never closed)")
	}
	select {
	case <-forwardDone:
	case <-time.After(2 * time.Second):
		t.Fatal("ForwardPTYMsgs did not return within 2s (msgCh not closed exactly once)")
	}

	mu.Lock()
	defer mu.Unlock()
	return got
}

func baseReaderCfg() PTYReaderConfig {
	return PTYReaderConfig{
		Label:           "test",
		ReadBufferSize:  4096,
		ReadQueueSize:   8,
		FrameInterval:   10 * time.Millisecond,
		MaxPendingBytes: 1 << 20,
	}
}

func TestRunPTYReaderClosesOnceOnEOF(t *testing.T) {
	cancel := make(chan struct{})
	got := runReaderAndForward(t, &scriptedReader{chunks: [][]byte{[]byte("hello")}}, cancel, baseReaderCfg())

	if string(got.outputBytes()) != "hello" {
		t.Fatalf("output = %q, want %q", got.outputBytes(), "hello")
	}
	if len(got.stops) != 1 {
		t.Fatalf("got %d Stopped msgs, want 1", len(got.stops))
	}
	if got.stops[0].err != io.EOF {
		t.Fatalf("Stopped err = %v, want io.EOF", got.stops[0].err)
	}
}

func TestRunPTYReaderClosesOnceOnError(t *testing.T) {
	cancel := make(chan struct{})
	got := runReaderAndForward(t, &scriptedReader{finalErr: errors.New("boom")}, cancel, baseReaderCfg())

	// The terminal error may be the read error or io.EOF depending on whether
	// the err channel or the closed data channel is observed first; both are
	// non-nil. The contract under test is exactly-once close, asserted by
	// runReaderAndForward returning rather than hanging or panicking.
	if len(got.stops) != 1 {
		t.Fatalf("got %d Stopped msgs, want 1", len(got.stops))
	}
	if got.stops[0].err == nil {
		t.Fatal("Stopped err = nil, want non-nil")
	}
}

func TestRunPTYReaderFlushesOnMaxPending(t *testing.T) {
	cfg := baseReaderCfg()
	cfg.FrameInterval = time.Hour // never tick: isolate the max-pending flush path
	cfg.MaxPendingBytes = 4
	cancel := make(chan struct{})

	got := runReaderAndForward(t, &scriptedReader{chunks: [][]byte{[]byte("ABCDEFGH")}}, cancel, cfg)

	if string(got.outputBytes()) != "ABCDEFGH" {
		t.Fatalf("output = %q, want ABCDEFGH (max-pending flush)", got.outputBytes())
	}
	if len(got.stops) != 1 || got.stops[0].err != io.EOF {
		t.Fatalf("stops = %+v, want exactly one io.EOF", got.stops)
	}
}

func TestRunPTYReaderClosesOnceOnCancel(t *testing.T) {
	cancel := make(chan struct{})
	release := make(chan struct{})
	defer close(release) // unblock the leaked read goroutine after the test

	// No chunks: the read goroutine blocks on release (not cancel), so closing
	// cancel exercises the loop's cancel path deterministically without racing
	// an EOF from the reader.
	go func() {
		time.Sleep(20 * time.Millisecond)
		close(cancel)
	}()
	got := runReaderAndForward(t, &scriptedReader{block: release}, cancel, baseReaderCfg())

	if len(got.outputs) != 0 {
		t.Fatalf("got %d outputs on cancel, want 0", len(got.outputs))
	}
	if len(got.stops) != 0 {
		t.Fatalf("got %d Stopped msgs on cancel, want 0 (cancel path sends no Stopped)", len(got.stops))
	}
}

func TestRunPTYReaderCancelWakesDeadlineReader(t *testing.T) {
	cancel := make(chan struct{})
	reader := newDeadlinePollingReader()
	go func() {
		<-reader.deadlineSet
		close(cancel)
	}()

	got := runReaderAndForward(t, reader, cancel, baseReaderCfg())

	if len(got.outputs) != 0 {
		t.Fatalf("got %d outputs on cancel, want 0", len(got.outputs))
	}
	if len(got.stops) != 0 {
		t.Fatalf("got %d Stopped msgs on cancel, want 0", len(got.stops))
	}
	select {
	case <-reader.cleared:
	case <-time.After(2 * time.Second):
		t.Fatal("read deadline was not cleared when reader exited")
	}
}
