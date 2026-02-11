package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

var (
	tmuxSessionStateFor = tmux.SessionStateFor
	tmuxKillSession     = tmux.KillSession
	tmuxSendKeys        = tmux.SendKeys
	tmuxSendInterrupt   = tmux.SendInterrupt
	tmuxSetSessionTag   = tmux.SetSessionTagValue
	tmuxStartSession    = tmuxNewSession
	startSendJobProcess = launchSendJobProcessor
	saveWorkspaceMeta   = func(store *data.WorkspaceStore, ws *data.Workspace) error {
		return store.Save(ws)
	}
)

func cmdAgentRun(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux agent run --workspace <id> [--assistant <name>] [--prompt <text>] [--idempotency-key <key>] [--json]"
	fs := newFlagSet("agent run")
	wsFlag := fs.String("workspace", "", "workspace ID (required)")
	assistant := fs.String("assistant", "", "assistant name (default: workspace assistant)")
	name := fs.String("name", "", "tab name")
	prompt := fs.String("prompt", "", "initial prompt to send")
	idempotencyKey := fs.String("idempotency-key", "", "idempotency key for safe retries")
	if err := fs.Parse(args); err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if fs.NArg() > 0 {
		return returnUsageError(
			w, wErr, gf, usage, version,
			fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " ")),
		)
	}
	if *wsFlag == "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	wsID, err := parseWorkspaceIDFlag(*wsFlag)
	if err != nil {
		return returnUsageError(
			w,
			wErr,
			gf,
			usage,
			version,
			err,
		)
	}
	if handled, code := maybeReplayIdempotentResponse(
		w, wErr, gf, version, "agent.run", *idempotencyKey,
	); handled {
		return code
	}

	svc, err := NewServices(version)
	if err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.run", *idempotencyKey,
				ExitInternalError, "init_failed", err.Error(), nil,
			)
		}
		Errorf(wErr, "failed to initialize: %v", err)
		return ExitInternalError
	}

	ws, err := svc.Store.Load(wsID)
	if err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.run", *idempotencyKey,
				ExitNotFound, "not_found", fmt.Sprintf("workspace %s not found", wsID), nil,
			)
		}
		Errorf(wErr, "workspace %s not found", wsID)
		return ExitNotFound
	}

	// Determine assistant
	agentAssistant := ws.Assistant
	if *assistant != "" {
		agentAssistant = *assistant
	}
	ac, ok := svc.Config.Assistants[agentAssistant]
	if !ok {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.run", *idempotencyKey,
				ExitUsage, "unknown_assistant", "unknown assistant: "+agentAssistant, nil,
			)
		}
		Errorf(wErr, "unknown assistant: %s", agentAssistant)
		return ExitUsage
	}

	// Generate tab ID and session name.
	tabID := newAgentTabID()
	sessionName := tmux.SessionName("amux", string(wsID), tabID)

	// Create detached tmux session
	createArgs := []string{
		"new-session", "-d", "-s", sessionName, "-c", ws.Root, ac.Command,
	}
	cmd, cancel := tmuxStartSession(svc.TmuxOpts, createArgs...)
	defer cancel()
	if err := cmd.Run(); err != nil {
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.run", *idempotencyKey,
				ExitInternalError, "session_failed", err.Error(), nil,
			)
		}
		Errorf(wErr, "failed to create tmux session: %v", err)
		return ExitInternalError
	}

	// Tag the session.
	tags := []struct {
		Key   string
		Value string
	}{
		{Key: "@amux", Value: "1"},
		{Key: "@amux_workspace", Value: string(wsID)},
		{Key: "@amux_tab", Value: tabID},
		{Key: "@amux_type", Value: "agent"},
		{Key: "@amux_assistant", Value: agentAssistant},
		{Key: "@amux_created_at", Value: strconv.FormatInt(time.Now().Unix(), 10)},
	}
	for _, tag := range tags {
		if err := tmuxSetSessionTag(sessionName, tag.Key, tag.Value, svc.TmuxOpts); err != nil {
			_ = tmuxKillSession(sessionName, svc.TmuxOpts)
			if gf.JSON {
				return returnJSONErrorMaybeIdempotent(
					w, wErr, gf, version, "agent.run", *idempotencyKey,
					ExitInternalError, "session_tag_failed", err.Error(), map[string]any{
						"session_name": sessionName,
						"tag":          tag.Key,
					},
				)
			}
			Errorf(wErr, "failed to tag session %s (%s): %v", sessionName, tag.Key, err)
			return ExitInternalError
		}
	}

	if code := verifyStartedAgentSession(
		w, wErr, gf, version, *idempotencyKey, sessionName, svc.TmuxOpts,
	); code != ExitOK {
		return code
	}

	if code := sendAgentRunPromptIfRequested(
		w, wErr, gf, version, *idempotencyKey, sessionName, *prompt, svc.TmuxOpts,
	); code != ExitOK {
		return code
	}

	// Update workspace metadata
	tabName := agentAssistant
	if *name != "" {
		tabName = *name
	}
	ws.OpenTabs = append(ws.OpenTabs, data.TabInfo{
		Assistant:   agentAssistant,
		Name:        tabName,
		SessionName: sessionName,
		Status:      "running",
		CreatedAt:   time.Now().Unix(),
	})
	if err := saveWorkspaceMeta(svc.Store, ws); err != nil {
		_ = tmuxKillSession(sessionName, svc.TmuxOpts)
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.run", *idempotencyKey,
				ExitInternalError, "metadata_save_failed", err.Error(), map[string]any{
					"workspace_id": string(wsID),
					"session_name": sessionName,
				},
			)
		}
		Errorf(wErr, "failed to persist workspace metadata: %v", err)
		return ExitInternalError
	}

	result := agentRunResult{
		SessionName: sessionName,
		AgentID:     formatAgentID(string(wsID), tabID),
		WorkspaceID: string(wsID),
		Assistant:   agentAssistant,
		TabID:       tabID,
	}

	if gf.JSON {
		return returnJSONSuccessWithIdempotency(
			w, wErr, gf, version, "agent.run", *idempotencyKey, result,
		)
	}

	PrintHuman(w, func(w io.Writer) {
		fmt.Fprintf(w, "Started agent %s (session: %s)\n", agentAssistant, sessionName)
	})
	return ExitOK
}

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

	// Validate session exists.
	state, err := tmuxSessionStateFor(sessionName, svc.TmuxOpts)
	if err != nil {
		requestedJobID := strings.TrimSpace(*jobIDFlag)
		markSendJobFailedIfPresent(requestedJobID, "session lookup failed: "+err.Error())
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.send", *idempotencyKey,
				ExitInternalError, "session_lookup_failed", err.Error(), map[string]any{
					"session_name": sessionName,
				},
			)
		}
		Errorf(wErr, "failed to check session %s: %v", sessionName, err)
		return ExitInternalError
	}
	if !state.Exists {
		requestedJobID := strings.TrimSpace(*jobIDFlag)
		markSendJobFailedIfPresent(requestedJobID, "session not found")
		if gf.JSON {
			return returnJSONErrorMaybeIdempotent(
				w, wErr, gf, version, "agent.send", *idempotencyKey,
				ExitNotFound, "not_found", fmt.Sprintf("session %s not found", sessionName), nil,
			)
		}
		Errorf(wErr, "session %s not found", sessionName)
		return ExitNotFound
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
		sessionName = job.SessionName
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
