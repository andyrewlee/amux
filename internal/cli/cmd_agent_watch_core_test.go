package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

func fakeCapture(contents ...string) captureFn {
	idx := 0
	return func(_ string, _ int, _ tmux.Options) (string, bool) {
		if idx >= len(contents) {
			return "", false
		}
		c := contents[idx]
		idx++
		return c, true
	}
}

// fakeCaptureFunc returns a capture function backed by a custom func.
func fakeCaptureFunc(fn func(call int) (string, bool)) captureFn {
	call := 0
	return func(_ string, _ int, _ tmux.Options) (string, bool) {
		c, ok := fn(call)
		call++
		return c, ok
	}
}

func parseEvents(t *testing.T, output string) []watchEvent {
	t.Helper()
	var events []watchEvent
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		var ev watchEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("failed to parse event JSON %q: %v", line, err)
		}
		events = append(events, ev)
	}
	return events
}

func TestWatchEmitsSnapshotThenExited(t *testing.T) {
	var buf bytes.Buffer
	capture := fakeCapture("hello world")

	cfg := watchConfig{
		SessionName:   "test-session",
		Lines:         100,
		Interval:      1 * time.Millisecond,
		IdleThreshold: 1 * time.Hour, // won't trigger
	}

	code := runWatchLoopWith(context.Background(), &buf, cfg, tmux.Options{}, capture)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}

	events := parseEvents(t, buf.String())
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2: %v", len(events), events)
	}
	if events[0].Type != "snapshot" {
		t.Errorf("events[0].Type = %q, want snapshot", events[0].Type)
	}
	if events[0].Content != "hello world" {
		t.Errorf("events[0].Content = %q, want %q", events[0].Content, "hello world")
	}
	if events[1].Type != "exited" {
		t.Errorf("events[1].Type = %q, want exited", events[1].Type)
	}
}

func TestWatchEmitsDeltaOnChange(t *testing.T) {
	var buf bytes.Buffer
	capture := fakeCapture("line1\nline2", "line1\nline2\nline3\nline4")

	cfg := watchConfig{
		SessionName:   "test-session",
		Lines:         100,
		Interval:      1 * time.Millisecond,
		IdleThreshold: 1 * time.Hour,
	}

	code := runWatchLoopWith(context.Background(), &buf, cfg, tmux.Options{}, capture)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}

	events := parseEvents(t, buf.String())
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3: %v", len(events), events)
	}
	if events[0].Type != "snapshot" {
		t.Errorf("events[0].Type = %q, want snapshot", events[0].Type)
	}
	if events[1].Type != "delta" {
		t.Errorf("events[1].Type = %q, want delta", events[1].Type)
	}
	if len(events[1].NewLines) != 2 || events[1].NewLines[0] != "line3" || events[1].NewLines[1] != "line4" {
		t.Errorf("events[1].NewLines = %v, want [line3, line4]", events[1].NewLines)
	}
	if events[2].Type != "exited" {
		t.Errorf("events[2].Type = %q, want exited", events[2].Type)
	}
}

func TestWatchDeltaMarksNeedsInput(t *testing.T) {
	var buf bytes.Buffer
	capture := fakeCapture("ready", "ready\nDo you want me to continue? (y/N)")

	cfg := watchConfig{
		SessionName:   "test-session",
		Lines:         100,
		Interval:      1 * time.Millisecond,
		IdleThreshold: 1 * time.Hour,
	}

	code := runWatchLoopWith(context.Background(), &buf, cfg, tmux.Options{}, capture)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}

	events := parseEvents(t, buf.String())
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3: %v", len(events), events)
	}
	if events[1].Type != "delta" {
		t.Fatalf("events[1].Type = %q, want delta", events[1].Type)
	}
	if !events[1].NeedsInput {
		t.Fatalf("delta needs_input = false, want true")
	}
	if events[1].InputHint != "Do you want me to continue? (y/N)" {
		t.Fatalf("delta input_hint = %q", events[1].InputHint)
	}
	if events[1].LatestLine != "Do you want me to continue? (y/N)" {
		t.Fatalf("delta latest_line = %q", events[1].LatestLine)
	}
}

