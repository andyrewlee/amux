package cli

import (
	"fmt"
	"strings"
)

func (runner assistantDXRunner) buildTaskStartCmd(workspace, assistant, prompt, maxSteps, turnBudget string) string {
	command := fmt.Sprintf(
		"%s task start --workspace %s --assistant %s --prompt %s",
		assistantDXQuote(runner.selfScript),
		assistantDXQuote(workspace),
		assistantDXQuote(assistant),
		assistantDXQuote(prompt),
	)
	if strings.TrimSpace(maxSteps) != "" {
		command += " --max-steps " + assistantDXQuote(maxSteps)
	}
	if strings.TrimSpace(turnBudget) != "" {
		command += " --turn-budget " + assistantDXQuote(turnBudget)
	}
	return command
}

func (runner assistantDXRunner) buildTaskStatusCmd(workspace, assistant string) string {
	return fmt.Sprintf(
		"%s task status --workspace %s --assistant %s",
		assistantDXQuote(runner.selfScript),
		assistantDXQuote(workspace),
		assistantDXQuote(assistant),
	)
}

func (runner assistantDXRunner) buildTaskContinueCmd(workspace, assistant, text, maxSteps, turnBudget string) string {
	command := fmt.Sprintf(
		"%s continue --workspace %s --assistant %s",
		assistantDXQuote(runner.selfScript),
		assistantDXQuote(workspace),
		assistantDXQuote(assistant),
	)
	if strings.TrimSpace(text) != "" {
		command += " --text " + assistantDXQuote(text)
	}
	command += " --enter"
	if strings.TrimSpace(maxSteps) != "" {
		command += " --max-steps " + assistantDXQuote(maxSteps)
	}
	if strings.TrimSpace(turnBudget) != "" {
		command += " --turn-budget " + assistantDXQuote(turnBudget)
	}
	return command
}

func assistantDXMapTaskStatus(taskStatus, overall string) string {
	taskStatus = strings.TrimSpace(taskStatus)
	overall = strings.TrimSpace(overall)
	switch taskStatus {
	case "needs_input":
		return "needs_input"
	case "attention":
		return "attention"
	}
	switch overall {
	case "needs_input":
		return "needs_input"
	case "in_progress", "session_exited", "partial", "partial_budget", "timed_out":
		return "attention"
	default:
		return "ok"
	}
}

func (runner assistantDXRunner) buildTaskFollowups(
	workspace,
	assistant,
	taskStatus,
	overall,
	prompt,
	inputHint,
	agentID,
	maxSteps,
	turnBudget string,
) (string, []assistantDXQuickAction) {
	statusCmd := runner.buildTaskStatusCmd(workspace, assistant)
	suggested := statusCmd
	actions := []assistantDXQuickAction{
		assistantDXNewAction("status", "Status", statusCmd, "primary", "Check task status"),
	}

	startPrompt := strings.TrimSpace(prompt)
	if startPrompt == "" {
		startPrompt = "Continue from current state and report status plus next action."
	}

	switch {
	case taskStatus == "needs_input" || overall == "needs_input":
		followup := strings.TrimSpace(inputHint)
		if followup == "" {
			followup = "Reply with the exact option needed, then continue and report status plus blockers."
		}
		continueCmd := runner.buildTaskContinueCmd(workspace, assistant, followup, maxSteps, turnBudget)
		suggested = continueCmd
		actions = append(actions, assistantDXNewAction("continue", "Continue", continueCmd, "primary", "Send response and continue"))
	case overall == "in_progress":
		return suggested, actions
	case overall == "session_exited" || strings.TrimSpace(agentID) == "":
		startCmd := runner.buildTaskStartCmd(workspace, assistant, startPrompt, maxSteps, turnBudget)
		suggested = startCmd
		actions = append(actions, assistantDXNewAction("start", "Start", startCmd, "primary", "Start another bounded run"))
	default:
		continueCmd := runner.buildTaskContinueCmd(workspace, assistant, "Continue from current state and provide status plus next action.", maxSteps, turnBudget)
		suggested = continueCmd
		actions = append(actions, assistantDXNewAction("continue", "Continue", continueCmd, "primary", "Send a follow-up instruction"))
		startCmd := runner.buildTaskStartCmd(workspace, assistant, startPrompt, maxSteps, turnBudget)
		actions = append(actions, assistantDXNewAction("start", "Start", startCmd, "secondary", "Start another bounded run"))
	}

	return suggested, actions
}

