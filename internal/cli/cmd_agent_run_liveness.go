package cli

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

func verifyStartedAgentSession(
	w, wErr io.Writer,
	gf GlobalFlags,
	version, idempotencyKey, sessionName string,
	tmuxOpts tmux.Options,
) int {
	state, err := tmuxSessionStateFor(sessionName, tmuxOpts)
	if err != nil {
		if killErr := tmuxKillSession(sessionName, tmuxOpts); killErr != nil {
			slog.Debug("best-effort session kill failed", "session", sessionName, "error", killErr)
		}
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.run", idempotencyKey,
				ExitInternalError, "session_lookup_failed", err.Error(), map[string]any{
					"session_name": sessionName,
				},
			)
		}
		Errorf(wErr, "failed to verify session %s: %v", sessionName, err)
		return ExitInternalError
	}
	if state.Exists && state.HasLivePane {
		return ExitOK
	}

	if err := tmuxKillSession(sessionName, tmuxOpts); err != nil {
		slog.Debug("best-effort session kill failed", "session", sessionName, "error", err)
	}
	msg := fmt.Sprintf("assistant session %s exited before startup completed", sessionName)
	if gf.JSON {
		return returnJSONErrorMaybeIdempotent(
			w, wErr, gf, version, "agent.run", idempotencyKey,
			ExitInternalError, "session_exited", msg, map[string]any{
				"session_name": sessionName,
			},
		)
	}
	Errorf(wErr, msg)
	return ExitInternalError
}

var (
	// promptReadyTimeout is how long to wait for the agent TUI to be ready
	// before sending the initial --prompt text.
	promptReadyTimeout = 30 * time.Second

	// promptPollInterval is how often to check pane output for readiness.
	promptPollInterval = 300 * time.Millisecond

	// promptStableRounds is how many consecutive polls must return identical
	// output before we consider the TUI fully loaded for non-Codex assistants.
	promptStableRounds = 3

	// promptDeliveryWait bounds how long we wait for visible pane changes after
	// sending the initial prompt before considering a single retry (Codex only).
	promptDeliveryWait = 2 * time.Second

	// promptDeliveryPollInterval is the poll cadence for prompt delivery checks.
	promptDeliveryPollInterval = 100 * time.Millisecond
)

func sendAgentRunPromptIfRequested(
	w, wErr io.Writer,
	gf GlobalFlags,
	version, idempotencyKey, sessionName, assistantName, prompt string,
	tmuxOpts tmux.Options,
	beforeSend func(),
) int {
	if prompt == "" {
		return ExitOK
	}

	// Wait for the agent TUI to render before sending. Agents like Codex can
	// take several seconds to initialize; a fixed short sleep causes the Enter
	// keystroke to arrive before the input handler is ready.
	waitForPaneOutput(sessionName, assistantName, tmuxOpts)
	if beforeSend != nil {
		beforeSend()
	}

	preSendContent, _ := tmuxCapturePaneTail(sessionName, 80, tmuxOpts)
	preSendHash := tmux.ContentHash(preSendContent)

	if err := tmuxSendKeys(sessionName, prompt, true, tmuxOpts); err != nil {
		return handlePromptSendError(w, wErr, gf, version, idempotencyKey, sessionName, tmuxOpts, err, "send")
	}

	// Codex startup can still occasionally drop the very first prompt even when
	// a cursor is visible. If pane output does not change after send, retry once.
	if strings.EqualFold(strings.TrimSpace(assistantName), "codex") &&
		!waitForPromptDelivery(sessionName, preSendHash, tmuxOpts) {
		waitForPaneOutput(sessionName, assistantName, tmuxOpts)
		if err := tmuxSendKeys(sessionName, prompt, true, tmuxOpts); err != nil {
			return handlePromptSendError(w, wErr, gf, version, idempotencyKey, sessionName, tmuxOpts, err, "retry")
		}
	}
	return ExitOK
}

func handlePromptSendError(
	w, wErr io.Writer,
	gf GlobalFlags,
	version, idempotencyKey, sessionName string,
	tmuxOpts tmux.Options,
	err error,
	action string,
) int {
	if killErr := tmuxKillSession(sessionName, tmuxOpts); killErr != nil {
		slog.Debug("best-effort session kill failed", "session", sessionName, "error", killErr)
	}
	if gf.JSON {
		return returnJSONErrorMaybeIdempotent(
			w, wErr, gf, version, "agent.run", idempotencyKey,
			ExitInternalError, "prompt_send_failed", err.Error(), map[string]any{
				"session_name": sessionName,
			},
		)
	}
	Errorf(wErr, "failed to %s initial prompt to %s: %v", action, sessionName, err)
	return ExitInternalError
}

// waitForPaneOutput polls the tmux pane until the output stabilizes (stops
// changing), meaning the agent TUI has fully loaded and is waiting for input.
// Agents like Codex render a banner immediately but then spend several seconds
// loading the model before the input prompt is ready. We need to wait through
// that entire startup, not just until the first frame appears.
func waitForPaneOutput(sessionName, assistantName string, opts tmux.Options) {
	deadline := time.Now().Add(promptReadyTimeout)
	var lastContent string
	stableCount := 0
	assistantName = strings.ToLower(strings.TrimSpace(assistantName))
	requirePromptMarker := assistantName == "codex"

	for time.Now().Before(deadline) {
		text, ok := tmuxCapturePaneTail(sessionName, 20, opts)
		if !ok {
			// Consecutive stability requires uninterrupted successful captures.
			stableCount = 0
			lastContent = ""
			time.Sleep(promptPollInterval)
			continue
		}
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			// Blank startup/redraw frames break the consecutive chain.
			stableCount = 0
			lastContent = ""
			time.Sleep(promptPollInterval)
			continue
		}
		// Use the raw text as a hash — if it hasn't changed, the TUI is stable.
		if trimmed == lastContent {
			stableCount++
		} else {
			lastContent = trimmed
			stableCount = 0
		}
		if paneReadyForPrompt(trimmed, assistantName) {
			if !requirePromptMarker || stableCount >= promptStableRounds {
				return
			}
			time.Sleep(promptPollInterval)
			continue
		}
		if stableCount >= promptStableRounds && !requirePromptMarker {
			return
		}
		time.Sleep(promptPollInterval)
	}
	slog.Debug(
		"prompt readiness timeout reached, sending anyway",
		"session", sessionName,
		"assistant", assistantName,
	)
}

func waitForPromptDelivery(sessionName string, baselineHash [16]byte, opts tmux.Options) bool {
	deadline := time.Now().Add(promptDeliveryWait)
	for time.Now().Before(deadline) {
		content, ok := tmuxCapturePaneTail(sessionName, 80, opts)
		if ok && tmux.ContentHash(content) != baselineHash {
			return true
		}
		time.Sleep(promptDeliveryPollInterval)
	}
	return false
}

func paneReadyForPrompt(content, assistantName string) bool {
	lines := strings.Split(content, "\n")
	switch assistantName {
	case "codex":
		return hasPromptLine(lines, "›")
	case "claude", "claude-cli":
		return hasPromptLine(lines, "❯")
	default:
		return hasPromptLine(lines, "❯") || hasPromptLine(lines, "›")
	}
}

func hasPromptLine(lines []string, marker string) bool {
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if line == marker || strings.HasPrefix(line, marker+" ") {
			return true
		}
	}
	return false
}
