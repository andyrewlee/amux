package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestWaitForAgentResponse_ContentChangesAndStabilizes(t *testing.T) {
	calls := 0
	capture := func(_ string, _ int, _ tmux.Options) (string, bool) {
		calls++
		switch {
		case calls <= 2:
			return "prompt text", true // same as preHash
		case calls <= 4:
			return "prompt text\nagent typing...", true // changed
		default:
			return "prompt text\nagent done", true // stabilized
		}
	}

	pre := "prompt text"
	preHash := tmux.ContentHash(pre)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := waitForAgentResponse(ctx, waitResponseConfig{
		SessionName:   "test-session",
		CaptureLines:  100,
		PollInterval:  1 * time.Millisecond,
		IdleThreshold: 5 * time.Millisecond,
	}, tmux.Options{}, capture, preHash, pre)

	if result.TimedOut {
		t.Error("expected no timeout")
	}
	if result.SessionExited {
		t.Error("expected session not exited")
	}
	if result.Status != "idle" {
		t.Errorf("status = %q, want %q", result.Status, "idle")
	}
	if !result.Changed {
		t.Error("expected changed = true")
	}
	if result.Content != "prompt text\nagent done" {
		t.Errorf("content = %q, want %q", result.Content, "prompt text\nagent done")
	}
	if result.Delta != "agent done" {
		t.Errorf("delta = %q, want %q", result.Delta, "agent done")
	}
	if result.LatestLine != "agent done" {
		t.Errorf("latest_line = %q, want %q", result.LatestLine, "agent done")
	}
	if result.Summary != "agent done" {
		t.Errorf("summary = %q, want %q", result.Summary, "agent done")
	}
	if result.IdleSeconds <= 0 {
		t.Errorf("idle_seconds = %f, want > 0", result.IdleSeconds)
	}
}

func TestWaitForAgentResponse_ContentNeverChanges(t *testing.T) {
	capture := func(_ string, _ int, _ tmux.Options) (string, bool) {
		return "unchanged content", true
	}

	pre := "unchanged content"
	preHash := tmux.ContentHash(pre)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result := waitForAgentResponse(ctx, waitResponseConfig{
		SessionName:   "test-session",
		CaptureLines:  100,
		PollInterval:  1 * time.Millisecond,
		IdleThreshold: 10 * time.Second, // won't be reached
	}, tmux.Options{}, capture, preHash, pre)

	if !result.TimedOut {
		t.Error("expected timeout")
	}
	if result.SessionExited {
		t.Error("expected session not exited")
	}
	if result.Status != "timed_out" {
		t.Errorf("status = %q, want %q", result.Status, "timed_out")
	}
	if result.Changed {
		t.Error("expected changed = false")
	}
	if result.Content != pre {
		t.Errorf("content = %q, want %q", result.Content, pre)
	}
	if result.Delta != "" {
		t.Errorf("delta = %q, want empty", result.Delta)
	}
	if result.LatestLine != pre {
		t.Errorf("latest_line = %q, want %q", result.LatestLine, pre)
	}
	if result.Summary != pre {
		t.Errorf("summary = %q, want %q", result.Summary, pre)
	}
}

func TestWaitForAgentResponse_InitialChangeTimeout(t *testing.T) {
	origInitialTimeout := waitResponseInitialChangeTimeout
	waitResponseInitialChangeTimeout = 5 * time.Millisecond
	defer func() { waitResponseInitialChangeTimeout = origInitialTimeout }()

	capture := func(_ string, _ int, _ tmux.Options) (string, bool) {
		return "unchanged content", true
	}

	pre := "unchanged content"
	preHash := tmux.ContentHash(pre)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := waitForAgentResponse(ctx, waitResponseConfig{
		SessionName:   "test-session",
		CaptureLines:  100,
		PollInterval:  1 * time.Millisecond,
		IdleThreshold: 10 * time.Second,
	}, tmux.Options{}, capture, preHash, pre)

	if !result.TimedOut {
		t.Fatal("expected timed_out = true")
	}
	if result.Status != "timed_out" {
		t.Fatalf("status = %q, want %q", result.Status, "timed_out")
	}
	if result.Changed {
		t.Fatal("changed = true, want false")
	}
}