func TestWatchEmitsIdleAfterThreshold(t *testing.T) {
	var buf bytes.Buffer
	// Same content twice → triggers idle, then session exits
	capture := fakeCapture("hello", "hello")

	cfg := watchConfig{
		SessionName:   "test-session",
		Lines:         100,
		Interval:      1 * time.Millisecond,
		IdleThreshold: 1 * time.Nanosecond, // immediate idle
	}

	code := runWatchLoopWith(context.Background(), &buf, cfg, tmux.Options{}, capture)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}

	events := parseEvents(t, buf.String())
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3: %v", len(events), events)
	}
	if events[0].Type != "snapshot" {
		t.Errorf("events[0].Type = %q, want snapshot", events[0].Type)
	}
	if events[1].Type != "idle" {
		t.Errorf("events[1].Type = %q, want idle", events[1].Type)
	}
	if events[1].IdleSeconds <= 0 {
		t.Errorf("events[1].IdleSeconds = %f, want > 0", events[1].IdleSeconds)
	}
	if events[2].Type != "exited" {
		t.Errorf("events[2].Type = %q, want exited", events[2].Type)
	}
}

func TestWatchIdleEmittedOnlyOnce(t *testing.T) {
	var buf bytes.Buffer
	// Same content three times → idle emitted once, then exit
	capture := fakeCapture("hello", "hello", "hello")

	cfg := watchConfig{
		SessionName:   "test-session",
		Lines:         100,
		Interval:      1 * time.Millisecond,
		IdleThreshold: 1 * time.Nanosecond,
	}

	code := runWatchLoopWith(context.Background(), &buf, cfg, tmux.Options{}, capture)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}

	events := parseEvents(t, buf.String())
	idleCount := 0
	for _, ev := range events {
		if ev.Type == "idle" {
			idleCount++
		}
	}
	if idleCount != 1 {
		t.Errorf("idle events = %d, want 1", idleCount)
	}
}

func TestWatchEmitsHeartbeatWhenConfigured(t *testing.T) {
	var buf bytes.Buffer
	capture := fakeCaptureFunc(func(call int) (string, bool) {
		if call < 8 {
			return "still working", true
		}
		return "", false
	})

	cfg := watchConfig{
		SessionName:   "test-session",
		Lines:         100,
		Interval:      1 * time.Millisecond,
		IdleThreshold: 1 * time.Hour,
		Heartbeat:     2 * time.Millisecond,
	}

	code := runWatchLoopWith(context.Background(), &buf, cfg, tmux.Options{}, capture)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}

	events := parseEvents(t, buf.String())
	heartbeatCount := 0
	for _, ev := range events {
		if ev.Type == "heartbeat" {
			heartbeatCount++
			if ev.HeartbeatSeconds <= 0 {
				t.Fatalf("heartbeat_seconds = %f, want > 0", ev.HeartbeatSeconds)
			}
			if ev.Summary == "" {
				t.Fatalf("heartbeat summary is empty")
			}
		}
	}
	if heartbeatCount == 0 {
		t.Fatalf("expected at least one heartbeat event, got events=%v", events)
	}
}

func TestWatchIdleResetsAfterDelta(t *testing.T) {
	var buf bytes.Buffer
	// Same → idle, change → delta (resets idle), same → idle again
	capture := fakeCapture("hello", "hello", "hello world", "hello world")

	cfg := watchConfig{
		SessionName:   "test-session",
		Lines:         100,
		Interval:      1 * time.Millisecond,
		IdleThreshold: 1 * time.Nanosecond,
	}

	code := runWatchLoopWith(context.Background(), &buf, cfg, tmux.Options{}, capture)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}

	events := parseEvents(t, buf.String())
	var types []string
	for _, ev := range events {
		types = append(types, ev.Type)
	}
	// snapshot, idle, delta, idle, exited
	expected := []string{"snapshot", "idle", "delta", "idle", "exited"}
	if len(types) != len(expected) {
		t.Fatalf("event types = %v, want %v", types, expected)
	}
	for i, tp := range types {
		if tp != expected[i] {
			t.Errorf("types[%d] = %q, want %q", i, tp, expected[i])
		}
	}
}

