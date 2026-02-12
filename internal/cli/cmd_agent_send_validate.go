package cli

import (
	"fmt"
	"io"

	"github.com/andyrewlee/amux/internal/tmux"
)

func validateAgentSendSession(
	w io.Writer,
	wErr io.Writer,
	gf GlobalFlags,
	version string,
	idempotencyKey string,
	sessionName string,
	requestedJobID string,
	opts tmux.Options,
) int {
	state, err := tmuxSessionStateFor(sessionName, opts)
	if err != nil {
		markSendJobFailedIfPresent(requestedJobID, "session lookup failed: "+err.Error())
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.send", idempotencyKey,
				ExitInternalError, "session_lookup_failed", err.Error(), map[string]any{
					"session_name": sessionName,
				},
			)
		}
		Errorf(wErr, "failed to check session %s: %v", sessionName, err)
		return ExitInternalError
	}
	if !state.Exists {
		markSendJobFailedIfPresent(requestedJobID, "session not found")
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.send", idempotencyKey,
				ExitNotFound, "not_found", fmt.Sprintf("session %s not found", sessionName), nil,
			)
		}
		Errorf(wErr, "session %s not found", sessionName)
		return ExitNotFound
	}
	return ExitOK
}
