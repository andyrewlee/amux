package cli

import (
	"fmt"
	"strings"
)

type assistantStepActionContext struct {
	AgentID               string
	WorkspaceID           string
	Assistant             string
	Summary               string
	ResponseSummary       string
	DeltaCompact          string
	InputHint             string
	Changed               bool
	BlockedPermissionMode bool
	StepScriptCmd         string
	NextAction            string
	SuggestedCommand      string
	TimedOutStartup       bool
}

func assistantStepBuildActionBundle(status string, ctx assistantStepActionContext) assistantStepActionBundle {
	contextLower := strings.ToLower(strings.Join([]string{ctx.Summary, ctx.ResponseSummary, ctx.DeltaCompact}, "\n"))
	testRemediationCommand := ""
	lintRemediationCommand := ""
	securityReviewCommand := ""
	reviewChangesCommand := ""
	if strings.TrimSpace(ctx.AgentID) != "" {
		if strings.Contains(contextLower, "test") && (strings.Contains(contextLower, "fail") || strings.Contains(contextLower, "panic") || strings.Contains(contextLower, "error")) {
			testRemediationCommand = fmt.Sprintf(`%s send --agent %s --text "Investigate failing tests, fix root causes, and report changed files plus exact test command/results." --enter --wait-timeout 60s --idle-threshold 10s`, ctx.StepScriptCmd, shellQuoteCommandValue(ctx.AgentID))
		}
		if strings.Contains(contextLower, "lint") || strings.Contains(contextLower, "format") || strings.Contains(contextLower, "gofumpt") || strings.Contains(contextLower, "style") {
			lintRemediationCommand = fmt.Sprintf(`%s send --agent %s --text "Resolve lint and formatting issues, then provide a concise summary of fixes." --enter --wait-timeout 60s --idle-threshold 10s`, ctx.StepScriptCmd, shellQuoteCommandValue(ctx.AgentID))
		}
		if strings.Contains(contextLower, "secret") || strings.Contains(contextLower, "token") || strings.Contains(contextLower, "credential") || strings.Contains(contextLower, "key leak") {
			securityReviewCommand = fmt.Sprintf(`%s send --agent %s --text "Run a focused security pass for exposed credentials/secrets and propose concrete remediation." --enter --wait-timeout 60s --idle-threshold 10s`, ctx.StepScriptCmd, shellQuoteCommandValue(ctx.AgentID))
		}
		if ctx.Changed && (assistantStepLineHasFileSignal(ctx.Summary) || assistantStepLineHasFileSignal(ctx.DeltaCompact) || strings.Contains(contextLower, "changed file") || strings.Contains(contextLower, "modified") || strings.Contains(contextLower, "refactor") || strings.Contains(contextLower, "patched")) {
			reviewChangesCommand = fmt.Sprintf(`%s send --agent %s --text "Summarize changed files, rationale, and any remaining risks in 5 bullets." --enter --wait-timeout 60s --idle-threshold 10s`, ctx.StepScriptCmd, shellQuoteCommandValue(ctx.AgentID))
		}
	}

	statusSendCommand := ""
	if strings.TrimSpace(ctx.AgentID) != "" {
		statusSendCommand = fmt.Sprintf(`%s send --agent %s --text "Provide a one-line progress status." --enter --wait-timeout 60s --idle-threshold 10s`, ctx.StepScriptCmd, shellQuoteCommandValue(ctx.AgentID))
	}

	switchCodexCommand := ""
	suggestedCommand := assistantStepRedactSecrets(ctx.SuggestedCommand)
	nextAction := assistantStepRedactSecrets(ctx.NextAction)
	if ctx.BlockedPermissionMode && strings.TrimSpace(ctx.WorkspaceID) != "" && ctx.Assistant != "codex" {
		switchCodexCommand = fmt.Sprintf(`%s run --workspace %s --assistant codex --prompt "Continue from current workspace state and provide concise status plus next action." --wait-timeout 60s --idle-threshold 10s`, ctx.StepScriptCmd, shellQuoteCommandValue(ctx.WorkspaceID))
		if strings.TrimSpace(suggestedCommand) == "" {
			suggestedCommand = switchCodexCommand
		}
	}

	replyContext := strings.Join([]string{ctx.InputHint, ctx.Summary, ctx.ResponseSummary, ctx.DeltaCompact}, "\n")
	reply1Command := ""
	reply2Command := ""
	reply3Command := ""
	reply4Command := ""
	reply5Command := ""
	replyACommand := ""
	replyBCommand := ""
	replyCCommand := ""
	replyDCommand := ""
	replyECommand := ""
	replyYesCommand := ""
	replyNoCommand := ""
	replyEnterCommand := ""
	if status == "needs_input" && strings.TrimSpace(ctx.AgentID) != "" {
		reply1Command = assistantStepReplyCommand(ctx.StepScriptCmd, ctx.AgentID, "1", true, assistantStepTextHasReplyOptionNumber(replyContext, "1"))
		reply2Command = assistantStepReplyCommand(ctx.StepScriptCmd, ctx.AgentID, "2", true, assistantStepTextHasReplyOptionNumber(replyContext, "2"))
		reply3Command = assistantStepReplyCommand(ctx.StepScriptCmd, ctx.AgentID, "3", true, assistantStepTextHasReplyOptionNumber(replyContext, "3"))
		reply4Command = assistantStepReplyCommand(ctx.StepScriptCmd, ctx.AgentID, "4", true, assistantStepTextHasReplyOptionNumber(replyContext, "4"))
		reply5Command = assistantStepReplyCommand(ctx.StepScriptCmd, ctx.AgentID, "5", true, assistantStepTextHasReplyOptionNumber(replyContext, "5"))
		replyACommand = assistantStepReplyCommand(ctx.StepScriptCmd, ctx.AgentID, "A", true, assistantStepTextHasReplyOptionLetter(replyContext, "A"))
		replyBCommand = assistantStepReplyCommand(ctx.StepScriptCmd, ctx.AgentID, "B", true, assistantStepTextHasReplyOptionLetter(replyContext, "B"))
		replyCCommand = assistantStepReplyCommand(ctx.StepScriptCmd, ctx.AgentID, "C", true, assistantStepTextHasReplyOptionLetter(replyContext, "C"))
		replyDCommand = assistantStepReplyCommand(ctx.StepScriptCmd, ctx.AgentID, "D", true, assistantStepTextHasReplyOptionLetter(replyContext, "D"))
		replyECommand = assistantStepReplyCommand(ctx.StepScriptCmd, ctx.AgentID, "E", true, assistantStepTextHasReplyOptionLetter(replyContext, "E"))
		if assistantStepTextHasYesNoPrompt(replyContext) {
			replyYesCommand = assistantStepReplyCommand(ctx.StepScriptCmd, ctx.AgentID, "yes", true, true)
			replyNoCommand = assistantStepReplyCommand(ctx.StepScriptCmd, ctx.AgentID, "no", true, true)
		}
		if assistantStepTextHasPressEnterPrompt(replyContext) {
			replyEnterCommand = assistantStepReplyCommand(ctx.StepScriptCmd, ctx.AgentID, "", true, true)
		}
	}
	if status == "needs_input" && assistantStepAnyNonEmpty(
		reply1Command, reply2Command, reply3Command, reply4Command, reply5Command,
		replyACommand, replyBCommand, replyCCommand, replyDCommand, replyECommand,
		replyYesCommand, replyNoCommand, replyEnterCommand,
	) {
		if ctx.BlockedPermissionMode {
			nextAction = "Ask the user to choose the permission mode locally. If local selection is not available, switch to a non-interactive assistant and continue."
		} else {
			nextAction = "Ask the user to choose one of the listed options, then send that exact reply (for example 1/2/3/4/5, A/B/C/D/E, yes/no, or Enter)."
		}
	}

	restartCommand := ""
	if status == "session_exited" && strings.TrimSpace(ctx.WorkspaceID) != "" && strings.TrimSpace(ctx.Assistant) != "" {
		restartCommand = fmt.Sprintf(`%s run --workspace %s --assistant %s --prompt "Continue from where you left off and provide a concise progress update." --wait-timeout 60s --idle-threshold 10s`, ctx.StepScriptCmd, shellQuoteCommandValue(ctx.WorkspaceID), shellQuoteCommandValue(ctx.Assistant))
	}

	quickActions := make([]assistantStepQuickAction, 0, 20)
	assistantStepAppendQuickAction(&quickActions, "fix_tests", "Fix Tests", testRemediationCommand, "success", "Investigate and fix failing tests")
	assistantStepAppendQuickAction(&quickActions, "fix_lint", "Fix Lint", lintRemediationCommand, "success", "Resolve lint and formatting issues")
	assistantStepAppendQuickAction(&quickActions, "security", "Security", securityReviewCommand, "danger", "Run a focused security remediation pass")
	assistantStepAppendQuickAction(&quickActions, "review", "Review", reviewChangesCommand, "primary", "Review and summarize recent code changes")
	assistantStepAppendQuickAction(&quickActions, "reply_1", "Reply 1", reply1Command, "success", "Reply with option 1")
	assistantStepAppendQuickAction(&quickActions, "reply_2", "Reply 2", reply2Command, "success", "Reply with option 2")
	assistantStepAppendQuickAction(&quickActions, "reply_3", "Reply 3", reply3Command, "success", "Reply with option 3")
	assistantStepAppendQuickAction(&quickActions, "reply_4", "Reply 4", reply4Command, "success", "Reply with option 4")
	assistantStepAppendQuickAction(&quickActions, "reply_5", "Reply 5", reply5Command, "success", "Reply with option 5")
	assistantStepAppendQuickAction(&quickActions, "reply_a", "Reply A", replyACommand, "success", "Reply with option A")
	assistantStepAppendQuickAction(&quickActions, "reply_b", "Reply B", replyBCommand, "success", "Reply with option B")
	assistantStepAppendQuickAction(&quickActions, "reply_c", "Reply C", replyCCommand, "success", "Reply with option C")
	assistantStepAppendQuickAction(&quickActions, "reply_d", "Reply D", replyDCommand, "success", "Reply with option D")
	assistantStepAppendQuickAction(&quickActions, "reply_e", "Reply E", replyECommand, "success", "Reply with option E")
	assistantStepAppendQuickAction(&quickActions, "reply_yes", "Reply Yes", replyYesCommand, "success", "Reply with yes")
	assistantStepAppendQuickAction(&quickActions, "reply_no", "Reply No", replyNoCommand, "danger", "Reply with no")
	assistantStepAppendQuickAction(&quickActions, "reply_enter", "Press Enter", replyEnterCommand, "success", "Press Enter to continue")
	assistantStepAppendQuickAction(&quickActions, "switch_codex", "Switch Codex", switchCodexCommand, "danger", "Switch to codex for non-interactive continuation")
	assistantStepAppendQuickAction(&quickActions, "suggested", "Continue", suggestedCommand, "primary", "Continue from current state")
	assistantStepAppendQuickAction(&quickActions, "status", "Status", statusSendCommand, "primary", "Request a one-line status update")
	assistantStepAppendQuickAction(&quickActions, "restart", "Restart", restartCommand, "danger", "Restart the agent in the current workspace")

	quickActionMap := make(map[string]string, len(quickActions))
	quickActionPrompts := make(map[string]string, len(quickActions))
	actionTokens := make([]string, 0, len(quickActions))
	for _, action := range quickActions {
		quickActionMap[action.CallbackData] = action.Command
		quickActionPrompts[action.CallbackData] = action.Prompt
		actionTokens = append(actionTokens, action.CallbackData)
	}

	return assistantStepActionBundle{
		QuickActions:       quickActions,
		QuickActionMap:     quickActionMap,
		QuickActionPrompts: quickActionPrompts,
		ActionTokens:       actionTokens,
		NextAction:         nextAction,
		SuggestedCommand:   suggestedCommand,
		StatusSendCommand:  statusSendCommand,
		RestartCommand:     restartCommand,
		SwitchCodexCommand: switchCodexCommand,
	}
}

