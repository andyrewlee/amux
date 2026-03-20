package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestAssistantWaitForIdleLoop_ReturnsLastContentOnIdle(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	capture := fakeCapture("hello", "hello")

	code := runAssistantWaitForIdleLoop(context.Background(), &stdout, &stderr, assistantWaitForIdleConfig{
		SessionName:   "sess-1",
		Timeout:       time.Hour,
		TimeoutLabel:  "1h",
		IdleThreshold: time.Nanosecond,
		PollInterval:  time.Millisecond,
		Lines:         100,
	}, tmux.Options{}, capture)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}
	if got := stdout.String(); got != "hello\n" {
		t.Fatalf("stdout = %q, want %q", got, "hello\n")
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want empty", got)
	}
}

func TestAssistantWaitForIdleLoop_ReturnsLastContentWhenSessionDisappears(t *testing.T) {
	origStateFor := tmuxSessionStateFor
	tmuxSessionStateFor = func(_ string, _ tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: false}, nil
	}
	defer func() { tmuxSessionStateFor = origStateFor }()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	capture := fakeCapture("hello")

	code := runAssistantWaitForIdleLoop(context.Background(), &stdout, &stderr, assistantWaitForIdleConfig{
		SessionName:   "sess-2",
		Timeout:       time.Hour,
		TimeoutLabel:  "1h",
		IdleThreshold: time.Hour,
		PollInterval:  time.Millisecond,
		Lines:         100,
	}, tmux.Options{}, capture)
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}
	if got := stdout.String(); got != "hello\n" {
		t.Fatalf("stdout = %q, want %q", got, "hello\n")
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want empty", got)
	}
}

func TestAssistantWaitForIdleLoop_TimesOutAndReturnsLastContent(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	capture := fakeCaptureFunc(func(_ int) (string, bool) {
		return "hello", true
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	code := runAssistantWaitForIdleLoop(ctx, &stdout, &stderr, assistantWaitForIdleConfig{
		SessionName:   "sess-3",
		Timeout:       5 * time.Millisecond,
		TimeoutLabel:  "5ms",
		IdleThreshold: time.Hour,
		PollInterval:  10 * time.Millisecond,
		Lines:         100,
	}, tmux.Options{}, capture)
	if code != ExitInternalError {
		t.Fatalf("exit code = %d, want %d", code, ExitInternalError)
	}
	if got := stdout.String(); got != "hello\n" {
		t.Fatalf("stdout = %q, want %q", got, "hello\n")
	}
	if got := stderr.String(); !strings.Contains(got, "Timeout after 5ms waiting for idle") {
		t.Fatalf("stderr = %q, want timeout message", got)
	}
}
