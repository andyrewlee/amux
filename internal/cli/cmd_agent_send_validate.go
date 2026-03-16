package cli

import (
	"fmt"

	"github.com/andyrewlee/amux/internal/tmux"
)

func validateAgentSendSession(
	ctx *cmdCtx,
	sessionName string,
	requestedJobID string,
	opts tmux.Options,
) int {
	state, err := tmuxSessionStateFor(sessionName, opts)
	if err != nil {
		markSendJobFailedIfPresent(requestedJobID, "session lookup failed: "+err.Error())
		return ctx.errResult(ExitInternalError, "session_lookup_failed", err.Error(), map[string]any{
			"session_name": sessionName,
		})
	}
	if !state.Exists {
		markSendJobFailedIfPresent(requestedJobID, "session not found")
		return ctx.errResult(ExitNotFound, "not_found", fmt.Sprintf("session %s not found", sessionName), nil)
	}
	return ExitOK
}
