package cli

import (
	"fmt"
	"io"
	"log/slog"
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

func sendAgentRunPromptIfRequested(
	w, wErr io.Writer,
	gf GlobalFlags,
	version, idempotencyKey, sessionName, prompt string,
	tmuxOpts tmux.Options,
) int {
	if prompt == "" {
		return ExitOK
	}

	time.Sleep(200 * time.Millisecond)
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