func (runner assistantDXRunner) emitTaskResult(
	commandName,
	workspace,
	assistant,
	prompt,
	maxSteps,
	turnBudget string,
	taskData map[string]any,
) assistantDXPayload {
	taskStatus := assistantDXFieldString(taskData, "status")
	overall := assistantDXFieldString(taskData, "overall_status")
	status := assistantDXMapTaskStatus(taskStatus, overall)
	summary := assistantDXFieldString(taskData, "summary")
	if summary == "" {
		summary = "Task completed."
	}
	nextAction := assistantDXFieldString(taskData, "next_action")
	if nextAction == "" {
		nextAction = "Check status and continue if needed."
	}
	suggested, actions := runner.buildTaskFollowups(
		workspace,
		assistant,
		taskStatus,
		overall,
		prompt,
		assistantDXFieldString(taskData, "input_hint"),
		assistantDXFieldString(taskData, "agent_id"),
		maxSteps,
		turnBudget,
	)
	message := summary
	if strings.TrimSpace(nextAction) != "" {
		message += "\nNext: " + nextAction
	}
	return assistantDXBuildPayload(
		true,
		commandName,
		status,
		summary,
		nextAction,
		suggested,
		map[string]any{
			"workspace": workspace,
			"assistant": assistant,
			"prompt":    prompt,
			"task":      taskData,
		},
		actions,
		message,
	)
}

func (runner assistantDXRunner) taskStartLike(commandName string, args []string) assistantDXPayload {
	workspace := ""
	assistant := "codex"
	prompt := ""
	waitTimeout := ""
	idleThreshold := ""
	startLockTTL := ""
	idempotencyKey := ""
	allowNewRun := false
	maxSteps := ""
	turnBudget := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--workspace", "--assistant", "--prompt", "--wait-timeout", "--idle-threshold", "--start-lock-ttl", "--idempotency-key", "--max-steps", "--turn-budget":
			if i+1 >= len(args) {
				return assistantDXErrorPayload(commandName, "invalid flags for "+commandName, "")
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
			}
			i++
		case "--allow-new-run":
			allowNewRun = true
		default:
			return assistantDXErrorPayload(commandName, "invalid flags for "+commandName, "")
		}
	}

	if strings.TrimSpace(workspace) == "" || strings.TrimSpace(prompt) == "" {
		return assistantDXErrorPayload(commandName, "missing required flags: --workspace and --prompt", "")
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

	env, errPayload := runner.invokeOK(commandName, commandArgs...)
	if errPayload != nil {
		return *errPayload
	}
	return runner.emitTaskResult(commandName, workspace, assistant, prompt, maxSteps, turnBudget, assistantDXObject(env.Data))
}

func (runner assistantDXRunner) taskStatus(args []string) assistantDXPayload {
	workspace := ""
	assistant := "codex"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--workspace", "--assistant":
			if i+1 >= len(args) {
				return assistantDXErrorPayload("task.status", "missing value for "+args[i], "")
			}
			if args[i] == "--workspace" {
				workspace = args[i+1]
			} else {
				assistant = args[i+1]
			}
			i++
		default:
			return assistantDXErrorPayload("task.status", "unknown flag: "+args[i], "")
		}
	}
	if strings.TrimSpace(workspace) == "" {
		return assistantDXErrorPayload("task.status", "missing required flag: --workspace", "")
	}
	env, errPayload := runner.invokeOK("task.status", "task", "status", "--workspace", workspace, "--assistant", assistant)
	if errPayload != nil {
		return *errPayload
	}
	return runner.emitTaskResult("task.status", workspace, assistant, "", "", "", assistantDXObject(env.Data))
}
