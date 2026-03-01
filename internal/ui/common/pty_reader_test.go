package common

import (
	"bytes"
	"io"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

type testOutputMsg struct {
	key  string
	data []byte
}

type testOtherMsg struct {
	n int
}

func TestForwardPTYMsgsDrainWindowMergesNearbyOutput(t *testing.T) {
	msgCh := make(chan tea.Msg, 4)
	var got []testOutputMsg
	done := make(chan struct{})

	go func() {
		ForwardPTYMsgs(msgCh, func(msg tea.Msg) {
			if out, ok := msg.(testOutputMsg); ok {
				got = append(got, out)
			}
		}, OutputMerger{
			ExtractData: func(msg tea.Msg) ([]byte, bool) {
				out, ok := msg.(testOutputMsg)
				return out.data, ok
			},
			CanMerge: func(current, next tea.Msg) bool {
				a, okA := current.(testOutputMsg)
				b, okB := next.(testOutputMsg)
				return okA && okB && a.key == b.key
			},
			Build: func(first tea.Msg, data []byte) tea.Msg {
				out, _ := first.(testOutputMsg)
				out.data = data
				return out
			},
			MaxPending:  1024,
			DrainWindow: 25 * time.Millisecond,
		})
		close(done)
	}()

	msgCh <- testOutputMsg{key: "tab-1", data: []byte("hello ")}
	time.Sleep(5 * time.Millisecond)
	msgCh <- testOutputMsg{key: "tab-1", data: []byte("world")}
	close(msgCh)
	<-done

	if len(got) != 1 {
		t.Fatalf("expected 1 merged output msg, got %d", len(got))
	}
	if !bytes.Equal(got[0].data, []byte("hello world")) {
		t.Fatalf("expected merged payload %q, got %q", "hello world", string(got[0].data))
	}
}

func TestForwardPTYMsgsDrainWindowFlushesBeforeNonMerge(t *testing.T) {
	msgCh := make(chan tea.Msg, 4)
	var outCount int
	var otherCount int
	done := make(chan struct{})

	go func() {
		ForwardPTYMsgs(msgCh, func(msg tea.Msg) {
			switch msg.(type) {
			case testOutputMsg:
				outCount++
			case testOtherMsg:
				otherCount++
			}
		}, OutputMerger{
			ExtractData: func(msg tea.Msg) ([]byte, bool) {
				out, ok := msg.(testOutputMsg)
				return out.data, ok
			},
			CanMerge: func(current, next tea.Msg) bool {
				a, okA := current.(testOutputMsg)
				b, okB := next.(testOutputMsg)
				return okA && okB && a.key == b.key
			},
			Build: func(first tea.Msg, data []byte) tea.Msg {
				out, _ := first.(testOutputMsg)
				out.data = data
				return out
			},
			MaxPending:  1024,
			DrainWindow: 25 * time.Millisecond,
		})
		close(done)
	}()

	msgCh <- testOutputMsg{key: "tab-1", data: []byte("abc")}
	msgCh <- testOtherMsg{n: 1}
	close(msgCh)
	<-done

	if outCount != 1 {
		t.Fatalf("expected 1 output msg, got %d", outCount)
	}
	if otherCount != 1 {
		t.Fatalf("expected 1 non-output msg, got %d", otherCount)
	}
}

func TestRunPTYReaderToSink_ForwardsOutputAndStopped(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()

	done := make(chan struct{})
	var got bytes.Buffer
	var stoppedErr error

	go func() {
		RunPTYReaderToSink(pr, make(chan struct{}), nil, PTYReaderConfig{
			Label:           "test.pty_reader_to_sink",
			ReadBufferSize:  32,
			ReadQueueSize:   8,
			FrameInterval:   2 * time.Millisecond,
			MaxPendingBytes: 1024,
		}, PTYDataSink{
			Output: func(data []byte) bool {
				_, _ = got.Write(data)
				return true
			},
			Stopped: func(err error) bool {
				stoppedErr = err
				close(done)
				return true
			},
		})
	}()

	if _, err := pw.Write([]byte("hello")); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	_ = pw.Close()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for sink stop callback")
	}

	if got.String() != "hello" {
		t.Fatalf("output = %q, want %q", got.String(), "hello")
	}
	if stoppedErr == nil {
		t.Fatal("expected stopped callback error, got nil")
	}
}
