package cli

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestWaitForAgentResponse_PreHashPreventsEarlyIdle(t *testing.T) {
	// Content stays the same as preHash for a while, then changes, then stabilizes.
	calls := 0
	capture := func(_ string, _ int, _ tmux.Options) (string, bool) {
		calls++
		if calls <= 5 {
			return "same as before send", true // matches preHash
		}
		return "agent replied", true // new content
	}

	pre := "same as before send"
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
	if result.Status != "idle" {
		t.Errorf("status = %q, want %q", result.Status, "idle")
	}
	if !result.Changed {
		t.Error("expected changed = true")
	}
	if result.Content != "agent replied" {
		t.Errorf("content = %q, want %q", result.Content, "agent replied")
	}
	if result.Delta != "agent replied" {
		t.Errorf("delta = %q, want %q", result.Delta, "agent replied")
	}
	if result.LatestLine != "agent replied" {
		t.Errorf("latest_line = %q, want %q", result.LatestLine, "agent replied")
	}
	if result.IdleSeconds <= 0 {
		t.Errorf("idle_seconds = %f, want > 0", result.IdleSeconds)
	}
}

func TestWaitForAgentResponse_UsesLastNonEmptyContentOnEmptyRedraw(t *testing.T) {
	// Some TUIs briefly redraw to empty output before settling.
	// We should keep the last non-empty snapshot in the final response.
	calls := 0
	capture := func(_ string, _ int, _ tmux.Options) (string, bool) {
		calls++
		switch {
		case calls <= 2:
			return "before send", true
		case calls <= 4:
			return "before send\nagent reply line", true
		default:
			return "", true
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
	if result.Content != "before send\nagent reply line" {
		t.Fatalf("content = %q, want last non-empty snapshot", result.Content)
	}
	if result.Delta != "agent reply line" {
		t.Fatalf("delta = %q, want %q", result.Delta, "agent reply line")
	}
	if result.LatestLine != "agent reply line" {
		t.Fatalf("latest_line = %q, want %q", result.LatestLine, "agent reply line")
	}
}

func TestWaitForAgentResponse_UsesLastDifferentContentWhenPaneReturnsToBaseline(t *testing.T) {
	// Some TUIs render a reply briefly and then redraw to the original prompt.
	// Keep the last snapshot that differed from preContent.
	calls := 0
	capture := func(_ string, _ int, _ tmux.Options) (string, bool) {
		calls++
		switch {
		case calls <= 2:
			return "before send", true
		case calls <= 4:
			return "before send\nshort reply", true
		default:
			return "before send", true
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
	if result.Content != "before send\nshort reply" {
		t.Fatalf("content = %q, want changed snapshot", result.Content)
	}
	if result.Delta != "short reply" {
		t.Fatalf("delta = %q, want %q", result.Delta, "short reply")
	}
	if result.LatestLine != "short reply" {
		t.Fatalf("latest_line = %q, want %q", result.LatestLine, "short reply")
	}
}

func TestWaitForAgentResponse_IgnoresVolatileProgressForIdle(t *testing.T) {
	origInitialTimeout := waitResponseInitialChangeTimeout
	waitResponseInitialChangeTimeout = 5 * time.Second
	defer func() { waitResponseInitialChangeTimeout = origInitialTimeout }()

	calls := 0
	capture := func(_ string, _ int, _ tmux.Options) (string, bool) {
		calls++
		switch calls {
		case 1:
			return "before send", true
		case 2:
			return "before send\nPlan: update parser\nWorking (0s • esc to interrupt)", true
		default:
			return fmt.Sprintf(
				"before send\nPlan: update parser\nWorking (%ds • esc to interrupt)",
				calls,
			), true
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
	if !result.Changed {
		t.Fatalf("changed = false, want true")
	}
	if result.Delta != "Plan: update parser" {
		t.Fatalf("delta = %q, want %q", result.Delta, "Plan: update parser")
	}
	if result.LatestLine != "Plan: update parser" {
		t.Fatalf("latest_line = %q, want %q", result.LatestLine, "Plan: update parser")
	}
}

func TestWaitForAgentResponse_IgnoresBulletedVolatileProgressForIdle(t *testing.T) {
	origInitialTimeout := waitResponseInitialChangeTimeout
	waitResponseInitialChangeTimeout = 5 * time.Second
	defer func() { waitResponseInitialChangeTimeout = origInitialTimeout }()

	calls := 0
	capture := func(_ string, _ int, _ tmux.Options) (string, bool) {
		calls++
		switch calls {
		case 1:
			return "before send", true
		case 2:
			return "before send\nPlan: update parser\n• Working (0s • esc to interrupt)", true
		default:
			return fmt.Sprintf(
				"before send\nPlan: update parser\n• Working (%ds • esc to interrupt)",
				calls,
			), true
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
	if !result.Changed {
		t.Fatalf("changed = false, want true")
	}
	if result.Delta != "Plan: update parser" {
		t.Fatalf("delta = %q, want %q", result.Delta, "Plan: update parser")
	}
	if result.LatestLine != "Plan: update parser" {
		t.Fatalf("latest_line = %q, want %q", result.LatestLine, "Plan: update parser")
	}
}

func TestWaitForAgentResponse_StripsTUIChromeFromDelta(t *testing.T) {
	calls := 0
	capture := func(_ string, _ int, _ tmux.Options) (string, bool) {
		calls++
		switch {
		case calls <= 2:
			return "before send", true
		default:
			return "before send\n╭────╮\n│ >_ OpenAI Codex │\n› user prompt\napp' or visit https://chatgpt.com/codex\n• final answer\n? for shortcuts 99% context left", true
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

	if result.Delta != "• final answer" {
		t.Fatalf("delta = %q, want %q", result.Delta, "• final answer")
	}
	if result.LatestLine != "• final answer" {
		t.Fatalf("latest_line = %q, want %q", result.LatestLine, "• final answer")
	}
}

func TestWaitForAgentResponse_DetectsNeedsInput(t *testing.T) {
	calls := 0
	capture := func(_ string, _ int, _ tmux.Options) (string, bool) {
		calls++
		switch {
		case calls <= 2:
			return "before send", true
		default:
			return "before send\nDo you want me to proceed? (y/N)", true
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

	if !result.NeedsInput {
		t.Fatalf("needs_input = false, want true")
	}
	if result.InputHint != "Do you want me to proceed? (y/N)" {
		t.Fatalf("input_hint = %q, want %q", result.InputHint, "Do you want me to proceed? (y/N)")
	}
	if result.Delta != "Do you want me to proceed? (y/N)" {
		t.Fatalf("delta = %q, want %q", result.Delta, "Do you want me to proceed? (y/N)")
	}
	if result.LatestLine != "Do you want me to proceed? (y/N)" {
		t.Fatalf("latest_line = %q, want %q", result.LatestLine, "Do you want me to proceed? (y/N)")
	}
}

func TestWaitForAgentResponse_ReturnsNeedsInputBeforeIdle(t *testing.T) {
	calls := 0
	capture := func(_ string, _ int, _ tmux.Options) (string, bool) {
		calls++
		switch calls {
		case 1:
			return "before send", true
		default:
			frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼"}
			frame := frames[(calls-2)%len(frames)]
			return "before send\nThinking " + frame + "\n⏵⏵ bypass permissions on (shift+tab to cycle) · esc to interrupt", true
		}
	}

	pre := "before send"
	preHash := tmux.ContentHash(pre)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	result := waitForAgentResponse(ctx, waitResponseConfig{
		SessionName:   "test-session",
		CaptureLines:  100,
		PollInterval:  1 * time.Millisecond,
		IdleThreshold: 5 * time.Second,
	}, tmux.Options{}, capture, preHash, pre)

	if result.Status != "needs_input" {
		t.Fatalf("status = %q, want %q", result.Status, "needs_input")
	}
	if result.TimedOut {
		t.Fatalf("timed_out = true, want false")
	}
	if result.SessionExited {
		t.Fatalf("session_exited = true, want false")
	}
	if !result.Changed {
		t.Fatalf("changed = false, want true")
	}
	if !result.NeedsInput {
		t.Fatalf("needs_input = false, want true")
	}
	if result.InputHint != "Assistant is waiting for local permission-mode selection." {
		t.Fatalf("input_hint = %q", result.InputHint)
	}
	if result.Summary != "Needs input: Assistant is waiting for local permission-mode selection." {
		t.Fatalf("summary = %q", result.Summary)
	}
	if calls > 4 {
		t.Fatalf("wait loop did not return early enough, capture calls = %d", calls)
	}
}

func TestWaitForAgentResponse_DoesNotReturnNeedsInputForCodexInlinePrompt(t *testing.T) {
	calls := 0
	capture := func(_ string, _ int, _ tmux.Options) (string, bool) {
		calls++
		switch calls {
		case 1:
			return "before send", true
		case 2, 3:
			return "before send\nWorking (0s • esc to interrupt)\n› Improve documentation in @filename\n? for shortcuts                                             30% context left", true
		default:
			return "before send\n• READY", true
		}
	}

	pre := "before send"
	preHash := tmux.ContentHash(pre)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	result := waitForAgentResponse(ctx, waitResponseConfig{
		SessionName:   "test-session",
		CaptureLines:  120,
		PollInterval:  1 * time.Millisecond,
		IdleThreshold: 5 * time.Millisecond,
	}, tmux.Options{}, capture, preHash, pre)

	if result.Status != "idle" {
		t.Fatalf("status = %q, want %q", result.Status, "idle")
	}
	if result.NeedsInput {
		t.Fatalf("needs_input = true, want false")
	}
	if result.InputHint != "" {
		t.Fatalf("input_hint = %q, want empty", result.InputHint)
	}
	if result.LatestLine != "• READY" {
		t.Fatalf("latest_line = %q, want %q", result.LatestLine, "• READY")
	}
	if result.Delta != "• READY" {
		t.Fatalf("delta = %q, want %q", result.Delta, "• READY")
	}
}
