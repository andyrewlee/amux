package cli

import (
	"errors"
	"io"
	"strings"
	"time"
)

const agentSendCommandName = "agent.send"

func cmdAgentSend(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux agent send (<session_name>|--agent <agent_id>) --text <message> [--enter] [--async] [--wait] [--wait-timeout <duration>] [--idle-threshold <duration>] [--idempotency-key <key>] [--json]"
	fs := newFlagSet("agent send")
	agentID := fs.String("agent", "", "agent ID (workspace_id:tab_id)")
	text := fs.String("text", "", "text to send (required)")
	enter := fs.Bool("enter", false, "send Enter key after text")
	async := fs.Bool("async", false, "enqueue send and return immediately")
	wait := fs.Bool("wait", false, "wait for agent to respond and go idle")
	waitTimeout := fs.Duration("wait-timeout", 120*time.Second, "max time to wait for response")
	idleThreshold := fs.Duration("idle-threshold", 10*time.Second, "idle time before returning response")
	idempotencyKey := fs.String("idempotency-key", "", "idempotency key for safe retries")
	processJob := fs.Bool("process-job", false, "internal: process existing send job")
	jobIDFlag := fs.String("job-id", "", "internal: existing send job id")
	sessionName, err := parseSinglePositionalWithFlags(fs, args)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if *text == "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	if sessionName == "" && *agentID == "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	if sessionName != "" && *agentID != "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	if *wait && *async {
		return returnUsageError(w, wErr, gf, usage, version,
			errors.New("--wait and --async cannot be used together"),
		)
	}
	if *waitTimeout <= 0 {
		return returnUsageError(w, wErr, gf, usage, version,
			errors.New("--wait-timeout must be > 0"),
		)
	}
	if *idleThreshold <= 0 {
		return returnUsageError(w, wErr, gf, usage, version,
			errors.New("--idle-threshold must be > 0"),
		)
	}
	if handled, code := maybeReplayIdempotentResponse(
		w, wErr, gf, version, agentSendCommandName, *idempotencyKey,
	); handled {
		return code
	}

	svc, err := NewServices(version)
	if err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, agentSendCommandName, *idempotencyKey,
				ExitInternalError, "init_failed", err.Error(), nil,
			)
		}
		Errorf(wErr, "failed to initialize: %v", err)
		return ExitInternalError
	}
	if *agentID != "" {
		resolved, code, handled := resolveSessionForAgentSend(
			w, wErr, gf, version, *idempotencyKey, *agentID, svc.TmuxOpts,
		)
		if handled {
			return code
		}
		sessionName = resolved
	}

	jobStore, err := newSendJobStore()
	if err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, agentSendCommandName, *idempotencyKey,
				ExitInternalError, "job_store_failed", err.Error(), nil,
			)
		}
		Errorf(wErr, "failed to initialize send job store: %v", err)
		return ExitInternalError
	}

	job, resolvedSessionName, code := resolveSendJobForExecution(
		w,
		wErr,
		gf,
		version,
		*idempotencyKey,
		jobStore,
		strings.TrimSpace(*jobIDFlag),
		sessionName,
		*agentID,
	)
	if code != ExitOK {
		return code
	}
	sessionName = resolvedSessionName

	if code := validateAgentSendSession(
		w, wErr, gf, version, *idempotencyKey, sessionName, job.ID, svc.TmuxOpts,
	); code != ExitOK {
		return code
	}

	// Internal process-job retries always execute inline in the child process.
	if *async && !*processJob {
		return dispatchAsyncAgentSend(
			w,
			wErr,
			gf,
			version,
			*idempotencyKey,
			jobStore,
			sessionName,
			*agentID,
			*text,
			*enter,
			job,
		)
	}

	return executeAgentSendJob(
		w,
		wErr,
		gf,
		version,
		*idempotencyKey,
		jobStore,
		svc,
		sessionName,
		*agentID,
		*text,
		*enter,
		job,
		sendWaitConfig{
			Wait:          *wait,
			WaitTimeout:   *waitTimeout,
			IdleThreshold: *idleThreshold,
		},
	)
}