func assistantStepBuildDeliveryPayload(mode, status string, substantiveOutput, timedOutStartup bool, sessionName, agentID, workspaceID string) assistantStepDeliveryPayload {
	deliveryKey := "mode:" + mode
	switch {
	case strings.TrimSpace(agentID) != "":
		deliveryKey = "agent:" + agentID
	case strings.TrimSpace(sessionName) != "":
		deliveryKey = "session:" + sessionName
	case strings.TrimSpace(workspaceID) != "":
		deliveryKey = "workspace:" + workspaceID
	}
	delivery := assistantStepDeliveryPayload{
		Key:               deliveryKey,
		Action:            "send",
		Priority:          1,
		RetryAfterSeconds: 0,
		ReplacePrevious:   false,
		DropPending:       false,
		Coalesce:          true,
	}
	switch status {
	case "timed_out":
		delivery.Action = "edit"
		delivery.Priority = 2
		delivery.ReplacePrevious = true
		delivery.RetryAfterSeconds = 5
		if timedOutStartup {
			delivery.RetryAfterSeconds = 8
		}
	case "needs_input", "session_exited":
		delivery.Action = "send"
		delivery.Priority = 0
		delivery.DropPending = true
	case "idle":
		if substantiveOutput {
			delivery.DropPending = true
		} else {
			delivery.Action = "edit"
			delivery.Priority = 2
			delivery.ReplacePrevious = true
			delivery.RetryAfterSeconds = 5
		}
	}
	return delivery
}

