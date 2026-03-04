package cli

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestPaneReadyForPrompt_CodexAndClaude(t *testing.T) {
	if !paneReadyForPrompt("loading\n› Improve documentation in @filename", "codex") {
		t.Fatalf("expected codex prompt marker to be detected")
	}
	if paneReadyForPrompt("loading\nmodel: gpt-5", "codex") {
		t.Fatalf("expected codex loading banner without prompt marker to be not ready")
	}
	if !paneReadyForPrompt("header\n❯ ", "claude") {
		t.Fatalf("expected claude prompt marker to be detected")
	}
}

func TestWaitForPaneOutput_CodexWaitsForPromptMarker(t *testing.T) {
	origCapture := tmuxCapturePaneTail
	origTimeout := promptReadyTimeout
	origPoll := promptPollInterval
	origStable := promptStableRounds
	defer func() {
		tmuxCapturePaneTail = origCapture
		promptReadyTimeout = origTimeout
		promptPollInterval = origPoll
		promptStableRounds = origStable
	}()

	promptReadyTimeout = 60 * time.Millisecond
	promptPollInterval = 1 * time.Millisecond
	promptStableRounds = 2

	calls := 0
	tmuxCapturePaneTail = func(_ string, _ int, _ tmux.Options) (string, bool) {
		calls++
		if calls <= 3 {
			return "model: loading", true
		}
		return "model: ready\n› Improve documentation in @filename", true
	}

	waitForPaneOutput("test-session", "codex", tmux.Options{})

	if calls < 4 {
		t.Fatalf("calls = %d, want >= 4 (must wait for codex prompt marker)", calls)
	}
}

func TestWaitForPaneOutput_NonCodexFallsBackToStableOutput(t *testing.T) {
	origCapture := tmuxCapturePaneTail
	origTimeout := promptReadyTimeout
	origPoll := promptPollInterval
	origStable := promptStableRounds
	defer func() {
		tmuxCapturePaneTail = origCapture
		promptReadyTimeout = origTimeout
		promptPollInterval = origPoll
		promptStableRounds = origStable
	}()

	promptReadyTimeout = 60 * time.Millisecond
	promptPollInterval = 1 * time.Millisecond
	promptStableRounds = 2

	calls := 0
	tmuxCapturePaneTail = func(_ string, _ int, _ tmux.Options) (string, bool) {
		calls++
		return "stable startup screen", true
	}

	waitForPaneOutput("test-session", "aider", tmux.Options{})

	if calls != 3 {
		t.Fatalf("calls = %d, want 3 for stable fallback", calls)
	}
}

func TestSendAgentRunPromptIfRequested_CodexRetriesWhenPromptNotDelivered_OptIn(t *testing.T) {
	t.Setenv("AMUX_AGENT_RUN_PROMPT_RETRY", "true")

	origCapture := tmuxCapturePaneTail
	origSend := tmuxSendKeys
	origTimeout := promptReadyTimeout
	origPoll := promptPollInterval
	origStable := promptStableRounds
	origDeliveryWait := promptDeliveryWait
	origDeliveryPoll := promptDeliveryPollInterval
	defer func() {
		tmuxCapturePaneTail = origCapture
		tmuxSendKeys = origSend
		promptReadyTimeout = origTimeout
		promptPollInterval = origPoll
		promptStableRounds = origStable
		promptDeliveryWait = origDeliveryWait
		promptDeliveryPollInterval = origDeliveryPoll
	}()

	promptReadyTimeout = 40 * time.Millisecond
	promptPollInterval = 1 * time.Millisecond
	promptStableRounds = 1
	promptDeliveryWait = 5 * time.Millisecond
	promptDeliveryPollInterval = 1 * time.Millisecond

	tmuxCapturePaneTail = func(_ string, _ int, _ tmux.Options) (string, bool) {
		// Never changes after send -> force one retry path.
		return "› Improve documentation in @filename", true
	}

	sendCalls := 0
	tmuxSendKeys = func(_, _ string, _ bool, _ tmux.Options) error {
		sendCalls++
		return nil
	}

	code := sendAgentRunPromptIfRequested(
		nil, nil,
		GlobalFlags{JSON: true},
		"test-v1",
		"",
		"session-a",
		"codex",
		"Reply with READY only.",
		tmux.Options{},
		nil,
	)
	if code != ExitOK {
		t.Fatalf("sendAgentRunPromptIfRequested() code = %d, want %d", code, ExitOK)
	}
	if sendCalls != 2 {
		t.Fatalf("tmuxSendKeys calls = %d, want 2", sendCalls)
	}
}

