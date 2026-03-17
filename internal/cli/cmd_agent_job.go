package cli

import (
	"fmt"
	"io"
	"time"
)

func routeAgentJob(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	return routeSubcommand(w, wErr, gf, args, version, "agent job", []subcommand{
		{names: []string{"status"}, handler: cmdAgentJobStatus},
		{names: []string{"cancel"}, handler: cmdAgentJobCancel},
		{names: []string{"wait"}, handler: cmdAgentJobWait},
	})
}

func cmdAgentJobStatus(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux agent job status <job_id> [--json]"
	fs := newFlagSet("agent job status")
	jobID, err := parseSinglePositionalWithFlags(fs, args)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if jobID == "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}

	ctx := &cmdCtx{w: w, wErr: wErr, gf: gf, version: version, cmd: "agent.job.status"}

	store, err := newSendJobStore()
	if err != nil {
		return ctx.errResult(ExitInternalError, "job_store_failed", err.Error(), nil, fmt.Sprintf("failed to initialize send job store: %v", err))
	}

	job, ok, err := store.get(jobID)
	if err != nil {
		return ctx.errResult(ExitInternalError, "job_status_failed", err.Error(), map[string]any{"job_id": jobID}, fmt.Sprintf("failed to read job %s: %v", jobID, err))
	}
	if !ok {
		return ctx.errResult(ExitNotFound, "not_found", "job not found", map[string]any{"job_id": jobID}, fmt.Sprintf("job %s not found", jobID))
	}

	writeJobStatusResult(w, gf, version, job)
	return ExitOK
}

func cmdAgentJobCancel(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux agent job cancel <job_id> [--idempotency-key <key>] [--json]"
	fs := newFlagSet("agent job cancel")
	idempotencyKey := fs.String("idempotency-key", "", "idempotency key for safe retries")
	jobID, err := parseSinglePositionalWithFlags(fs, args)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if jobID == "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}

	ctx := &cmdCtx{w: w, wErr: wErr, gf: gf, version: version, cmd: "agent.job.cancel", idemKey: *idempotencyKey}

	if handled, code := ctx.maybeReplay(); handled {
		return code
	}

	store, err := newSendJobStore()
	if err != nil {
		return ctx.errResult(ExitInternalError, "job_store_failed", err.Error(), nil, fmt.Sprintf("failed to initialize send job store: %v", err))
	}

	job, ok, canceled, err := store.cancel(jobID)
	if err != nil {
		return ctx.errResult(ExitInternalError, "job_cancel_failed", err.Error(), map[string]any{"job_id": jobID}, fmt.Sprintf("failed to cancel job %s: %v", jobID, err))
	}
	if !ok {
		return ctx.errResult(ExitNotFound, "not_found", "job not found", map[string]any{"job_id": jobID}, fmt.Sprintf("job %s not found", jobID))
	}

	result := agentJobCancelResult{
		JobID:    job.ID,
		Status:   string(job.Status),
		Canceled: canceled,
	}
	if gf.JSON {
		return ctx.successResult(result)
	}

	PrintHuman(w, func(w io.Writer) {
		if canceled {
			fmt.Fprintf(w, "Canceled job %s\n", job.ID)
			return
		}
		fmt.Fprintf(w, "Job %s is %s; nothing canceled\n", job.ID, job.Status)
	})
	return ExitOK
}

func cmdAgentJobWait(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux agent job wait <job_id> [--timeout <dur>] [--interval <dur>] [--json]"
	fs := newFlagSet("agent job wait")
	timeout := fs.Duration("timeout", 30*time.Second, "max wait duration")
	interval := fs.Duration("interval", 200*time.Millisecond, "poll interval")
	jobID, err := parseSinglePositionalWithFlags(fs, args)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if jobID == "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	if *timeout <= 0 || *interval <= 0 {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}

	ctx := &cmdCtx{w: w, wErr: wErr, gf: gf, version: version, cmd: "agent.job.wait"}

	store, err := newSendJobStore()
	if err != nil {
		return ctx.errResult(ExitInternalError, "job_store_failed", err.Error(), nil, fmt.Sprintf("failed to initialize send job store: %v", err))
	}

	deadline := time.Now().Add(*timeout)
	for {
		job, ok, getErr := store.get(jobID)
		if getErr != nil {
			return ctx.errResult(ExitInternalError, "job_status_failed", getErr.Error(), map[string]any{"job_id": jobID}, fmt.Sprintf("failed to read job %s: %v", jobID, getErr))
		}
		if !ok {
			return ctx.errResult(ExitNotFound, "not_found", "job not found", map[string]any{"job_id": jobID}, fmt.Sprintf("job %s not found", jobID))
		}
		if isTerminalSendJobStatus(job.Status) {
			writeJobStatusResult(w, gf, version, job)
			if job.Status == sendJobFailed {
				return ExitInternalError
			}
			return ExitOK
		}

		if time.Now().After(deadline) {
			return ctx.errResult(
				ExitInternalError,
				"timeout",
				"timed out waiting for job completion",
				map[string]any{
					"job_id": job.ID,
					"status": string(job.Status),
				},
				fmt.Sprintf("timed out waiting for job %s completion (status: %s)", job.ID, job.Status),
			)
		}
		time.Sleep(*interval)
	}
}
