package ptyio

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// eventStep is one scripted Read result: the bytes and the error a single
// call returns together. Real PTY reads may legally return both.
type eventStep struct {
	data []byte
	err  error
}

// eventScriptReader returns each step's (data, err) pair in order, then
// blocks (or returns io.EOF) once the script is exhausted. When the script's
// last step carries an error it is the terminal read; lastReadReached is
// closed as it is consumed so tests can observe "the producer has published
// everything" without sleeping.
type eventScriptReader struct {
	steps           []eventStep
	idx             int
	lastReadReached chan struct{}
	once            sync.Once
}

func (s *eventScriptReader) Read(p []byte) (int, error) {
	if s.idx >= len(s.steps) {
		return 0, io.EOF
	}
	step := s.steps[s.idx]
	s.idx++
	if s.idx == len(s.steps) && s.lastReadReached != nil {
		s.once.Do(func() { close(s.lastReadReached) })
	}
	return copy(p, step.data), step.err
}

func outputBytesOf(outputs []testOutputMsg) []byte {
	var b []byte
	for _, o := range outputs {
		b = append(b, o.data...)
	}
	return b
}

func assertSingleStop(t *testing.T, stops []testStoppedMsg, want error) {
	t.Helper()
	if len(stops) != 1 {
		t.Fatalf("got %d Stopped msgs, want exactly 1", len(stops))
	}
	if !errors.Is(stops[0].err, want) {
		t.Fatalf("Stopped err = %v, want %v", stops[0].err, want)
	}
}

// TestRunPTYReaderDataAndEOFInSameCall is the core regression: a Read that
// returns bytes AND io.EOF in one call must still deliver the bytes before
// the single Stopped. The pre-fix reader tested err first and discarded n>0.
func TestRunPTYReaderDataAndEOFInSameCall(t *testing.T) {
	cancel := make(chan struct{})
	payload := []byte("tail bytes that arrive with EOF")
	r := &eventScriptReader{steps: []eventStep{{data: payload, err: io.EOF}}}

	got := runReaderAndForward(t, r, cancel, baseReaderCfg())

	if string(got.outputBytes()) != string(payload) {
		t.Fatalf("output = %q, want %q — same-call data was dropped", got.outputBytes(), payload)
	}
	assertSingleStop(t, got.stops, io.EOF)
}

// TestRunPTYReaderDataAndErrorInSameCall covers a non-EOF terminal error
// (EIO-style) arriving with final diagnostics in the same Read.
func TestRunPTYReaderDataAndErrorInSameCall(t *testing.T) {
	sentinel := errors.New("simulated EIO")
	cancel := make(chan struct{})
	payload := []byte("panic output delivered with EIO")
	r := &eventScriptReader{steps: []eventStep{{data: payload, err: sentinel}}}

	got := runReaderAndForward(t, r, cancel, baseReaderCfg())

	if string(got.outputBytes()) != string(payload) {
		t.Fatalf("output = %q, want %q", got.outputBytes(), payload)
	}
	assertSingleStop(t, got.stops, sentinel)
}

// TestRunPTYReaderDataWithTimeoutThenMore verifies a timeout that carries
// bytes is a retry signal, not data loss: the bytes are forwarded and the
// next read continues the stream.
func TestRunPTYReaderDataWithTimeoutThenMore(t *testing.T) {
	cancel := make(chan struct{})
	r := &eventScriptReader{steps: []eventStep{
		{data: []byte("first-"), err: timeoutErr{}},
		{data: []byte("second"), err: nil},
		{data: []byte("-tail"), err: io.EOF},
	}}

	got := runReaderAndForward(t, r, cancel, baseReaderCfg())

	if string(got.outputBytes()) != "first-second-tail" {
		t.Fatalf("output = %q, want %q", got.outputBytes(), "first-second-tail")
	}
	assertSingleStop(t, got.stops, io.EOF)
}

