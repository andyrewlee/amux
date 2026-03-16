package cli

import (
	"errors"

	"github.com/andyrewlee/amux/internal/tmux"
)

func resolveSessionForAgentSend(
	ctx *cmdCtx,
	agentID string,
	opts tmux.Options,
) (string, int, bool) {
	resolved, err := resolveSessionNameForAgentID(agentID, opts)
	if err == nil {
		return resolved, 0, false
	}

	if errors.Is(err, errInvalidAgentID) {
		return "", ctx.errResult(ExitUsage, "invalid_agent_id", err.Error(), map[string]any{"agent_id": agentID}), true
	}
	if errors.Is(err, errAgentNotFound) {
		return "", ctx.errResult(ExitNotFound, "not_found", "agent not found", map[string]any{"agent_id": agentID}), true
	}
	return "", ctx.errResult(ExitInternalError, "session_lookup_failed", err.Error(), map[string]any{"agent_id": agentID}), true
}
