package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/tmux"
)

func resolveSendJobForExecution(
	ctx *cmdCtx,
	jobStore *sendJobStore,
	requestedJobID string,
	sessionName string,
	agentID string,
) (sendJob, string, int) {
	if requestedJobID != "" {
		existing, ok, getErr := jobStore.get(requestedJobID)
		if getErr != nil {
			return sendJob{}, sessionName, ctx.errResult(
				ExitInternalError, "job_status_failed", getErr.Error(), map[string]any{"job_id": requestedJobID},
				fmt.Sprintf("failed to load send job status: %v", getErr),
			)
		}
		if !ok {
			return sendJob{}, sessionName, ctx.errResult(
				ExitNotFound, "not_found", "send job not found", map[string]any{"job_id": requestedJobID},
				fmt.Sprintf("send job %s not found", requestedJobID),
			)
		}
		job := existing
		// For process-job retries, job metadata is the source of truth.
		sessionName = job.SessionName
		if sessionName == "" {
			_, _ = jobStore.setStatus(job.ID, sendJobFailed, "stored send job is missing session name")
			return sendJob{}, sessionName, ctx.errResult(
				ExitInternalError, "job_status_failed", "stored send job is missing session name", map[string]any{"job_id": job.ID},
				fmt.Sprintf("stored send job %s is missing session name", job.ID),
			)
		}
		return job, sessionName, ExitOK
	}

	job, err := jobStore.create(sessionName, agentID)
	if err != nil {
		return sendJob{}, sessionName, ctx.errResult(
			ExitInternalError, "job_create_failed", err.Error(), nil,
			fmt.Sprintf("failed to create send job: %v", err),
		)
	}
	return job, sessionName, ExitOK
}

func dispatchAsyncAgentSend(
	ctx *cmdCtx,
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
		return ctx.errResult(
			ExitInternalError, "job_dispatch_failed", err.Error(), map[string]any{"job_id": job.ID},
			fmt.Sprintf("failed to start async send processor: %v", err),
		)
	}

	result := agentSendResult{
		SessionName: sessionName,
		AgentID:     agentID,
		JobID:       job.ID,
		Status:      string(sendJobPending),
		Sent:        false,
		Delivered:   false,
	}
	if ctx.gf.JSON {
		return ctx.successResult(result)
	}
	PrintHuman(ctx.w, func(w io.Writer) {
		fmt.Fprintf(w, "Queued text to %s (job: %s)\n", sessionName, job.ID)
	})
	return ExitOK
}