// TestRunPTYReaderQueuedTailDeliveredBeforeStopped proves ordering under a
// full queue: uniquely numbered chunks past MaxPendingBytes must all reach
// the sink, in order, before the one Stopped — a terminal event may never
// overtake queued data.
func TestRunPTYReaderQueuedTailDeliveredBeforeStopped(t *testing.T) {
	cancel := make(chan struct{})
	const chunks = 64
	steps := make([]eventStep, 0, chunks+1)
	var want []byte
	for i := 0; i < chunks; i++ {
		b := []byte(fmt.Sprintf("%04d|", i))
		steps = append(steps, eventStep{data: b})
		want = append(want, b...)
	}
	steps = append(steps, eventStep{err: io.EOF})

	cfg := baseReaderCfg()
	// The read queue must hold every published event while the consumer is
	// paused — a producer blocked on a full queue can never reach the
	// terminal read this test waits on.
	cfg.ReadQueueSize = chunks + 2
	cfg.MaxPendingBytes = 16      // force many size-triggered flushes
	cfg.FrameInterval = time.Hour // isolate the size-flush path

	// Slow the consumer so the read queue genuinely fills before drain: gate
	// the forwarder's sink until the reader's terminal read has run.
	release := make(chan struct{})
	r := &eventScriptReader{steps: steps, lastReadReached: make(chan struct{})}

	msgCh := make(chan tea.Msg)
	var hb int64
	factory := PTYMsgFactory{
		Output:  func(data []byte) tea.Msg { return testOutputMsg{data: append([]byte(nil), data...)} },
		Stopped: func(err error) tea.Msg { return testStoppedMsg{err: err} },
	}
	merger := OutputMerger{
		ExtractData: func(m tea.Msg) ([]byte, bool) {
			o, ok := m.(testOutputMsg)
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
	fwdDone := make(chan struct{})
	go func() {
		first := true
		ForwardPTYMsgs(msgCh, func(m tea.Msg) {
			if first {
				<-release // hold the first delivery until the producer is done
				first = false
			}
			mu.Lock()
			defer mu.Unlock()
			switch v := m.(type) {
			case testOutputMsg:
				got.outputs = append(got.outputs, v)
			case testStoppedMsg:
				got.stops = append(got.stops, v)
			}
		}, merger)
		close(fwdDone)
	}()

	readerDone := make(chan struct{})
	go func() {
		RunPTYReader(r, msgCh, cancel, &hb, cfg, factory)
		close(readerDone)
	}()

	select {
	case <-r.lastReadReached:
	case <-time.After(2 * time.Second):
		t.Fatal("reader never reached its terminal read")
	}
	close(release)

	select {
	case <-readerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("RunPTYReader did not return after queue drain")
	}
	select {
	case <-fwdDone:
	case <-time.After(2 * time.Second):
		t.Fatal("ForwardPTYMsgs did not return (msgCh not closed exactly once)")
	}

	mu.Lock()
	defer mu.Unlock()
	gotBytes := got.outputBytes()
	if string(gotBytes) != string(want) {
		t.Fatalf("delivered bytes = %q, want complete ordered %q", gotBytes, want)
	}
	assertSingleStop(t, got.stops, io.EOF)
}

// TestRunPTYReaderFlushTickUnderBackpressure covers the elapsed-tick flush
// racing a blocked consumer: pending bytes flush on the tick once the sink
// unblocks, and the terminal event still lands last.
func TestRunPTYReaderFlushTickUnderBackpressure(t *testing.T) {
	cancel := make(chan struct{})
	release := make(chan struct{})
	r := &eventScriptReader{steps: []eventStep{
		{data: []byte("before-tick")},
		{err: io.EOF},
	}, lastReadReached: make(chan struct{})}

	cfg := baseReaderCfg()
	cfg.FrameInterval = 5 * time.Millisecond // tick quickly
	cfg.MaxPendingBytes = 1 << 20            // size flush never triggers

	msgCh := make(chan tea.Msg)
	var hb int64
	factory := PTYMsgFactory{
		Output:  func(data []byte) tea.Msg { return testOutputMsg{data: append([]byte(nil), data...)} },
		Stopped: func(err error) tea.Msg { return testStoppedMsg{err: err} },
	}

	var mu sync.Mutex
	var outputs []testOutputMsg
	var stops []testStoppedMsg
	fwdDone := make(chan struct{})
	go func() {
		first := true
		for msg := range msgCh {
			if first {
				<-release // hold the first receive until the producer finished
				first = false
			}
			mu.Lock()
			switch v := msg.(type) {
			case testOutputMsg:
				outputs = append(outputs, v)
			case testStoppedMsg:
				stops = append(stops, v)
			}
			mu.Unlock()
		}
		close(fwdDone)
	}()

	readerDone := make(chan struct{})
	go func() {
		RunPTYReader(r, msgCh, cancel, &hb, cfg, factory)
		close(readerDone)
	}()

	select {
	case <-r.lastReadReached:
	case <-time.After(2 * time.Second):
		t.Fatal("reader never reached its terminal read")
	}
	close(release)

	select {
	case <-readerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("RunPTYReader did not return")
	}
	select {
	case <-fwdDone:
	case <-time.After(2 * time.Second):
		t.Fatal("consumer did not finish")
	}

	mu.Lock()
	defer mu.Unlock()
	if string(outputBytesOf(outputs)) != "before-tick" {
		t.Fatalf("output = %q, want %q", outputBytesOf(outputs), "before-tick")
	}
	assertSingleStop(t, stops, io.EOF)
}

// TestRunPTYReaderEmptyEOF verifies a bare EOF produces no output and exactly
// one Stopped carrying io.EOF.
func TestRunPTYReaderEmptyEOF(t *testing.T) {
	cancel := make(chan struct{})
	r := &eventScriptReader{steps: []eventStep{{err: io.EOF}}}

	got := runReaderAndForward(t, r, cancel, baseReaderCfg())

	if len(got.outputs) != 0 {
		t.Fatalf("empty stream produced %d outputs, want 0", len(got.outputs))
	}
	assertSingleStop(t, got.stops, io.EOF)
}
