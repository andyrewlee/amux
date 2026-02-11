package cli

import (
	"errors"
	"io"

	"github.com/andyrewlee/amux/internal/tmux"
)

func resolveSessionForAgentSend(
	w io.Writer,
	wErr io.Writer,
	gf GlobalFlags,
	version string,
	idempotencyKey string,
	agentID string,
	opts tmux.Options,
) (string, int, bool) {
	resolved, err := resolveSessionNameForAgentID(agentID, opts)
	if err == nil {
		return resolved, 0, false
	}

	if errors.Is(err, errInvalidAgentID) {
		if gf.JSON {
			return "", returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.send", idempotencyKey,
				ExitUsage, "invalid_agent_id", err.Error(), map[string]any{"agent_id": agentID},
			), true
		}
		Errorf(wErr, "invalid --agent: %v", err)
		return "", ExitUsage, true
	}
	if errors.Is(err, errAgentNotFound) {
		if gf.JSON {
			return "", returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.send", idempotencyKey,
				ExitNotFound, "not_found", "agent not found", map[string]any{"agent_id": agentID},
			), true
		}
		Errorf(wErr, "agent %s not found", agentID)
		return "", ExitNotFound, true
	}
	if gf.JSON {
		return "", returnJSONErrorMaybeIdempotent(
			w, wErr, gf, version, "agent.send", idempotencyKey,
			ExitInternalError, "session_lookup_failed", err.Error(), map[string]any{"agent_id": agentID},
		), true
	}
	Errorf(wErr, "failed to resolve --agent %s: %v", agentID, err)
	return "", ExitInternalError, true
}
