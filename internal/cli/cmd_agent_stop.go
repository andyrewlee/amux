package cli

import (
	"errors"
	"fmt"
	"io"
	"time"
)

type agentStopResult struct {
	Stopped         []string `json:"stopped"`
	AgentID         string   `json:"agent_id,omitempty"`
	StoppedAgentIDs []string `json:"stopped_agent_ids,omitempty"`
}

func cmdAgentStop(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux agent stop (<session_name>|--agent <agent_id>) [--graceful] [--grace-period <dur>] [--idempotency-key <key>] [--json]\n       amux agent stop --all --yes [--graceful] [--grace-period <dur>] [--idempotency-key <key>] [--json]"
	fs := newFlagSet("agent stop")
	all := fs.Bool("all", false, "stop all agents")
	yes := fs.Bool("yes", false, "confirm (required for --all)")
	agentID := fs.String("agent", "", "agent ID (workspace_id:tab_id)")
	graceful := fs.Bool("graceful", true, "send Ctrl-C first and wait before force stop")
	gracePeriod := fs.Duration("grace-period", 1200*time.Millisecond, "wait time before force stop")
	idempotencyKey := fs.String("idempotency-key", "", "idempotency key for safe retries")
	sessionName, err := parseSinglePositionalWithFlags(fs, args)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if *gracePeriod < 0 {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	if *all && (sessionName != "" || *agentID != "") {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}

	if *all {
		if !*yes {
			if gf.JSON {
				// Do not pass idempotency key: storing this error would block
				// a subsequent retry that includes --yes.
				return returnJSONErrorMaybeIdempotent(
					w, wErr, gf, version, "agent.stop.all", "",
					ExitUnsafeBlocked, "confirmation_required", "pass --yes to confirm stopping all agents", nil,
				)
			}
			Errorf(wErr, "pass --yes to confirm stopping all agents")
			return ExitUnsafeBlocked
		}
		if handled, code := maybeReplayIdempotentResponse(
			w, wErr, gf, version, "agent.stop.all", *idempotencyKey,
		); handled {
			return code
		}
		svc, err := NewServices(version)
		if err != nil {
			if gf.JSON {
				return returnJSONErrorMaybeIdempotent(
					w, wErr, gf, version, "agent.stop.all", *idempotencyKey,
					ExitInternalError, "init_failed", err.Error(), nil,
				)
			}
			Errorf(wErr, "failed to initialize: %v", err)
			return ExitInternalError
		}
		return stopAllAgents(
			w, wErr, gf, svc, version, "agent.stop.all", *idempotencyKey, *graceful, *gracePeriod,
		)
	}
	if sessionName == "" && *agentID == "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	if sessionName != "" && *agentID != "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	if handled, code := maybeReplayIdempotentResponse(
		w, wErr, gf, version, "agent.stop", *idempotencyKey,
	); handled {
		return code
	}
	svc, err := NewServices(version)
	if err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.stop", *idempotencyKey,
				ExitInternalError, "init_failed", err.Error(), nil,
			)
		}
		Errorf(wErr, "failed to initialize: %v", err)
		return ExitInternalError
	}
	if *agentID != "" {
		resolved, err := resolveSessionNameForAgentID(*agentID, svc.TmuxOpts)
		if err != nil {
			if errors.Is(err, errInvalidAgentID) {
				if gf.JSON {
					return returnJSONErrorMaybeIdempotent(
						w, wErr, gf, version, "agent.stop", *idempotencyKey,
						ExitUsage, "invalid_agent_id", err.Error(), map[string]any{"agent_id": *agentID},
					)
				}
				Errorf(wErr, "invalid --agent: %v", err)
				return ExitUsage
			}
			if errors.Is(err, errAgentNotFound) {
				if gf.JSON {
					return returnJSONErrorMaybeIdempotent(
						w, wErr, gf, version, "agent.stop", *idempotencyKey,
						ExitNotFound, "not_found", "agent not found", map[string]any{"agent_id": *agentID},
					)
				}
				Errorf(wErr, "agent %s not found", *agentID)
				return ExitNotFound
			}
			if gf.JSON {
				return returnJSONErrorMaybeIdempotent(
					w, wErr, gf, version, "agent.stop", *idempotencyKey,
					ExitInternalError, "stop_failed", err.Error(), map[string]any{"agent_id": *agentID},
				)
			}
			Errorf(wErr, "failed to resolve --agent %s: %v", *agentID, err)
			return ExitInternalError
		}
		sessionName = resolved
	}

	state, err := tmuxSessionStateFor(sessionName, svc.TmuxOpts)
	if err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.stop", *idempotencyKey,
				ExitInternalError, "stop_failed", err.Error(), nil,
			)
		}
		Errorf(wErr, "failed to check session: %v", err)
		return ExitInternalError
	}
	if !state.Exists {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.stop", *idempotencyKey,
				ExitNotFound, "not_found", fmt.Sprintf("session %s not found", sessionName), nil,
			)
		}
		Errorf(wErr, "session %s not found", sessionName)
		return ExitNotFound
	}

	if err := stopAgentSession(sessionName, svc, *graceful, *gracePeriod); err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.stop", *idempotencyKey,
				ExitInternalError, "stop_failed", err.Error(), nil,
			)
		}
		Errorf(wErr, "failed to stop session: %v", err)
		return ExitInternalError
	}

	removeTabFromStore(svc, sessionName)

	result := agentStopResult{Stopped: []string{sessionName}, AgentID: *agentID}

	if gf.JSON {
		return returnJSONSuccessWithIdempotency(
			w, wErr, gf, version, "agent.stop", *idempotencyKey, result,
		)
	}

	PrintHuman(w, func(w io.Writer) {
		fmt.Fprintf(w, "Stopped %s\n", sessionName)
	})
	return ExitOK
}