func TestWaitForAgentResponse_EmptyTimeoutUsesNoOutputLatestLine(t *testing.T) {
	capture := func(_ string, _ int, _ tmux.Options) (string, bool) {
		return "", true
	}

	pre := ""
	preHash := tmux.ContentHash(pre)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result := waitForAgentResponse(ctx, waitResponseConfig{
		SessionName:   "test-session",
		CaptureLines:  100,
		PollInterval:  1 * time.Millisecond,
		IdleThreshold: 10 * time.Second,
	}, tmux.Options{}, capture, preHash, pre)

	if !result.TimedOut {
		t.Fatal("expected timed_out = true")
	}
	if result.LatestLine != "(no output yet)" {
		t.Fatalf("latest_line = %q, want %q", result.LatestLine, "(no output yet)")
	}
	if result.Content != "" {
		t.Fatalf("content = %q, want empty", result.Content)
	}
	if result.Delta != "" {
		t.Fatalf("delta = %q, want empty", result.Delta)
	}
}

func TestWaitForAgentResponse_SessionExits(t *testing.T) {
	calls := 0
	capture := func(_ string, _ int, _ tmux.Options) (string, bool) {
		calls++
		if calls <= 2 {
			return "some output", true
		}
		return "", false // session gone
	}

	preHash := tmux.ContentHash("initial")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := waitForAgentResponse(ctx, waitResponseConfig{
		SessionName:   "test-session",
		CaptureLines:  100,
		PollInterval:  1 * time.Millisecond,
		IdleThreshold: 10 * time.Second,
	}, tmux.Options{}, capture, preHash, "initial")

	if result.TimedOut {
		t.Error("expected no timeout")
	}
	if !result.SessionExited {
		t.Error("expected session_exited = true")
	}
	if result.Status != "session_exited" {
		t.Errorf("status = %q, want %q", result.Status, "session_exited")
	}
	if !result.Changed {
		t.Error("expected changed = true")
	}
	if result.Content != "some output" {
		t.Errorf("content = %q, want %q", result.Content, "some output")
	}
	if result.Delta != "some output" {
		t.Errorf("delta = %q, want %q", result.Delta, "some output")
	}
	if result.LatestLine != "some output" {
		t.Errorf("latest_line = %q, want %q", result.LatestLine, "some output")
	}
}

func TestWaitForAgentResponse_SessionExitsImmediately_FallsBackToPreContent(t *testing.T) {
	// Session exits on the very first poll — lastContent is empty.
	// Should return preContent as fallback.
	capture := func(_ string, _ int, _ tmux.Options) (string, bool) {
		return "", false
	}

	pre := "screen content before send"
	preHash := tmux.ContentHash(pre)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := waitForAgentResponse(ctx, waitResponseConfig{
		SessionName:   "test-session",
		CaptureLines:  100,
		PollInterval:  1 * time.Millisecond,
		IdleThreshold: 10 * time.Second,
	}, tmux.Options{}, capture, preHash, pre)

	if !result.SessionExited {
		t.Error("expected session_exited = true")
	}
	if result.Status != "session_exited" {
		t.Errorf("status = %q, want %q", result.Status, "session_exited")
	}
	if result.Changed {
		t.Error("expected changed = false")
	}
	if result.Content != pre {
		t.Errorf("content = %q, want preContent %q", result.Content, pre)
	}
	if result.Delta != "" {
		t.Errorf("delta = %q, want empty", result.Delta)
	}
	if result.LatestLine != pre {
		t.Errorf("latest_line = %q, want %q", result.LatestLine, pre)
	}
}

func TestWaitForAgentResponse_TransientCaptureMissDoesNotMarkSessionExited(t *testing.T) {
	calls := 0
	capture := func(_ string, _ int, _ tmux.Options) (string, bool) {
		calls++
		switch calls {
		case 1:
			return "before send", true
		case 2:
			return "", false // transient capture miss
		default:
			return "before send\nagent reply", true
		}
	}

	pre := "before send"
	preHash := tmux.ContentHash(pre)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := waitForAgentResponse(ctx, waitResponseConfig{
		SessionName:   "test-session",
		CaptureLines:  100,
		PollInterval:  1 * time.Millisecond,
		IdleThreshold: 5 * time.Millisecond,
	}, tmux.Options{}, capture, preHash, pre)

	if result.Status != "idle" {
		t.Fatalf("status = %q, want %q", result.Status, "idle")
	}
	if result.SessionExited {
		t.Fatalf("session_exited = true, want false")
	}
	if result.Content != "before send\nagent reply" {
		t.Fatalf("content = %q, want %q", result.Content, "before send\nagent reply")
	}
	if result.Delta != "agent reply" {
		t.Fatalf("delta = %q, want %q", result.Delta, "agent reply")
	}
	if result.LatestLine != "agent reply" {
		t.Fatalf("latest_line = %q, want %q", result.LatestLine, "agent reply")
	}
}