func assistantStepReplyCommand(stepScriptCmd, agentID, text string, enter, enabled bool) string {
	if !enabled {
		return ""
	}
	if text == "" {
		return fmt.Sprintf(`%s send --agent %s --enter --wait-timeout 60s --idle-threshold 10s`, stepScriptCmd, shellQuoteCommandValue(agentID))
	}
	suffix := ""
	if enter {
		suffix = " --enter"
	}
	return fmt.Sprintf(`%s send --agent %s --text "%s"%s --wait-timeout 60s --idle-threshold 10s`, stepScriptCmd, shellQuoteCommandValue(agentID), text, suffix)
}

func assistantStepAppendQuickAction(actions *[]assistantStepQuickAction, id, label, command, style, prompt string) {
	if strings.TrimSpace(command) == "" {
		return
	}
	*actions = append(*actions, assistantStepQuickAction{
		ID:           id,
		Label:        label,
		Command:      command,
		Style:        style,
		CallbackData: "qa:" + id,
		Prompt:       prompt,
	})
}

func assistantStepActionsFallback(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}
	return "Actions: " + strings.Join(tokens, " | ")
}

func assistantStepInlineButtons(actions []assistantStepQuickAction, rowSize int) [][]assistantStepInlineButton {
	if rowSize <= 0 {
		rowSize = 2
	}
	rows := make([][]assistantStepInlineButton, 0, (len(actions)+rowSize-1)/rowSize)
	for i := 0; i < len(actions); i += rowSize {
		end := i + rowSize
		if end > len(actions) {
			end = len(actions)
		}
		row := make([]assistantStepInlineButton, 0, end-i)
		for _, action := range actions[i:end] {
			row = append(row, assistantStepInlineButton{
				Text:         action.Label,
				CallbackData: action.CallbackData,
				Style:        action.Style,
			})
		}
		rows = append(rows, row)
	}
	return rows
}

