package cli

import (
	"os"
	"strconv"
	"strings"
	"time"
)

func assistantDXParseDurationSeconds(raw string) (int, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, false
	}
	if seconds, err := time.ParseDuration(value); err == nil {
		return int(seconds.Seconds()), true
	}
	for _, suffix := range []string{"s", "m", "h"} {
		if strings.HasSuffix(value, suffix) {
			base := strings.TrimSuffix(value, suffix)
			switch suffix {
			case "s":
				return assistantDXAtoiOrZero(base), true
			case "m":
				return assistantDXAtoiOrZero(base) * 60, true
			case "h":
				return assistantDXAtoiOrZero(base) * 3600, true
			}
		}
	}
	return assistantDXAtoiOrZero(value), true
}

func assistantDXAtoiOrZero(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return value
}

func assistantDXTaskReachedTerminalState(taskData map[string]any) bool {
	if assistantDXFieldString(taskData, "status") == "needs_input" {
		return true
	}
	return assistantDXFieldString(taskData, "overall_status") != "in_progress"
}

func (runner assistantDXRunner) review(args []string) assistantDXPayload {
	workspace := ""
	assistant := "codex"
	prompt := assistantDXDefaultReviewPrompt
	waitTimeout := ""
	idleThreshold := ""
	startLockTTL := ""
	idempotencyKey := ""
	allowNewRun := false
	monitor := true
	monitorTimeout := nonEmpty(strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_DX_MONITOR_TIMEOUT")), "8m")
	pollInterval := nonEmpty(strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_DX_MONITOR_POLL_INTERVAL")), "15s")
	maxSteps := ""
	turnBudget := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--workspace", "--assistant", "--prompt", "--wait-timeout", "--idle-threshold", "--start-lock-ttl", "--idempotency-key", "--max-steps", "--turn-budget", "--monitor-timeout", "--poll-interval":
			if i+1 >= len(args) {
				return assistantDXErrorPayload("review", "missing value for "+args[i], "")
			}
			value := args[i+1]
			switch args[i] {
			case "--workspace":
				workspace = value
			case "--assistant":
				assistant = value
			case "--prompt":
				prompt = value
			case "--wait-timeout":
				waitTimeout = value
			case "--idle-threshold":
				idleThreshold = value
			case "--start-lock-ttl":
				startLockTTL = value
			case "--idempotency-key":
				idempotencyKey = value
			case "--max-steps":
				maxSteps = value
			case "--turn-budget":
				turnBudget = value
			case "--monitor-timeout":
				monitorTimeout = value
			case "--poll-interval":
				pollInterval = value
			}
			i++
		case "--allow-new-run":
			allowNewRun = true
		case "--no-monitor":
			monitor = false
		default:
			return assistantDXErrorPayload("review", "unknown flag: "+args[i], "")
		}
	}

	if strings.TrimSpace(workspace) == "" {
		return assistantDXErrorPayload("review", "missing required flag: --workspace", "")
	}

	commandArgs := []string{"task", "start", "--workspace", workspace, "--assistant", assistant, "--prompt", prompt}
	if strings.TrimSpace(waitTimeout) != "" {
		commandArgs = append(commandArgs, "--wait-timeout", waitTimeout)
	}
	if strings.TrimSpace(idleThreshold) != "" {
		commandArgs = append(commandArgs, "--idle-threshold", idleThreshold)
	}
	if strings.TrimSpace(startLockTTL) != "" {
		commandArgs = append(commandArgs, "--start-lock-ttl", startLockTTL)
	}
	if strings.TrimSpace(idempotencyKey) != "" {
		commandArgs = append(commandArgs, "--idempotency-key", idempotencyKey)
	}
	if allowNewRun {
		commandArgs = append(commandArgs, "--allow-new-run")
	}

	env, errPayload := runner.invokeOK("review", commandArgs...)
	if errPayload != nil {
		return *errPayload
	}
	taskData := assistantDXObject(env.Data)
	if monitor {
		timeoutSeconds, ok := assistantDXParseDurationSeconds(monitorTimeout)
		if !ok || timeoutSeconds <= 0 {
			timeoutSeconds = 480
		}
		pollSeconds, ok := assistantDXParseDurationSeconds(pollInterval)
		if !ok || pollSeconds <= 0 {
			pollSeconds = 15
		}
		started := time.Now()
		for !assistantDXTaskReachedTerminalState(taskData) && int(time.Since(started).Seconds()) < timeoutSeconds {
			time.Sleep(time.Duration(pollSeconds) * time.Second)
			statusEnv, statusErr := runner.invokeOK("review", "task", "status", "--workspace", workspace, "--assistant", assistant)
			if statusErr != nil {
				return *statusErr
			}
			taskData = assistantDXObject(statusEnv.Data)
		}
	}

	return runner.emitTaskResult("review", workspace, assistant, prompt, maxSteps, turnBudget, taskData)
}

