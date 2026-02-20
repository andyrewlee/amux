package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

type failingWriter struct{}

func (failingWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("broken pipe")
}

func TestWatchExitsWhenOutputWriterFails(t *testing.T) {
	capture := fakeCaptureFunc(func(_ int) (string, bool) {
		return "hello", true
	})

	cfg := watchConfig{
		SessionName:   "test-session",
		Lines:         100,
		Interval:      1 * time.Millisecond,
		IdleThreshold: 1 * time.Nanosecond,
	}

	done := make(chan int, 1)
	go func() {
		done <- runWatchLoopWith(context.Background(), failingWriter{}, cfg, tmux.Options{}, capture)
	}()

	select {
	case code := <-done:
		if code != ExitOK {
			t.Fatalf("exit code = %d, want %d", code, ExitOK)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("watch loop did not exit after writer failure")
	}
}

func TestWatchEventHasTimestamp(t *testing.T) {
	var buf bytes.Buffer
	capture := fakeCapture("hello")

	cfg := watchConfig{
		SessionName:   "test-session",
		Lines:         100,
		Interval:      1 * time.Millisecond,
		IdleThreshold: 1 * time.Hour,
	}

	runWatchLoopWith(context.Background(), &buf, cfg, tmux.Options{}, capture)

	events := parseEvents(t, buf.String())
	for i, ev := range events {
		if ev.Timestamp == "" {
			t.Errorf("events[%d].Timestamp is empty", i)
		}
		if _, err := time.Parse(time.RFC3339, ev.Timestamp); err != nil {
			t.Errorf("events[%d].Timestamp %q is not valid RFC3339: %v", i, ev.Timestamp, err)
		}
	}
}

func TestWatchEventHasHash(t *testing.T) {
	var buf bytes.Buffer
	capture := fakeCapture("hello")

	cfg := watchConfig{
		SessionName:   "test-session",
		Lines:         100,
		Interval:      1 * time.Millisecond,
		IdleThreshold: 1 * time.Hour,
	}

	runWatchLoopWith(context.Background(), &buf, cfg, tmux.Options{}, capture)

	events := parseEvents(t, buf.String())
	if events[0].Hash == "" {
		t.Error("snapshot event hash is empty")
	}
	// MD5 hex is 32 characters
	if len(events[0].Hash) != 32 {
		t.Errorf("snapshot event hash length = %d, want 32", len(events[0].Hash))
	}
}
