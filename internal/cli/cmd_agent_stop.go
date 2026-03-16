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
		ctx := &cmdCtx{w: w, wErr: wErr, gf: gf, version: version, cmd: "agent.stop.all", idemKey: *idempotencyKey}
		if !*yes {
			if gf.JSON {
				ReturnError(w, "confirmation_required", "pass --yes to confirm stopping all agents", nil, version)
				return ExitUnsafeBlocked
			}
			Errorf(wErr, "pass --yes to confirm stopping all agents")
			return ExitUnsafeBlocked
		}
		if handled, code := ctx.maybeReplay(); handled {
			return code
		}
		svc, err := NewServices(version)
		if err != nil {
			return ctx.errResult(ExitInternalError, "init_failed", err.Error(), nil, fmt.Sprintf("failed to initialize: %v", err))
		}
		return stopAllAgents(
			ctx, svc, *graceful, *gracePeriod,
		)
	}
	if sessionName == "" && *agentID == "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	if sessionName != "" && *agentID != "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}

	ctx := &cmdCtx{w: w, wErr: wErr, gf: gf, version: version, cmd: "agent.stop", idemKey: *idempotencyKey}

	if handled, code := ctx.maybeReplay(); handled {
		return code
	}
	svc, err := NewServices(version)
	if err != nil {
		return ctx.errResult(ExitInternalError, "init_failed", err.Error(), nil, fmt.Sprintf("failed to initialize: %v", err))
	}
	if *agentID != "" {
		resolved, err := resolveSessionNameForAgentID(*agentID, svc.TmuxOpts)
		if err != nil {
			if errors.Is(err, errInvalidAgentID) {
				return ctx.errResult(ExitUsage, "invalid_agent_id", err.Error(), map[string]any{"agent_id": *agentID}, fmt.Sprintf("invalid --agent: %v", err))
			}
			if errors.Is(err, errAgentNotFound) {
				return ctx.errResult(ExitNotFound, "not_found", "agent not found", map[string]any{"agent_id": *agentID}, fmt.Sprintf("agent %s not found", *agentID))
			}
			return ctx.errResult(ExitInternalError, "stop_failed", err.Error(), map[string]any{"agent_id": *agentID}, fmt.Sprintf("failed to resolve --agent %s: %v", *agentID, err))
		}
		sessionName = resolved
	}

	state, err := tmuxSessionStateFor(sessionName, svc.TmuxOpts)
	if err != nil {
		return ctx.errResult(ExitInternalError, "stop_failed", err.Error(), nil, fmt.Sprintf("failed to check session: %v", err))
	}
	if !state.Exists {
		return ctx.errResult(ExitNotFound, "not_found", fmt.Sprintf("session %s not found", sessionName), nil, fmt.Sprintf("session %s not found", sessionName))
	}

	if err := stopAgentSession(sessionName, svc, *graceful, *gracePeriod); err != nil {
		return ctx.errResult(ExitInternalError, "stop_failed", err.Error(), nil, fmt.Sprintf("failed to stop session: %v", err))
	}

	removeTabFromStore(svc, sessionName)

	result := agentStopResult{Stopped: []string{sessionName}, AgentID: *agentID}

	if gf.JSON {
		return ctx.successResult(result)
	}

	PrintHuman(w, func(w io.Writer) {
		fmt.Fprintf(w, "Stopped %s\n", sessionName)
	})
	return ExitOK
}