func (runner assistantDXRunner) continueTask(args []string) assistantDXPayload {
	agent := ""
	workspace := ""
	assistant := "codex"
	text := ""
	enter := false
	waitTimeout := "60s"
	idleThreshold := "10s"
	maxSteps := ""
	turnBudget := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--agent", "--workspace", "--assistant", "--text", "--wait-timeout", "--idle-threshold", "--max-steps", "--turn-budget":
			if i+1 >= len(args) {
				return assistantDXErrorPayload("continue", "missing value for "+args[i], "")
			}
			value := args[i+1]
			switch args[i] {
			case "--agent":
				agent = value
			case "--workspace":
				workspace = value
			case "--assistant":
				assistant = value
			case "--text":
				text = value
			case "--wait-timeout":
				waitTimeout = value
			case "--idle-threshold":
				idleThreshold = value
			case "--max-steps":
				maxSteps = value
			case "--turn-budget":
				turnBudget = value
			}
			i++
		case "--enter":
			enter = true
		case "--auto-start":
			// Intentionally ignored: preserve explicit control only.
		default:
			return assistantDXErrorPayload("continue", "unknown flag: "+args[i], "")
		}
	}

	if strings.TrimSpace(agent) == "" {
		if strings.TrimSpace(workspace) == "" {
			return assistantDXErrorPayload("continue", "missing target: pass --agent or --workspace", "")
		}
		statusEnv, errPayload := runner.invokeOK("continue", "task", "status", "--workspace", workspace, "--assistant", assistant)
		if errPayload != nil {
			return *errPayload
		}
		agent = assistantDXFieldString(assistantDXObject(statusEnv.Data), "agent_id")
		if strings.TrimSpace(agent) == "" {
			return assistantDXErrorPayload("continue", "no active agent for workspace "+workspace, "")
		}
	}

	if strings.TrimSpace(maxSteps) == "" {
		maxSteps = "1"
	}
	if strings.TrimSpace(turnBudget) == "" {
		turnBudget = "90"
	}

	turnAny, code := runAssistantTurn(assistantTurnOptions{
		Mode:          assistantStepModeSend,
		WaitTimeout:   waitTimeout,
		IdleThreshold: idleThreshold,
		MaxSteps:      maxSteps,
		TurnBudget:    turnBudget,
		AgentID:       agent,
		Text:          text,
		Enter:         enter,
	})
	if code != ExitOK {
		if errPayload, ok := turnAny.(assistantTurnErrorPayload); ok {
			return assistantDXErrorPayload("continue", errPayload.Summary, errPayload.Error)
		}
		return assistantDXErrorPayload("continue", "assistant-turn failed", "")
	}

	turnPayload, ok := turnAny.(assistantTurnPayload)
	if !ok {
		return assistantDXErrorPayload("continue", "assistant-turn returned unexpected payload", "")
	}

	rawStatus := strings.TrimSpace(turnPayload.OverallStatus)
	if rawStatus == "" {
		rawStatus = strings.TrimSpace(turnPayload.Status)
	}
	status := "ok"
	switch rawStatus {
	case "needs_input":
		status = "needs_input"
	case "timed_out", "session_exited", "partial", "partial_budget", "attention":
		status = "attention"
	}

	summary := strings.TrimSpace(turnPayload.Summary)
	if summary == "" {
		summary = "Continue completed."
	}
	nextAction := strings.TrimSpace(turnPayload.NextAction)
	if nextAction == "" {
		nextAction = "Check status and continue with the next step."
	}

	suggested := strings.TrimSpace(turnPayload.SuggestedCommand)
	if suggested == "" && strings.TrimSpace(workspace) != "" {
		suggested = runner.buildTaskStatusCmd(workspace, assistant)
	}

	actions := make([]assistantDXQuickAction, 0, len(turnPayload.QuickActions))
	for _, action := range turnPayload.QuickActions {
		id := strings.TrimSpace(action.ID)
		if id == "" {
			id = strings.TrimSpace(action.CallbackData)
		}
		actions = append(actions, assistantDXNewAction(id, action.Label, action.Command, action.Style, action.Prompt))
	}
	if len(actions) == 0 && strings.TrimSpace(workspace) != "" {
		actions = append(actions, assistantDXNewAction("status", "Status", runner.buildTaskStatusCmd(workspace, assistant), "primary", "Check task status"))
	}

	data := map[string]any{
		"agent":       nonEmpty(strings.TrimSpace(turnPayload.AgentID), agent),
		"workspace":   nonEmpty(strings.TrimSpace(turnPayload.WorkspaceID), workspace),
		"assistant":   nonEmpty(strings.TrimSpace(turnPayload.Assistant), assistant),
		"max_steps":   maxSteps,
		"turn_budget": turnBudget,
		"turn":        turnPayload,
	}

	message := summary + "\nNext: " + nextAction
	return assistantDXBuildPayload(true, "continue", status, summary, nextAction, suggested, data, actions, message)
}
