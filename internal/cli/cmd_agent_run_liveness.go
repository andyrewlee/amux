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

// promptReadyTimeout is how long to wait for the agent TUI to render before
// sending the initial --prompt text.
const promptReadyTimeout = 15 * time.Second

// promptPollInterval is how often to check pane output for readiness.
const promptPollInterval = 200 * time.Millisecond

func sendAgentRunPromptIfRequested(
	w, wErr io.Writer,
	gf GlobalFlags,
	version, idempotencyKey, sessionName, prompt string,
	tmuxOpts tmux.Options,
) int {
	if prompt == "" {
		return ExitOK
	}

	// Wait for the agent TUI to render before sending. Agents like Codex can
	// take several seconds to initialize; a fixed short sleep causes the Enter
	// keystroke to arrive before the input handler is ready.
	waitForPaneOutput(sessionName, tmuxOpts)

	if err := tmuxSendKeys(sessionName, prompt, true, tmuxOpts); err != nil {
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
		Errorf(wErr, "failed to send initial prompt to %s: %v", sessionName, err)
		return ExitInternalError
	}
	return ExitOK
}

// waitForPaneOutput polls the tmux pane until it has visible output (the agent
// TUI has rendered), or until the timeout is reached. Best-effort: if the
// timeout expires we still proceed with sending the prompt.
func waitForPaneOutput(sessionName string, opts tmux.Options) {
	deadline := time.Now().Add(promptReadyTimeout)
	for time.Now().Before(deadline) {
		text, ok := tmux.CapturePaneTail(sessionName, 5, opts)
		if ok && strings.TrimSpace(text) != "" {
			// Agent has produced output; give it a brief moment to finish
			// rendering the input prompt after the initial frame.
			time.Sleep(promptPollInterval)
			return
		}
		time.Sleep(promptPollInterval)
	}
	slog.Debug("prompt readiness timeout reached, sending anyway", "session", sessionName)
}
