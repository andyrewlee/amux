package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// fakeCapture returns a capture function that cycles through the given
// content values on successive calls. After exhausting the list, it
// returns ("", false) to simulate session exit.
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

func TestCmdAgentWatchRejectsNonPositiveIdleThreshold(t *testing.T) {
	var w, wErr bytes.Buffer
	code := cmdAgentWatch(&w, &wErr, GlobalFlags{}, []string{"session-a", "--idle-threshold", "0s"}, "test-v1")
	if code != ExitUsage {
		t.Fatalf("cmdAgentWatch() code = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(wErr.String(), "--idle-threshold must be > 0") {
		t.Fatalf("expected validation message, got %q", wErr.String())
	}
}

func TestCmdAgentWatchRejectsNonPositiveIdleThresholdJSON(t *testing.T) {
	var w, wErr bytes.Buffer
	code := cmdAgentWatch(&w, &wErr, GlobalFlags{JSON: true}, []string{"session-a", "--idle-threshold", "-1s"}, "test-v1")
	if code != ExitUsage {
		t.Fatalf("cmdAgentWatch() code = %d, want %d", code, ExitUsage)
	}
	if wErr.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", wErr.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, w.String())
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "invalid_idle_threshold" {
		t.Fatalf("expected invalid_idle_threshold, got %#v", env.Error)
	}
}

func TestComputeNewLinesAppended(t *testing.T) {
	prev := []string{"a", "b", "c"}
	curr := []string{"a", "b", "c", "d", "e"}
	got := computeNewLines(prev, curr)
	want := []string{"d", "e"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestComputeNewLinesNoOverlap(t *testing.T) {
	prev := []string{"a", "b"}
	curr := []string{"x", "y", "z"}
	got := computeNewLines(prev, curr)
	// No overlap → return all current
	if len(got) != 3 {
		t.Fatalf("got %v, want all current lines", got)
	}
}

func TestComputeNewLinesShrunkNoAdditions(t *testing.T) {
	prev := []string{"a", "b"}
	curr := []string{"b"}
	got := computeNewLines(prev, curr)
	if len(got) != 0 {
		t.Fatalf("got %v, want 0 new lines", got)
	}
}

func TestComputeNewLinesShrunkTrailingBlankLine(t *testing.T) {
	prev := strings.Split("line1\n\n", "\n")
	curr := strings.Split("line1\n", "\n")
	got := computeNewLines(prev, curr)
	if len(got) != 0 {
		t.Fatalf("got %v, want 0 new lines", got)
	}
}

func TestComputeNewLinesEmptyPrevious(t *testing.T) {
	curr := []string{"a", "b"}
	got := computeNewLines(nil, curr)
	if len(got) != 2 {
		t.Fatalf("got %v, want %v", got, curr)
	}
}

func TestComputeNewLinesTrailingBlankLineAdded(t *testing.T) {
	prev := strings.Split("line1\n", "\n")
	curr := strings.Split("line1\n\n", "\n")
	got := computeNewLines(prev, curr)
	if len(got) != 1 || got[0] != "" {
		t.Fatalf("got %v, want [\"\"]", got)
	}
}
