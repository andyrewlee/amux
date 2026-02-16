package cli

import (
	"fmt"
	"io"
)

func resolveSendJobForExecution(
	w io.Writer,
	wErr io.Writer,
	gf GlobalFlags,
	version string,
	idempotencyKey string,
	jobStore *sendJobStore,
	requestedJobID string,
	sessionName string,
	agentID string,
) (sendJob, string, int) {
	if requestedJobID != "" {
		existing, ok, getErr := jobStore.get(requestedJobID)
		if getErr != nil {
			if gf.JSON {
				return sendJob{}, sessionName, returnJSONErrorMaybeIdempotent(
					w, wErr, gf, version, agentSendCommandName, idempotencyKey,
					ExitInternalError, "job_status_failed", getErr.Error(), map[string]any{"job_id": requestedJobID},
				)
			}
			Errorf(wErr, "failed to load send job status: %v", getErr)
			return sendJob{}, sessionName, ExitInternalError
		}
		if !ok {
			if gf.JSON {
				return sendJob{}, sessionName, returnJSONErrorMaybeIdempotent(
					w, wErr, gf, version, agentSendCommandName, idempotencyKey,
					ExitNotFound, "not_found", "send job not found", map[string]any{"job_id": requestedJobID},
				)
			}
			Errorf(wErr, "send job %s not found", requestedJobID)
			return sendJob{}, sessionName, ExitNotFound
		}
		job := existing
		// For process-job retries, job metadata is the source of truth.
		sessionName = job.SessionName
		if sessionName == "" {
			_, _ = jobStore.setStatus(job.ID, sendJobFailed, "stored send job is missing session name")
			if gf.JSON {
				return sendJob{}, sessionName, returnJSONErrorMaybeIdempotent(
					w, wErr, gf, version, agentSendCommandName, idempotencyKey,
					ExitInternalError, "job_status_failed", "stored send job is missing session name", map[string]any{"job_id": job.ID},
				)
			}
			Errorf(wErr, "stored send job %s is missing session name", job.ID)
			return sendJob{}, sessionName, ExitInternalError
		}
		return job, sessionName, ExitOK
	}

	job, err := jobStore.create(sessionName, agentID)
	if err != nil {
		if gf.JSON {
			return sendJob{}, sessionName, returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, agentSendCommandName, idempotencyKey,
				ExitInternalError, "job_create_failed", err.Error(), nil,
			)
		}
		Errorf(wErr, "failed to create send job: %v", err)
		return sendJob{}, sessionName, ExitInternalError
	}
	return job, sessionName, ExitOK
}

func dispatchAsyncAgentSend(
	w io.Writer,
	wErr io.Writer,
	gf GlobalFlags,
	version string,
	idempotencyKey string,
	jobStore *sendJobStore,
	sessionName string,
	agentID string,
	text string,
	enter bool,
	job sendJob,
) int {
	if err := startSendJobProcess(sendJobProcessArgs{
		SessionName: sessionName,
		AgentID:     agentID,
		Text:        text,
		Enter:       enter,
		JobID:       job.ID,
	}); err != nil {
		_, _ = jobStore.setStatus(
			job.ID,
			sendJobFailed,
			"failed to start async send processor: "+err.Error(),
		)
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, agentSendCommandName, idempotencyKey,
				ExitInternalError, "job_dispatch_failed", err.Error(), map[string]any{"job_id": job.ID},
			)
		}
		Errorf(wErr, "failed to start async send processor: %v", err)
		return ExitInternalError
	}

	result := agentSendResult{
		SessionName: sessionName,
		AgentID:     agentID,
		JobID:       job.ID,
		Status:      string(sendJobPending),
		Sent:        false,
	}
	if gf.JSON {
		return returnJSONSuccessWithIdempotency(
			w, wErr, gf, version, agentSendCommandName, idempotencyKey, result,
		)
	}
	PrintHuman(w, func(w io.Writer) {
		fmt.Fprintf(w, "Queued text to %s (job: %s)\n", sessionName, job.ID)
	})
	return ExitOK
}

func executeAgentSendJob(
	w io.Writer,
	wErr io.Writer,
	gf GlobalFlags,
	version string,
	idempotencyKey string,
	jobStore *sendJobStore,
	svc *Services,
	sessionName string,
	agentID string,
	text string,
	enter bool,
	job sendJob,
) int {
	// Both direct sends and --process-job retries pass through the same
	// per-session queue path to preserve FIFO delivery semantics.
	queueLock, err := waitForSessionQueueTurnForJob(jobStore, sessionName, job.ID)
	if err != nil {
		_, _ = jobStore.setStatus(job.ID, sendJobFailed, err.Error())
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, agentSendCommandName, idempotencyKey,
				ExitInternalError, "job_queue_failed", err.Error(), map[string]any{"job_id": job.ID},
			)
		}
		Errorf(wErr, "failed to join send queue: %v", err)
		return ExitInternalError
	}
	defer releaseSessionQueueTurn(queueLock)

	jobID := job.ID
	job, ok, err := jobStore.get(jobID)
	if err != nil {
		_, _ = jobStore.setStatus(jobID, sendJobFailed, err.Error())
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, agentSendCommandName, idempotencyKey,
				ExitInternalError, "job_status_failed", err.Error(), map[string]any{"job_id": jobID},
			)
		}
		Errorf(wErr, "failed to load send job status: %v", err)
		return ExitInternalError
	}
	if !ok {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, agentSendCommandName, idempotencyKey,
				ExitInternalError, "job_not_found", "send job not found", map[string]any{"job_id": jobID},
			)
		}
		Errorf(wErr, "send job %s not found", jobID)
		return ExitInternalError
	}

	if job.Status == sendJobCanceled {
		return handleSendJobNotRunnable(
			w, wErr, gf, version, idempotencyKey, sessionName, agentID, job,
		)
	}

	job, err = jobStore.setStatus(job.ID, sendJobRunning, "")
	if err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, agentSendCommandName, idempotencyKey,
				ExitInternalError, "job_status_failed", err.Error(), map[string]any{"job_id": job.ID},
			)
		}
		Errorf(wErr, "failed to update send job status: %v", err)
		return ExitInternalError
	}
	if job.Status != sendJobRunning {
		return handleSendJobNotRunnable(
			w, wErr, gf, version, idempotencyKey, sessionName, agentID, job,
		)
	}

	if err := tmuxSendKeys(sessionName, text, enter, svc.TmuxOpts); err != nil {
		failedJob, setErr := jobStore.setStatus(job.ID, sendJobFailed, err.Error())
		if setErr != nil {
			failedJob = job
			failedJob.Status = sendJobFailed
		}
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, agentSendCommandName, idempotencyKey,
				ExitInternalError, "send_failed", err.Error(), map[string]any{
					"job_id":   failedJob.ID,
					"status":   string(failedJob.Status),
					"agent_id": agentID,
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
		AgentID:     agentID,
		JobID:       job.ID,
		Status:      string(job.Status),
		Error:       job.Error,
		Sent:        job.Status == sendJobCompleted,
	}

	if gf.JSON {
		return returnJSONSuccessWithIdempotency(
			w, wErr, gf, version, agentSendCommandName, idempotencyKey, result,
		)
	}
	PrintHuman(w, func(w io.Writer) {
		fmt.Fprintf(w, "Sent text to %s (job: %s)\n", sessionName, job.ID)
	})
	return ExitOK
}
