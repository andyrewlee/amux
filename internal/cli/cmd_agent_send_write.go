package cli

import (
	"fmt"
	"io"
	"strings"
)

func cmdAgentSend(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux agent send (<session_name>|--agent <agent_id>) --text <message> [--enter] [--async] [--idempotency-key <key>] [--json]"
	fs := newFlagSet("agent send")
	agentID := fs.String("agent", "", "agent ID (workspace_id:tab_id)")
	text := fs.String("text", "", "text to send (required)")
	enter := fs.Bool("enter", false, "send Enter key after text")
	async := fs.Bool("async", false, "enqueue send and return immediately")
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
	if handled, code := maybeReplayIdempotentResponse(
		w, wErr, gf, version, "agent.send", *idempotencyKey,
	); handled {
		return code
	}

	svc, err := NewServices(version)
	if err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.send", *idempotencyKey,
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
				w, wErr, gf, version, "agent.send", *idempotencyKey,
				ExitInternalError, "job_store_failed", err.Error(), nil,
			)
		}
		Errorf(wErr, "failed to initialize send job store: %v", err)
		return ExitInternalError
	}

	requestedJobID := strings.TrimSpace(*jobIDFlag)
	var job sendJob
	if requestedJobID != "" {
		existing, ok, getErr := jobStore.get(requestedJobID)
		if getErr != nil {
			if gf.JSON {
				return returnJSONErrorMaybeIdempotent(
					w, wErr, gf, version, "agent.send", *idempotencyKey,
					ExitInternalError, "job_status_failed", getErr.Error(), map[string]any{"job_id": requestedJobID},
				)
			}
			Errorf(wErr, "failed to load send job status: %v", getErr)
			return ExitInternalError
		}
		if !ok {
			if gf.JSON {
				return returnJSONErrorMaybeIdempotent(
					w, wErr, gf, version, "agent.send", *idempotencyKey,
					ExitNotFound, "not_found", "send job not found", map[string]any{"job_id": requestedJobID},
				)
			}
			Errorf(wErr, "send job %s not found", requestedJobID)
			return ExitNotFound
		}
		job = existing
		// For process-job retries, job metadata is the source of truth.
		sessionName = job.SessionName
		if sessionName == "" {
			_, _ = jobStore.setStatus(job.ID, sendJobFailed, "stored send job is missing session name")
			if gf.JSON {
				return returnJSONErrorMaybeIdempotent(
					w, wErr, gf, version, "agent.send", *idempotencyKey,
					ExitInternalError, "job_status_failed", "stored send job is missing session name", map[string]any{"job_id": job.ID},
				)
			}
			Errorf(wErr, "stored send job %s is missing session name", job.ID)
			return ExitInternalError
		}
	} else {
		job, err = jobStore.create(sessionName, *agentID)
		if err != nil {
			if gf.JSON {
				return returnJSONErrorMaybeIdempotent(
					w, wErr, gf, version, "agent.send", *idempotencyKey,
					ExitInternalError, "job_create_failed", err.Error(), nil,
				)
			}
			Errorf(wErr, "failed to create send job: %v", err)
			return ExitInternalError
		}
	}

	if code := validateAgentSendSession(
		w, wErr, gf, version, *idempotencyKey, sessionName, job.ID, svc.TmuxOpts,
	); code != ExitOK {
		return code
	}

	if *async && !*processJob {
		if err := startSendJobProcess(sendJobProcessArgs{
			SessionName: sessionName,
			AgentID:     *agentID,
			Text:        *text,
			Enter:       *enter,
			JobID:       job.ID,
		}); err != nil {
			_, _ = jobStore.setStatus(
				job.ID,
				sendJobFailed,
				"failed to start async send processor: "+err.Error(),
			)
			if gf.JSON {
				return returnJSONErrorMaybeIdempotent(
					w, wErr, gf, version, "agent.send", *idempotencyKey,
					ExitInternalError, "job_dispatch_failed", err.Error(), map[string]any{"job_id": job.ID},
				)
			}
			Errorf(wErr, "failed to start async send processor: %v", err)
			return ExitInternalError
		}

		result := agentSendResult{
			SessionName: sessionName,
			AgentID:     *agentID,
			JobID:       job.ID,
			Status:      string(sendJobPending),
			Sent:        false,
		}
		if gf.JSON {
			return returnJSONSuccessWithIdempotency(
				w, wErr, gf, version, "agent.send", *idempotencyKey, result,
			)
		}
		PrintHuman(w, func(w io.Writer) {
			fmt.Fprintf(w, "Queued text to %s (job: %s)\n", sessionName, job.ID)
		})
		return ExitOK
	}

	queueLock, err := waitForSessionQueueTurnForJob(jobStore, sessionName, job.ID)
	if err != nil {
		_, _ = jobStore.setStatus(job.ID, sendJobFailed, err.Error())
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.send", *idempotencyKey,
				ExitInternalError, "job_queue_failed", err.Error(), map[string]any{"job_id": job.ID},
			)
		}
		Errorf(wErr, "failed to join send queue: %v", err)
		return ExitInternalError
	}
	defer releaseSessionQueueTurn(queueLock)

	job, ok, err := jobStore.get(job.ID)
	if err != nil {
		_, _ = jobStore.setStatus(job.ID, sendJobFailed, err.Error())
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.send", *idempotencyKey,
				ExitInternalError, "job_status_failed", err.Error(), map[string]any{"job_id": job.ID},
			)
		}
		Errorf(wErr, "failed to load send job status: %v", err)
		return ExitInternalError
	}
	if !ok {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.send", *idempotencyKey,
				ExitInternalError, "job_not_found", "send job not found", map[string]any{"job_id": job.ID},
			)
		}
		Errorf(wErr, "send job %s not found", job.ID)
		return ExitInternalError
	}

	if job.Status == sendJobCanceled {
		return handleSendJobNotRunnable(
			w, wErr, gf, version, *idempotencyKey, sessionName, *agentID, job,
		)
	}

	job, err = jobStore.setStatus(job.ID, sendJobRunning, "")
	if err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.send", *idempotencyKey,
				ExitInternalError, "job_status_failed", err.Error(), map[string]any{"job_id": job.ID},
			)
		}
		Errorf(wErr, "failed to update send job status: %v", err)
		return ExitInternalError
	}
	if job.Status != sendJobRunning {
		return handleSendJobNotRunnable(
			w, wErr, gf, version, *idempotencyKey, sessionName, *agentID, job,
		)
	}

	if err := tmuxSendKeys(sessionName, *text, *enter, svc.TmuxOpts); err != nil {
		failedJob, setErr := jobStore.setStatus(job.ID, sendJobFailed, err.Error())
		if setErr != nil {
			failedJob = job
			failedJob.Status = sendJobFailed
		}
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.send", *idempotencyKey,
				ExitInternalError, "send_failed", err.Error(), map[string]any{
					"job_id":   failedJob.ID,
					"status":   string(failedJob.Status),
					"agent_id": *agentID,
				},
			)
		}
		Errorf(wErr, "failed to send keys: %v", err)
		return ExitInternalError
	}

	if completedJob, setErr := jobStore.setStatus(job.ID, sendJobCompleted, ""); setErr == nil {
		job = completedJob
	} else {
		if !gf.JSON {
			Errorf(wErr, "warning: sent text but failed to persist completion for job %s: %v", job.ID, setErr)
		}
		job.Status = sendJobCompleted
		job.Error = ""
	}

	result := agentSendResult{
		SessionName: sessionName,
		AgentID:     *agentID,
		JobID:       job.ID,
		Status:      string(job.Status),
		Error:       job.Error,
		Sent:        job.Status == sendJobCompleted,
	}

	if gf.JSON {
		return returnJSONSuccessWithIdempotency(
			w, wErr, gf, version, "agent.send", *idempotencyKey, result,
		)
	}
	PrintHuman(w, func(w io.Writer) {
		fmt.Fprintf(w, "Sent text to %s (job: %s)\n", sessionName, job.ID)
	})
	return ExitOK
}