func assistantStepChunkMessage(message string, chunkChars int) ([]string, []assistantStepChunkMeta) {
	rawChunks := assistantStepSmartSplit(message, chunkChars)
	if len(rawChunks) == 0 {
		rawChunks = []string{""}
	}
	chunks := make([]string, 0, len(rawChunks))
	meta := make([]assistantStepChunkMeta, 0, len(rawChunks))
	total := len(rawChunks)
	for i, chunk := range rawChunks {
		text := chunk
		if i > 0 {
			text = fmt.Sprintf("continued (%d/%d)\n%s", i+1, total, chunk)
		}
		chunks = append(chunks, text)
		meta = append(meta, assistantStepChunkMeta{Index: i + 1, Total: total, Text: text})
	}
	return chunks, meta
}

func assistantStepSmartSplit(text string, size int) []string {
	if size <= 0 {
		size = 1200
	}
	if len(text) == 0 {
		return nil
	}
	source := text
	chunks := make([]string, 0, 1)
	for len(source) > size {
		head := source[:size]
		cut := assistantStepNextChunkCut(head, size)
		chunk := strings.TrimLeft(source[:cut], "\n")
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		source = source[cut:]
	}
	source = strings.TrimLeft(source, "\n")
	if source != "" {
		chunks = append(chunks, source)
	}
	return chunks
}