func TestWatchNoDeltaWhenContentShrinks(t *testing.T) {
	var buf bytes.Buffer
	capture := fakeCapture("a\nb", "b")

	cfg := watchConfig{
		SessionName:   "test-session",
		Lines:         100,
		Interval:      1 * time.Millisecond,
		IdleThreshold: 1 * time.Hour,
	}

	code := runWatchLoopWith(context.Background(), &buf, cfg, tmux.Options{}, capture)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}

	events := parseEvents(t, buf.String())
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2: %v", len(events), events)
	}
	if events[0].Type != "snapshot" {
		t.Errorf("events[0].Type = %q, want snapshot", events[0].Type)
	}
	if events[1].Type != "exited" {
		t.Errorf("events[1].Type = %q, want exited", events[1].Type)
	}
}

func TestWatchExitsOnSessionGoneImmediately(t *testing.T) {
	var buf bytes.Buffer
	// Session gone from the start
	capture := fakeCapture()

	cfg := watchConfig{
		SessionName:   "test-session",
		Lines:         100,
		Interval:      1 * time.Millisecond,
		IdleThreshold: 5 * time.Second,
	}

	code := runWatchLoopWith(context.Background(), &buf, cfg, tmux.Options{}, capture)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}

	events := parseEvents(t, buf.String())
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Type != "exited" {
		t.Errorf("events[0].Type = %q, want exited", events[0].Type)
	}
}

func TestWatchInitialTransientCaptureMissDoesNotExit(t *testing.T) {
	origStateFor := tmuxSessionStateFor
	tmuxSessionStateFor = func(_ string, _ tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	defer func() { tmuxSessionStateFor = origStateFor }()

	var buf bytes.Buffer
	capture := fakeCaptureFunc(func(call int) (string, bool) {
		if call < 2 {
			return "", false
		}
		return "hello", true
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	cfg := watchConfig{
		SessionName:   "test-session",
		Lines:         100,
		Interval:      1 * time.Millisecond,
		IdleThreshold: 1 * time.Hour,
	}

	code := runWatchLoopWith(ctx, &buf, cfg, tmux.Options{}, capture)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}

	events := parseEvents(t, buf.String())
	if len(events) == 0 {
		t.Fatalf("expected at least one event")
	}
	if events[0].Type != "snapshot" {
		t.Fatalf("events[0].Type = %q, want snapshot", events[0].Type)
	}
}

func TestWatchTransientCaptureMissDuringLoopDoesNotEmitExited(t *testing.T) {
	origStateFor := tmuxSessionStateFor
	tmuxSessionStateFor = func(_ string, _ tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	defer func() { tmuxSessionStateFor = origStateFor }()

	var buf bytes.Buffer
	capture := fakeCaptureFunc(func(call int) (string, bool) {
		switch call {
		case 0:
			return "a", true
		case 1:
			return "", false
		default:
			return "a\nb", true
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	cfg := watchConfig{
		SessionName:   "test-session",
		Lines:         100,
		Interval:      1 * time.Millisecond,
		IdleThreshold: 1 * time.Hour,
	}

	code := runWatchLoopWith(ctx, &buf, cfg, tmux.Options{}, capture)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}

	events := parseEvents(t, buf.String())
	foundDelta := false
	for _, ev := range events {
		if ev.Type == "exited" {
			t.Fatalf("unexpected exited event after transient miss: %v", events)
		}
		if ev.Type == "delta" {
			foundDelta = true
		}
	}
	if !foundDelta {
		t.Fatalf("expected delta event after transient miss, got %v", events)
	}
}

func TestWatchExitsOnContextCancel(t *testing.T) {
	var buf bytes.Buffer

	// Infinite content — always returns the same thing
	capture := fakeCaptureFunc(func(_ int) (string, bool) {
		return "hello", true
	})

	ctx, cancel := context.WithCancel(context.Background())

	cfg := watchConfig{
		SessionName:   "test-session",
		Lines:         100,
		Interval:      1 * time.Millisecond,
		IdleThreshold: 1 * time.Hour,
	}

	done := make(chan int, 1)
	go func() {
		done <- runWatchLoopWith(ctx, &buf, cfg, tmux.Options{}, capture)
	}()

	// Let a few ticks happen, then cancel
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case code := <-done:
		if code != ExitOK {
			t.Fatalf("exit code = %d, want %d", code, ExitOK)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watch loop did not exit after context cancel")
	}

	// Should have at least the initial snapshot
	events := parseEvents(t, buf.String())
	if len(events) == 0 {
		t.Fatal("expected at least one event")
	}
	if events[0].Type != "snapshot" {
		t.Errorf("events[0].Type = %q, want snapshot", events[0].Type)
	}
}