// executeAgentSendJobCore performs the send and returns the result without
// writing output. Callers use this to optionally append --wait data before
// serializing the response.
func executeAgentSendJobCore(
	ctx *cmdCtx,
	jobStore *sendJobStore,
	svc *Services,
	sessionName string,
	agentID string,
	text string,
	enter bool,
	job sendJob,
	needWaitBaseline bool,
) (agentSendResult, string, int) {
	// Both direct sends and --process-job retries pass through the same
	// per-session queue path to preserve FIFO delivery semantics.
	queueLock, err := waitForSessionQueueTurnForJob(jobStore, sessionName, job.ID)
	if err != nil {
		_, _ = jobStore.setStatus(job.ID, sendJobFailed, err.Error())
		return agentSendResult{}, "", ctx.errResult(
			ExitInternalError, "job_queue_failed", err.Error(), map[string]any{"job_id": job.ID},
			fmt.Sprintf("failed to join send queue: %v", err),
		)
	}
	defer releaseSessionQueueTurn(queueLock)

	jobID := job.ID
	job, ok, err := jobStore.get(jobID)
	if err != nil {
		_, _ = jobStore.setStatus(jobID, sendJobFailed, err.Error())
		return agentSendResult{}, "", ctx.errResult(
			ExitInternalError, "job_status_failed", err.Error(), map[string]any{"job_id": jobID},
			fmt.Sprintf("failed to load send job status: %v", err),
		)
	}
	if !ok {
		return agentSendResult{}, "", ctx.errResult(
			ExitInternalError, "job_not_found", "send job not found", map[string]any{"job_id": jobID},
			fmt.Sprintf("send job %s not found", jobID),
		)
	}

	if job.Status == sendJobCanceled || job.Status == sendJobCompleted {
		return agentSendResult{
			SessionName: sessionName,
			AgentID:     agentID,
			JobID:       job.ID,
			Status:      string(job.Status),
			Sent:        job.Status == sendJobCompleted,
			Delivered:   false,
		}, "", ExitOK
	}

	job, err = jobStore.setStatus(job.ID, sendJobRunning, "")
	if err != nil {
		return agentSendResult{}, "", ctx.errResult(
			ExitInternalError, "job_status_failed", err.Error(), map[string]any{"job_id": job.ID},
			fmt.Sprintf("failed to update send job status: %v", err),
		)
	}
	if job.Status != sendJobRunning {
		if job.Status == sendJobCanceled || job.Status == sendJobCompleted {
			return agentSendResult{
				SessionName: sessionName,
				AgentID:     agentID,
				JobID:       job.ID,
				Status:      string(job.Status),
				Sent:        job.Status == sendJobCompleted,
				Delivered:   false,
			}, "", ExitOK
		}
		humanMessage := fmt.Sprintf("send job %s is %s and cannot be executed", job.ID, job.Status)
		if strings.TrimSpace(job.Error) != "" {
			humanMessage = fmt.Sprintf("send job %s is %s: %s", job.ID, job.Status, job.Error)
		}
		return agentSendResult{}, "", ctx.errResult(
			ExitInternalError, "job_status_conflict", "send job is not runnable", map[string]any{
				"job_id": job.ID,
				"status": string(job.Status),
				"error":  job.Error,
			},
			humanMessage,
		)
	}

	preContent := ""
	if needWaitBaseline {
		preContent = captureWaitBaselineWithRetry(sessionName, svc.TmuxOpts)
	}

	if err := tmuxSendKeys(sessionName, text, enter, svc.TmuxOpts); err != nil {
		failedJob, setErr := jobStore.setStatus(job.ID, sendJobFailed, err.Error())
		if setErr != nil {
			failedJob = job
			failedJob.Status = sendJobFailed
		}
		return agentSendResult{}, "", ctx.errResult(
			ExitInternalError, "send_failed", err.Error(), map[string]any{
				"job_id":   failedJob.ID,
				"status":   string(failedJob.Status),
				"agent_id": agentID,
			},
			fmt.Sprintf("failed to send keys: %v", err),
		)
	}

	if completedJob, setErr := jobStore.setStatus(job.ID, sendJobCompleted, ""); setErr == nil {
		job = completedJob
	} else {
		if !ctx.gf.JSON {
			Errorf(ctx.wErr, "warning: sent text but failed to persist completion for job %s: %v", job.ID, setErr)
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
		Delivered:   true,
	}
	return result, preContent, ExitOK
}

// sendWaitConfig holds --wait parameters for agent send.
type sendWaitConfig struct {
	Wait          bool
	WaitTimeout   time.Duration
	IdleThreshold time.Duration
}

func executeAgentSendJob(
	ctx *cmdCtx,
	jobStore *sendJobStore,
	svc *Services,
	sessionName string,
	agentID string,
	text string,
	enter bool,
	job sendJob,
	waitCfg sendWaitConfig,
) int {
	result, preContent, code := executeAgentSendJobCore(
		ctx,
		jobStore, svc, sessionName, agentID, text, enter, job, waitCfg.Wait,
	)
	if code != ExitOK {
		return code
	}

	if waitCfg.Wait && result.Delivered {
		resp := runSendWait(svc.TmuxOpts, sessionName, waitCfg, preContent)
		result.Response = &resp
	}

	if ctx.gf.JSON {
		return ctx.successResult(result)
	}
	PrintHuman(ctx.w, func(w io.Writer) {
		switch {
		case result.Status == string(sendJobCanceled):
			fmt.Fprintf(w, "Send job %s canceled before execution\n", result.JobID)
		case result.Status == string(sendJobCompleted) && !result.Delivered:
			fmt.Fprintf(w, "Send job %s already completed\n", result.JobID)
		case result.Delivered:
			fmt.Fprintf(w, "Sent text to %s (job: %s)\n", sessionName, result.JobID)
		default:
			if result.Error != "" {
				fmt.Fprintf(w, "Send job %s is %s: %s\n", result.JobID, result.Status, result.Error)
			} else {
				fmt.Fprintf(w, "Send job %s is %s and was not delivered\n", result.JobID, result.Status)
			}
		}
		if result.Response != nil {
			if result.Response.NeedsInput {
				if strings.TrimSpace(result.Response.InputHint) != "" {
					fmt.Fprintf(w, "Agent needs input: %s\n", strings.TrimSpace(result.Response.InputHint))
				} else {
					fmt.Fprintf(w, "Agent needs input\n")
				}
			} else if result.Response.TimedOut {
				fmt.Fprintf(w, "Timed out waiting for response\n")
			} else if result.Response.SessionExited {
				fmt.Fprintf(w, "Session exited while waiting\n")
			} else {
				fmt.Fprintf(w, "Agent idle after %.1fs\n", result.Response.IdleSeconds)
			}
		}
	})
	return ExitOK
}

func runSendWait(tmuxOpts tmux.Options, sessionName string, waitCfg sendWaitConfig, preContent string) waitResponseResult {
	preHash := tmux.ContentHash(preContent)

	ctx, cancel := contextWithSignal()
	defer cancel()
	ctx, timeoutCancel := context.WithTimeout(ctx, waitCfg.WaitTimeout)
	defer timeoutCancel()

	return waitForAgentResponse(ctx, waitResponseConfig{
		SessionName:   sessionName,
		CaptureLines:  100,
		PollInterval:  500 * time.Millisecond,
		IdleThreshold: waitCfg.IdleThreshold,
	}, tmuxOpts, tmuxCapturePaneTail, preHash, preContent)
}

func captureWaitBaselineWithRetry(sessionName string, opts tmux.Options) string {
	const (
		maxAttempts = 3
		retryDelay  = 75 * time.Millisecond
	)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		content, ok := tmuxCapturePaneTail(sessionName, 100, opts)
		if ok {
			return content
		}
		if attempt < maxAttempts {
			time.Sleep(retryDelay)
		}
	}
	logging.Warn("wait baseline capture unavailable for session %s after %d attempts; proceeding with empty baseline", sessionName, maxAttempts)
	return ""
}