func assistantStepNextChunkCut(head string, size int) int {
	findings := []int{strings.LastIndex(head, "\n\n"), strings.LastIndex(head, "\n"), strings.LastIndex(head, " ")}
	for _, idx := range findings {
		if idx >= size/3 {
			if idx == strings.LastIndex(head, "\n\n") {
				return idx + 2
			}
			return idx + 1
		}
	}
	return size
}

func assistantStepBuildChannelMessage(statusEmoji, summary, nextAction, inputHint, deltaExcerpt, suggestedCommand, verbosity, status string, changed bool, agentID, workspaceID string, recoveryAttempted bool, recoveryPollsUsed int) string {
	message := statusEmoji + " " + summary
	switch verbosity {
	case "quiet":
		if strings.TrimSpace(inputHint) != "" && status == "needs_input" {
			message += "\nInput: " + inputHint
		}
	case "detailed":
		if strings.TrimSpace(nextAction) != "" {
			message += "\nNext: " + nextAction
		}
		if strings.TrimSpace(inputHint) != "" && status == "needs_input" {
			message += "\nInput: " + inputHint
		}
		if strings.TrimSpace(deltaExcerpt) != "" {
			message += "\nDetails:\n" + deltaExcerpt
		}
		if strings.TrimSpace(suggestedCommand) != "" {
			message += "\nCommand: " + suggestedCommand
		}
		message += fmt.Sprintf("\nMeta: status=%s changed=%t agent=%s workspace=%s", status, changed, nonEmpty(strings.TrimSpace(agentID), "none"), nonEmpty(strings.TrimSpace(workspaceID), "none"))
		if recoveryAttempted {
			message += fmt.Sprintf("\nRecovery: attempted=true polls=%d", recoveryPollsUsed)
		}
	default:
		if strings.TrimSpace(nextAction) != "" {
			message += "\nNext: " + nextAction
		}
		if strings.TrimSpace(inputHint) != "" && status == "needs_input" {
			message += "\nInput: " + inputHint
		}
		if strings.TrimSpace(deltaExcerpt) != "" {
			message += "\nDetails:\n" + deltaExcerpt
		}
		if strings.TrimSpace(suggestedCommand) != "" {
			message += "\nCommand: " + suggestedCommand
		}
	}
	return message
}

func assistantStepChunkChars() int {
	value := assistantStepEnvInt("AMUX_ASSISTANT_STEP_CHUNK_CHARS", 1200)
	if value <= 0 {
		return 1200
	}
	return value
}

func assistantStepStatusEmoji(status string) string {
	switch status {
	case "idle":
		return "✅"
	case "needs_input":
		return "❓"
	case "timed_out":
		return "⏱️"
	case "session_exited":
		return "🛑"
	default:
		return "ℹ️"
	}
}

func assistantStepDetailLinesForVerbosity(verbosity, raw string) int {
	if strings.TrimSpace(raw) != "" {
		if value := assistantStepEnvInt("AMUX_ASSISTANT_STEP_DETAIL_LINES", -1); value >= 0 {
			return value
		}
	}
	switch verbosity {
	case "quiet":
		return 0
	case "detailed":
		return 8
	default:
		return 3
	}
}

func assistantStepNormalizeVerbosity(value string) string {
	switch value {
	case "quiet", "normal", "detailed":
		return value
	default:
		return "normal"
	}
}

func assistantStepNormalizeInlineButtonsScope(value string) string {
	switch value {
	case "off", "dm", "group", "all", "allowlist":
		return value
	default:
		return "allowlist"
	}
}