func TestSendAgentRunPromptIfRequested_CodexNoRetryWhenPromptAlreadyEchoed(t *testing.T) {
	t.Setenv("AMUX_AGENT_RUN_PROMPT_RETRY", "true")

	origCapture := tmuxCapturePaneTail
	origSend := tmuxSendKeys
	origTimeout := promptReadyTimeout
	origPoll := promptPollInterval
	origStable := promptStableRounds
	origDeliveryWait := promptDeliveryWait
	origDeliveryPoll := promptDeliveryPollInterval
	defer func() {
		tmuxCapturePaneTail = origCapture
		tmuxSendKeys = origSend
		promptReadyTimeout = origTimeout
		promptPollInterval = origPoll
		promptStableRounds = origStable
		promptDeliveryWait = origDeliveryWait
		promptDeliveryPollInterval = origDeliveryPoll
	}()

	prompt := "/review uncommitted changes"
	capture := "OpenAI Codex\n› /review uncommitted changes"

	promptReadyTimeout = 40 * time.Millisecond
	promptPollInterval = 1 * time.Millisecond
	promptStableRounds = 1
	promptDeliveryWait = 5 * time.Millisecond
	promptDeliveryPollInterval = 1 * time.Millisecond

	tmuxCapturePaneTail = func(_ string, _ int, _ tmux.Options) (string, bool) {
		// Keep capture unchanged to emulate quiet startup where hash-based
		// delivery checks alone would false-negative.
		return capture, true
	}

	sendCalls := 0
	tmuxSendKeys = func(_, _ string, _ bool, _ tmux.Options) error {
		sendCalls++
		return nil
	}

	code := sendAgentRunPromptIfRequested(
		nil, nil,
		GlobalFlags{JSON: true},
		"test-v1",
		"",
		"session-a",
		"codex",
		prompt,
		tmux.Options{},
		nil,
	)
	if code != ExitOK {
		t.Fatalf("sendAgentRunPromptIfRequested() code = %d, want %d", code, ExitOK)
	}
	if sendCalls != 1 {
		t.Fatalf("tmuxSendKeys calls = %d, want 1", sendCalls)
	}
}

func TestSendAgentRunPromptIfRequested_CodexDefaultDoesNotRetry(t *testing.T) {
	origCapture := tmuxCapturePaneTail
	origSend := tmuxSendKeys
	origTimeout := promptReadyTimeout
	origPoll := promptPollInterval
	origStable := promptStableRounds
	origDeliveryWait := promptDeliveryWait
	origDeliveryPoll := promptDeliveryPollInterval
	defer func() {
		tmuxCapturePaneTail = origCapture
		tmuxSendKeys = origSend
		promptReadyTimeout = origTimeout
		promptPollInterval = origPoll
		promptStableRounds = origStable
		promptDeliveryWait = origDeliveryWait
		promptDeliveryPollInterval = origDeliveryPoll
	}()

	promptReadyTimeout = 40 * time.Millisecond
	promptPollInterval = 1 * time.Millisecond
	promptStableRounds = 1
	promptDeliveryWait = 5 * time.Millisecond
	promptDeliveryPollInterval = 1 * time.Millisecond

	tmuxCapturePaneTail = func(_ string, _ int, _ tmux.Options) (string, bool) {
		return "› /review uncommitted changes", true
	}

	sendCalls := 0
	tmuxSendKeys = func(_, _ string, _ bool, _ tmux.Options) error {
		sendCalls++
		return nil
	}

	code := sendAgentRunPromptIfRequested(
		nil, nil,
		GlobalFlags{JSON: true},
		"test-v1",
		"",
		"session-a",
		"codex",
		"/review uncommitted changes",
		tmux.Options{},
		nil,
	)
	if code != ExitOK {
		t.Fatalf("sendAgentRunPromptIfRequested() code = %d, want %d", code, ExitOK)
	}
	if sendCalls != 1 {
		t.Fatalf("tmuxSendKeys calls = %d, want 1", sendCalls)
	}
}

