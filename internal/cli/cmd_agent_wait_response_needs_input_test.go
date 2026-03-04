package cli

import (
	"context"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestWaitForAgentResponse_DoesNotReturnNeedsInputForGenericReplyWithText(t *testing.T) {
	calls := 0
	capture := func(_ string, _ int, _ tmux.Options) (string, bool) {
		calls++
		switch calls {
		case 1:
			return "before send", true
		case 2, 3:
			return "before send\nI'll reply with a patch summary once tests finish.", true
		default:
			return "before send\n• DONE", true
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
	if result.LatestLine != "• DONE" {
		t.Fatalf("latest_line = %q, want %q", result.LatestLine, "• DONE")
	}
}

func TestWaitForAgentResponse_ReturnsNeedsInputForReplyWithNumericChoices(t *testing.T) {
	calls := 0
	capture := func(_ string, _ int, _ tmux.Options) (string, bool) {
		calls++
		switch calls {
		case 1:
			return "before send", true
		default:
			return "before send\nReply with 1 to continue, 2 to cancel.", true
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
		IdleThreshold: 5 * time.Second,
	}, tmux.Options{}, capture, preHash, pre)

	if result.Status != "needs_input" {
		t.Fatalf("status = %q, want %q", result.Status, "needs_input")
	}
	if !result.NeedsInput {
		t.Fatalf("needs_input = false, want true")
	}
	if result.InputHint != "Reply with 1 to continue, 2 to cancel." {
		t.Fatalf("input_hint = %q", result.InputHint)
	}
	if calls > 4 {
		t.Fatalf("wait loop did not return early enough, capture calls = %d", calls)
	}
}

func TestWaitForAgentResponse_ReturnsNeedsInputForReplyWithNumericChoicesMidSentence(t *testing.T) {
	calls := 0
	capture := func(_ string, _ int, _ tmux.Options) (string, bool) {
		calls++
		switch calls {
		case 1:
			return "before send", true
		default:
			return "before send\nTo continue, reply with 1 to approve or 2 to cancel.", true
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
		IdleThreshold: 5 * time.Second,
	}, tmux.Options{}, capture, preHash, pre)

	if result.Status != "needs_input" {
		t.Fatalf("status = %q, want %q", result.Status, "needs_input")
	}
	if !result.NeedsInput {
		t.Fatalf("needs_input = false, want true")
	}
	if result.InputHint != "To continue, reply with 1 to approve or 2 to cancel." {
		t.Fatalf("input_hint = %q", result.InputHint)
	}
	if calls > 4 {
		t.Fatalf("wait loop did not return early enough, capture calls = %d", calls)
	}
}

func TestWaitForAgentResponse_ReturnsNeedsInputForReplyWithYesConfirmation(t *testing.T) {
	calls := 0
	capture := func(_ string, _ int, _ tmux.Options) (string, bool) {
		calls++
		switch calls {
		case 1:
			return "before send", true
		default:
			return "before send\nReply with yes to continue.", true
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
		IdleThreshold: 5 * time.Second,
	}, tmux.Options{}, capture, preHash, pre)

	if result.Status != "needs_input" {
		t.Fatalf("status = %q, want %q", result.Status, "needs_input")
	}
	if !result.NeedsInput {
		t.Fatalf("needs_input = false, want true")
	}
	if result.InputHint != "Reply with yes to continue." {
		t.Fatalf("input_hint = %q", result.InputHint)
	}
	if calls > 4 {
		t.Fatalf("wait loop did not return early enough, capture calls = %d", calls)
	}
}
