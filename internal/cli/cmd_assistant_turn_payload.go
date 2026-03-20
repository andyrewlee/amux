package cli

import (
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func buildAssistantTurnPayload(opts assistantTurnOptions, runtime assistantTurnRuntime, state assistantTurnState) (assistantTurnPayload, error) {
	elapsed := int(time.Since(state.StartTime).Seconds())
	overallStatus := "partial"
	switch {
	case state.LastStatus == "idle" && state.LastSubstantiveOutput:
		overallStatus = "completed"
	case state.LastStatus == "needs_input" || state.LastNeedsInput:
		overallStatus = "needs_input"
	case state.LastStatus == "session_exited":
		overallStatus = "session_exited"
	case state.LastStatus == "timed_out":
		overallStatus = "timed_out"
	}
	if state.BudgetExhausted && overallStatus != "completed" {
		overallStatus = "partial_budget"
	}

	finalSummary := state.LastSummary
	if strings.TrimSpace(finalSummary) == "" {
		finalSummary = "Turn ended with status: " + nonEmpty(state.LastStatus, "unknown") + "."
	}
	if overallStatus == "completed" {
		finalSummary = "Completed in " + strconv.Itoa(state.StepsUsed) + " step(s). " + finalSummary
	} else {
		finalSummary = "Partial after " + strconv.Itoa(state.StepsUsed) + " step(s). " + finalSummary
	}

	lastNextAction := state.LastNextAction
	lastSuggestedCommand := state.LastSuggestedCommand

	if overallStatus == "completed" && strings.TrimSpace(state.LastWorkspaceID) != "" && assistantTurnSummaryClaimsWorkspaceEdits(state.LastSummary) {
		workspaceRoot := assistantTurnWorkspaceRootForTurn(runtime.AMUXBin, state.LastWorkspaceID)
		if workspaceRoot != "" {
			if clean, ok := assistantTurnWorkspaceIsClean(workspaceRoot); ok && clean {
				if committedSinceStart, commitCheckOK := assistantTurnWorkspaceHasCommitSince(workspaceRoot, state.StartTime); commitCheckOK && !committedSinceStart {
					overallStatus = "partial"
					finalSummary = "Partial after " + strconv.Itoa(state.StepsUsed) + " step(s). Claimed file updates, but no workspace changes were detected."
					lastNextAction = "Ask for exact changed files and apply the requested edits."
					if strings.TrimSpace(state.LastAgentID) != "" {
						lastSuggestedCommand = assistantTurnShellCommand(
							runtime.TurnScriptCmdRef,
							"send",
							"--agent", state.LastAgentID,
							"--text", "List exact files changed and apply the missing edits now.",
							"--enter",
							"--max-steps", "2",
							"--turn-budget", "180",
							"--wait-timeout", "60s",
							"--idle-threshold", "10s",
						)
					}
				}
			}
		}
	}

	finalSummary = assistantStepRedactSecrets(finalSummary)
	lastNextAction = assistantStepRedactSecrets(lastNextAction)
	lastSuggestedCommand = assistantStepRedactSecrets(lastSuggestedCommand)

	progressPercent := 0
	if runtime.MaxSteps > 0 {
		progressPercent = state.StepsUsed * 100 / runtime.MaxSteps
	}

	statusEmoji := assistantTurnStatusEmoji(overallStatus)
	message := assistantTurnChannelMessage(statusEmoji, finalSummary, lastNextAction, lastSuggestedCommand, runtime.Verbosity, state.StepsUsed, runtime.MaxSteps, progressPercent, elapsed, runtime.TurnBudgetSeconds, state.TimeoutStreak, runtime.TimeoutStreakLimit)
	chunks, chunksMeta := assistantStepChunkMessage(message, runtime.ChunkChars)

	reviewContext := strings.ToLower(strings.Join([]string{lastNextAction, lastSuggestedCommand}, "\n"))
	fullContext := strings.ToLower(strings.Join([]string{finalSummary, lastNextAction, lastSuggestedCommand}, "\n"))
	testCmd := ""
	lintCmd := ""
	securityCmd := ""
	reviewCmd := ""
	statusCmd := ""
	if strings.TrimSpace(state.LastAgentID) != "" {
		if strings.Contains(fullContext, "test") && (strings.Contains(fullContext, "fail") || strings.Contains(fullContext, "panic") || strings.Contains(fullContext, "error")) {
			testCmd = assistantTurnShellCommand(runtime.TurnScriptCmdRef, "send", "--agent", state.LastAgentID, "--text", "Investigate failing tests, fix root causes, and report changed files plus exact test command/results.", "--enter", "--max-steps", "2", "--turn-budget", "180", "--wait-timeout", "60s", "--idle-threshold", "10s")
		}
		if strings.Contains(fullContext, "lint") || strings.Contains(fullContext, "format") || strings.Contains(fullContext, "gofumpt") || strings.Contains(fullContext, "style") {
			lintCmd = assistantTurnShellCommand(runtime.TurnScriptCmdRef, "send", "--agent", state.LastAgentID, "--text", "Resolve lint and formatting issues, then provide a concise summary of fixes.", "--enter", "--max-steps", "2", "--turn-budget", "180", "--wait-timeout", "60s", "--idle-threshold", "10s")
		}
		if strings.Contains(fullContext, "secret") || strings.Contains(fullContext, "token") || strings.Contains(fullContext, "credential") || strings.Contains(fullContext, "key leak") {
			securityCmd = assistantTurnShellCommand(runtime.TurnScriptCmdRef, "send", "--agent", state.LastAgentID, "--text", "Run a focused security pass for exposed credentials/secrets and propose concrete remediation.", "--enter", "--max-steps", "2", "--turn-budget", "180", "--wait-timeout", "60s", "--idle-threshold", "10s")
		}
		if overallStatus == "completed" && (assistantTurnSummaryClaimsWorkspaceEdits(state.LastSummary) || assistantTurnSummaryDescribesWorkspaceEdits(state.LastSummary) || assistantTurnContextHasReviewableFileChangePhrase(reviewContext)) {
			reviewCmd = assistantTurnShellCommand(runtime.TurnScriptCmdRef, "send", "--agent", state.LastAgentID, "--text", "Summarize changed files, rationale, and remaining risks in 5 bullets.", "--enter", "--max-steps", "2", "--turn-budget", "180", "--wait-timeout", "60s", "--idle-threshold", "10s")
		}
		statusCmd = assistantTurnShellCommand(runtime.StepScriptCmdRef, "send", "--agent", state.LastAgentID, "--text", "Provide a one-line progress status.", "--enter", "--wait-timeout", "60s", "--idle-threshold", "10s")
	}

	quickActions := make([]assistantStepQuickAction, 0, 6)
	assistantStepAppendQuickAction(&quickActions, "fix_tests", "Fix Tests", testCmd, "success", "Investigate and fix failing tests")
	assistantStepAppendQuickAction(&quickActions, "fix_lint", "Fix Lint", lintCmd, "success", "Resolve lint and formatting issues")
	assistantStepAppendQuickAction(&quickActions, "security", "Security", securityCmd, "danger", "Run a focused security remediation pass")
	assistantStepAppendQuickAction(&quickActions, "review", "Review", reviewCmd, "primary", "Review and summarize recent code changes")
	assistantStepAppendQuickAction(&quickActions, "continue", "Continue", lastSuggestedCommand, "primary", "Continue from current state")
	assistantStepAppendQuickAction(&quickActions, "status", "Status", statusCmd, "primary", "Request a one-line status update")

	quickActionMap := make(map[string]string, len(quickActions))
	quickActionPrompts := make(map[string]string, len(quickActions))
	actionTokens := make([]string, 0, len(quickActions))
	for _, action := range quickActions {
		quickActionMap[action.CallbackData] = action.Command
		quickActionPrompts[action.CallbackData] = action.Prompt
		actionTokens = append(actionTokens, action.CallbackData)
	}

	inlineButtons := [][]assistantStepInlineButton{}
	if runtime.InlineButtonsEnabled {
		inlineButtons = assistantStepInlineButtons(quickActions, 2)
	}

	progressUpdates := assistantTurnProgressUpdates(state.Milestones, overallStatus, runtime.MaxSteps, assistantTurnDelivery(runtime, overallStatus, state.LastAgentID, state.LastWorkspaceID, state.TurnID))
	channelProgress := assistantTurnChannelProgressUpdates(state.Milestones, runtime.MaxSteps)
	delivery := assistantTurnDelivery(runtime, overallStatus, state.LastAgentID, state.LastWorkspaceID, state.TurnID)

	return assistantTurnPayload{
		OK:                 true,
		Mode:               opts.Mode,
		TurnID:             state.TurnID,
		Status:             nonEmpty(state.LastStatus, "unknown"),
		OverallStatus:      overallStatus,
		StatusEmoji:        statusEmoji,
		Verbosity:          runtime.Verbosity,
		Summary:            finalSummary,
		AgentID:            state.LastAgentID,
		WorkspaceID:        state.LastWorkspaceID,
		Assistant:          state.LastAssistantOut,
		StepsUsed:          state.StepsUsed,
		MaxSteps:           runtime.MaxSteps,
		ElapsedSeconds:     elapsed,
		TurnBudgetSeconds:  runtime.TurnBudgetSeconds,
		BudgetExhausted:    state.BudgetExhausted,
		ProgressPercent:    progressPercent,
		TimeoutStreak:      state.TimeoutStreak,
		TimeoutStreakLimit: runtime.TimeoutStreakLimit,
		NextAction:         lastNextAction,
		SuggestedCommand:   lastSuggestedCommand,
		Delivery:           delivery,
		Events:             state.Events,
		Milestones:         state.Milestones,
		ProgressUpdates:    progressUpdates,
		QuickActions:       quickActions,
		QuickActionMap:     quickActionMap,
		QuickActionPrompts: quickActionPrompts,
		Channel: assistantTurnChannelPayload{
			Message:              message,
			Verbosity:            runtime.Verbosity,
			ChunkChars:           runtime.ChunkChars,
			Chunks:               chunks,
			ChunksMeta:           chunksMeta,
			InlineButtonsScope:   runtime.InlineButtonsScope,
			InlineButtonsEnabled: runtime.InlineButtonsEnabled,
			CallbackDataMaxBytes: 64,
			InlineButtons:        inlineButtons,
			ActionTokens:         actionTokens,
			ActionsFallback:      assistantStepActionsFallback(actionTokens),
			ProgressUpdates:      channelProgress,
		},
	}, nil
}

func assistantTurnStatusEmoji(status string) string {
	switch status {
	case "completed":
		return "✅"
	case "needs_input":
		return "❓"
	case "timed_out", "partial_budget":
		return "⏱️"
	case "session_exited":
		return "🛑"
	default:
		return "ℹ️"
	}
}

func assistantTurnChannelMessage(statusEmoji, summary, nextAction, suggestedCommand, verbosity string, stepsUsed, maxSteps, progressPercent, elapsed, budget, timeoutStreak, timeoutStreakLimit int) string {
	message := statusEmoji + " " + summary
	switch verbosity {
	case "quiet":
		return message
	case "detailed":
		if strings.TrimSpace(nextAction) != "" {
			message += "\nNext: " + nextAction
		}
		if strings.TrimSpace(suggestedCommand) != "" {
			message += "\nCommand: " + suggestedCommand
		}
		message += "\nProgress: " + strconv.Itoa(stepsUsed) + "/" + strconv.Itoa(maxSteps) + " steps (" + strconv.Itoa(progressPercent) + "%)"
		message += "\nMeta: elapsed=" + strconv.Itoa(elapsed) + "s budget=" + strconv.Itoa(budget) + "s timeout_streak=" + strconv.Itoa(timeoutStreak) + "/" + strconv.Itoa(timeoutStreakLimit)
		return message
	default:
		if strings.TrimSpace(nextAction) != "" {
			message += "\nNext: " + nextAction
		}
		if strings.TrimSpace(suggestedCommand) != "" {
			message += "\nCommand: " + suggestedCommand
		}
		message += "\nProgress: " + strconv.Itoa(stepsUsed) + "/" + strconv.Itoa(maxSteps) + " steps (" + strconv.Itoa(progressPercent) + "%)"
		return message
	}
}

func assistantTurnProgressUpdates(milestones []assistantTurnMilestone, overallStatus string, maxSteps int, delivery assistantStepDeliveryPayload) []assistantTurnProgressUpdate {
	out := make([]assistantTurnProgressUpdate, 0, len(milestones))
	last := len(milestones) - 1
	for i, milestone := range milestones {
		message := assistantTurnStatusEmoji(milestone.Status) + " " + milestone.Summary
		action := "edit"
		replacePrevious := true
		if i == last && (overallStatus == "completed" || overallStatus == "needs_input" || overallStatus == "session_exited") {
			action = "send"
			replacePrevious = false
		}
		priority := 1
		if milestone.Status == "needs_input" || milestone.Status == "session_exited" {
			priority = 0
		} else if milestone.Status == "timed_out" {
			priority = 2
		}
		percent := 0
		if maxSteps > 0 {
			percent = milestone.Step * 100 / maxSteps
		}
		out = append(out, assistantTurnProgressUpdate{
			Step:             milestone.Step,
			Status:           milestone.Status,
			Summary:          milestone.Summary,
			NextAction:       milestone.NextAction,
			SuggestedCommand: milestone.SuggestedCommand,
			Progress: assistantTurnProgress{
				Step:     milestone.Step,
				MaxSteps: maxSteps,
				Percent:  percent,
			},
			Message: message,
			Delivery: assistantTurnProgressDelivery{
				Key:             delivery.Key,
				Action:          action,
				Priority:        priority,
				ReplacePrevious: replacePrevious,
				Coalesce:        true,
			},
		})
	}
	return out
}

func assistantTurnChannelProgressUpdates(milestones []assistantTurnMilestone, maxSteps int) []assistantTurnChannelProgressUpdate {
	out := make([]assistantTurnChannelProgressUpdate, 0, len(milestones))
	for _, milestone := range milestones {
		percent := 0
		if maxSteps > 0 {
			percent = milestone.Step * 100 / maxSteps
		}
		out = append(out, assistantTurnChannelProgressUpdate{
			Step:            milestone.Step,
			Status:          milestone.Status,
			ProgressPercent: percent,
			Message:         assistantTurnStatusEmoji(milestone.Status) + " " + milestone.Summary,
		})
	}
	return out
}

func assistantTurnDelivery(runtime assistantTurnRuntime, overallStatus, agentID, workspaceID, turnID string) assistantStepDeliveryPayload {
	key := "turn:" + turnID
	switch {
	case strings.TrimSpace(agentID) != "":
		key = "agent:" + agentID
	case strings.TrimSpace(workspaceID) != "":
		key = "workspace:" + workspaceID
	}
	delivery := assistantStepDeliveryPayload{
		Key:               key,
		Action:            "send",
		Priority:          1,
		RetryAfterSeconds: 0,
		ReplacePrevious:   false,
		DropPending:       true,
		Coalesce:          true,
	}
	switch overallStatus {
	case "timed_out", "partial", "partial_budget":
		delivery.Action = "edit"
		delivery.Priority = 2
		delivery.RetryAfterSeconds = 8
		delivery.ReplacePrevious = true
		delivery.DropPending = false
	case "needs_input", "session_exited":
		delivery.Action = "send"
		delivery.Priority = 0
		delivery.DropPending = true
	}
	return delivery
}

func assistantTurnShellCommand(cmdRef, subcommand string, args ...string) string {
	parts := []string{assistantCompatShellCommandRef(cmdRef), subcommand}
	for _, arg := range args {
		parts = append(parts, shellQuoteCommandValue(arg))
	}
	return strings.Join(parts, " ")
}

func assistantTurnWorkspaceRootForTurn(amuxBin, workspaceID string) string {
	if strings.TrimSpace(workspaceID) == "" {
		return ""
	}
	for _, args := range [][]string{
		{"workspace", "list", "--all"},
		{"workspace", "list"},
		{"workspace", "list", "--archived", "--all"},
		{"workspace", "list", "--archived"},
	} {
		if root, ok := assistantTurnWorkspaceRootFromListArgs(amuxBin, workspaceID, args...); ok {
			return root
		}
	}
	return ""
}

func assistantTurnWorkspaceRootFromListArgs(amuxBin, workspaceID string, args ...string) (string, bool) {
	out, _ := exec.Command(amuxBin, append([]string{"--json"}, args...)...).Output()
	var payload struct {
		OK    bool            `json:"ok"`
		Data  []WorkspaceInfo `json:"data"`
		Error *ErrorInfo      `json:"error"`
	}
	if json.Unmarshal(out, &payload) != nil || !payload.OK {
		return "", false
	}
	for _, item := range payload.Data {
		if item.ID == workspaceID {
			return item.Root, strings.TrimSpace(item.Root) != ""
		}
	}
	return "", false
}

func assistantTurnWorkspaceIsClean(root string) (bool, bool) {
	if strings.TrimSpace(root) == "" {
		return false, false
	}
	if info, err := exec.Command("git", "-C", root, "rev-parse", "--is-inside-work-tree").Output(); err != nil || !strings.Contains(string(info), "true") {
		return false, false
	}
	out, err := exec.Command("git", "-C", root, "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil {
		return false, false
	}
	return strings.TrimSpace(string(out)) == "", true
}

func assistantTurnWorkspaceHasCommitSince(root string, since time.Time) (bool, bool) {
	if strings.TrimSpace(root) == "" {
		return false, false
	}
	if err := exec.Command("git", "-C", root, "rev-parse", "--verify", "HEAD").Run(); err != nil {
		return false, true
	}
	out, err := exec.Command("git", "-C", root, "log", "-1", "--format=%ct").Output()
	if err != nil {
		return false, false
	}
	commitUnix, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return false, false
	}
	sinceUnix := since.Unix()
	if sinceUnix > 0 {
		sinceUnix--
	}
	return commitUnix >= sinceUnix, true
}