func TestWaitForAgentResponse_CaptureMissesWhileSessionAliveDoNotExit(t *testing.T) {
	origStateFor := tmuxSessionStateFor
	tmuxSessionStateFor = func(_ string, _ tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	defer func() { tmuxSessionStateFor = origStateFor }()

	calls := 0
	capture := func(_ string, _ int, _ tmux.Options) (string, bool) {
		calls++
		switch {
		case calls == 1:
			return "before send", true
		case calls <= 6:
			return "", false // multiple misses, but tmux session still exists
		default:
			return "before send\nagent reply", true
		}
	}

	pre := "before send"
	preHash := tmux.ContentHash(pre)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := waitForAgentResponse(ctx, waitResponseConfig{
		SessionName:   "test-session",
		CaptureLines:  100,
		PollInterval:  1 * time.Millisecond,
		IdleThreshold: 5 * time.Millisecond,
	}, tmux.Options{}, capture, preHash, pre)

	if result.Status != "idle" {
		t.Fatalf("status = %q, want %q", result.Status, "idle")
	}
	if result.SessionExited {
		t.Fatalf("session_exited = true, want false")
	}
	if result.Content != "before send\nagent reply" {
		t.Fatalf("content = %q, want %q", result.Content, "before send\nagent reply")
	}
	if result.Delta != "agent reply" {
		t.Fatalf("delta = %q, want %q", result.Delta, "agent reply")
	}
}

func TestWaitForAgentResponse_CaptureMissesWithStateCheckErrorDoNotExit(t *testing.T) {
	origStateFor := tmuxSessionStateFor
	tmuxSessionStateFor = func(_ string, _ tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{}, errors.New("tmux timeout")
	}
	defer func() { tmuxSessionStateFor = origStateFor }()

	calls := 0
	capture := func(_ string, _ int, _ tmux.Options) (string, bool) {
		calls++
		switch {
		case calls == 1:
			return "before send", true
		case calls <= 6:
			return "", false
		default:
			return "before send\nagent reply", true
		}
	}

	pre := "before send"
	preHash := tmux.ContentHash(pre)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := waitForAgentResponse(ctx, waitResponseConfig{
		SessionName:   "test-session",
		CaptureLines:  100,
		PollInterval:  1 * time.Millisecond,
		IdleThreshold: 5 * time.Millisecond,
	}, tmux.Options{}, capture, preHash, pre)

	if result.Status != "idle" {
		t.Fatalf("status = %q, want %q", result.Status, "idle")
	}
	if result.SessionExited {
		t.Fatalf("session_exited = true, want false")
	}
	if result.Delta != "agent reply" {
		t.Fatalf("delta = %q, want %q", result.Delta, "agent reply")
	}
}

func TestWaitForAgentResponse_TransientMissingSessionChecksDoNotExit(t *testing.T) {
	origStateFor := tmuxSessionStateFor
	stateChecks := 0
	tmuxSessionStateFor = func(_ string, _ tmux.Options) (tmux.SessionState, error) {
		stateChecks++
		if stateChecks <= 2 {
			return tmux.SessionState{Exists: false, HasLivePane: false}, nil
		}
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	defer func() { tmuxSessionStateFor = origStateFor }()

	calls := 0
	capture := func(_ string, _ int, _ tmux.Options) (string, bool) {
		calls++
		switch {
		case calls == 1:
			return "before send", true
		case calls <= 10:
			return "", false
		default:
			return "before send\nagent reply", true
		}
	}

	pre := "before send"
	preHash := tmux.ContentHash(pre)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := waitForAgentResponse(ctx, waitResponseConfig{
		SessionName:   "test-session",
		CaptureLines:  100,
		PollInterval:  1 * time.Millisecond,
		IdleThreshold: 5 * time.Millisecond,
	}, tmux.Options{}, capture, preHash, pre)

	if result.Status != "idle" {
		t.Fatalf("status = %q, want %q", result.Status, "idle")
	}
	if result.SessionExited {
		t.Fatalf("session_exited = true, want false")
	}
	if result.Delta != "agent reply" {
		t.Fatalf("delta = %q, want %q", result.Delta, "agent reply")
	}
}