func TestSendAgentRunPromptIfRequested_NonCodexDoesNotRetry(t *testing.T) {
	origCapture := tmuxCapturePaneTail
	origSend := tmuxSendKeys
	origTimeout := promptReadyTimeout
	origPoll := promptPollInterval
	origStable := promptStableRounds
	defer func() {
		tmuxCapturePaneTail = origCapture
		tmuxSendKeys = origSend
		promptReadyTimeout = origTimeout
		promptPollInterval = origPoll
		promptStableRounds = origStable
	}()

	promptReadyTimeout = 40 * time.Millisecond
	promptPollInterval = 1 * time.Millisecond
	promptStableRounds = 1

	tmuxCapturePaneTail = func(_ string, _ int, _ tmux.Options) (string, bool) {
		return "❯ ", true
	}

	sendCalls := 0
	tmuxSendKeys = func(_, _ string, _ bool, _ tmux.Options) error {
		sendCalls++
		return nil
	}

	code := sendAgentRunPromptIfRequested(
		nil, nil,
		GlobalFlags{JSON: true},
		"test-v1",
		"",
		"session-b",
		"claude",
		"Reply with READY only.",
		tmux.Options{},
		nil,
	)
	if code != ExitOK {
		t.Fatalf("sendAgentRunPromptIfRequested() code = %d, want %d", code, ExitOK)
	}
	if sendCalls != 1 {
		t.Fatalf("tmuxSendKeys calls = %d, want 1", sendCalls)
	}
}

func TestPromptLikelyAlreadyQueued(t *testing.T) {
	if !promptLikelyAlreadyQueued("OpenAI Codex\n› /review uncommitted changes", "/review uncommitted changes") {
		t.Fatalf("expected queued prompt match")
	}
	if promptLikelyAlreadyQueued("OpenAI Codex\n› ", "/review uncommitted changes") {
		t.Fatalf("did not expect queued prompt match without prompt text")
	}
}

func TestSendAgentRunPromptIfRequested_BeforeSendHookRunsBeforeSend(t *testing.T) {
	origCapture := tmuxCapturePaneTail
	origSend := tmuxSendKeys
	origTimeout := promptReadyTimeout
	origPoll := promptPollInterval
	origStable := promptStableRounds
	defer func() {
		tmuxCapturePaneTail = origCapture
		tmuxSendKeys = origSend
		promptReadyTimeout = origTimeout
		promptPollInterval = origPoll
		promptStableRounds = origStable
	}()

	promptReadyTimeout = 40 * time.Millisecond
	promptPollInterval = 1 * time.Millisecond
	promptStableRounds = 1

	tmuxCapturePaneTail = func(_ string, _ int, _ tmux.Options) (string, bool) {
		return "❯ ", true
	}

	hookCalled := false
	tmuxSendKeys = func(_, _ string, _ bool, _ tmux.Options) error {
		if !hookCalled {
			t.Fatalf("expected beforeSend hook to run before tmuxSendKeys")
		}
		return nil
	}

	code := sendAgentRunPromptIfRequested(
		nil, nil,
		GlobalFlags{JSON: true},
		"test-v1",
		"",
		"session-c",
		"claude",
		"Reply with READY only.",
		tmux.Options{},
		func() { hookCalled = true },
	)
	if code != ExitOK {
		t.Fatalf("sendAgentRunPromptIfRequested() code = %d, want %d", code, ExitOK)
	}
	if !hookCalled {
		t.Fatalf("expected beforeSend hook to be called")
	}
}
