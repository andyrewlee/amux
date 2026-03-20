package cli

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func buildAssistantStepPayload(opts assistantStepOptions, data assistantStepUnderlying, amuxBin string) assistantStepPayload {
	status := "unknown"
	sessionName := data.SessionName
	agentID := data.AgentID
	workspaceID := data.WorkspaceID
	if strings.TrimSpace(workspaceID) == "" {
		workspaceID = data.ID
	}
	assistant := data.Assistant
	latestLine := ""
	responseSummary := ""
	delta := ""
	needsInput := false
	inputHint := ""
	timedOut := false
	sessionExited := false
	changed := false
	if data.Response != nil {
		status = nonEmpty(strings.TrimSpace(data.Response.Status), "unknown")
		latestLine = data.Response.LatestLine
		responseSummary = data.Response.Summary
		delta = data.Response.Delta
		needsInput = data.Response.NeedsInput
		inputHint = data.Response.InputHint
		timedOut = data.Response.TimedOut
		sessionExited = data.Response.SessionExited
		changed = data.Response.Changed
	}
	if strings.TrimSpace(workspaceID) == "" {
		if candidate := assistantStepWorkspaceFromAgentID(agentID); candidate != "" {
			workspaceID = candidate
		} else if candidate := assistantStepWorkspaceFromAgentID(opts.AgentID); candidate != "" {
			workspaceID = candidate
		}
	}

	if status == "timed_out" {
		if latestLine == "(no output yet)" {
			latestLine = ""
		}
		if responseSummary == "(no output yet)" {
			responseSummary = ""
		}
	}

	latestLine = assistantStepRedactSecrets(latestLine)
	responseSummary = assistantStepRedactSecrets(responseSummary)
	delta = assistantStepRedactSecrets(delta)
	inputHint = assistantStepRedactSecrets(inputHint)

	if assistantStepIsChromeLine(assistantStepTrimLine(latestLine)) {
		latestLine = ""
	}
	if assistantStepIsChromeLine(assistantStepTrimLine(responseSummary)) {
		responseSummary = ""
	}

	deltaCompact := assistantStepCompactAgentText(delta)
	summary := responseSummary
	if strings.TrimSpace(summary) == "" {
		summary = latestLine
	}
	if strings.TrimSpace(summary) == "" && strings.TrimSpace(deltaCompact) != "" {
		summary = assistantStepLastNonemptyLine(deltaCompact)
	}
	if assistantStepIsChromeLine(assistantStepTrimLine(summary)) {
		summary = ""
	}
	if strings.TrimSpace(latestLine) == "" && strings.TrimSpace(deltaCompact) != "" {
		latestLine = assistantStepLastNonemptyLine(deltaCompact)
	}
	if strings.TrimSpace(responseSummary) == "" && strings.TrimSpace(summary) != "" {
		responseSummary = summary
	}

	substantiveOutput := strings.TrimSpace(summary) != "" || strings.TrimSpace(latestLine) != "" || strings.TrimSpace(deltaCompact) != ""
	if status == "needs_input" && needsInput {
		inputHintTrimmed := assistantStepTrimLine(inputHint)
		inputHintLower := strings.ToLower(inputHintTrimmed)
		needsInputIsGeneric := substantiveOutput && inputHintTrimmed == ""
		switch {
		case strings.HasPrefix(inputHintLower, "what can i do for you?"),
			strings.HasPrefix(inputHintLower, "anything else?"),
			strings.HasPrefix(inputHintLower, "how would you like to proceed?"):
			needsInputIsGeneric = true
		}
		if needsInputIsGeneric {
			status = "idle"
			needsInput = false
			inputHint = ""
		}
	}

	recoveredFromCapture := false
	recoveryAttempted := false
	recoveryPollsUsed := 0
	if status == "timed_out" && !substantiveOutput && strings.TrimSpace(sessionName) != "" {
		recoveryAttempted = true
		recoveryPolls := assistantStepEnvInt("AMUX_ASSISTANT_STEP_TIMEOUT_RECOVERY_POLLS", 6)
		recoveryInterval := assistantStepEnvInt("AMUX_ASSISTANT_STEP_TIMEOUT_RECOVERY_INTERVAL", 5)
		recoveryLines := assistantStepEnvInt("AMUX_ASSISTANT_STEP_TIMEOUT_RECOVERY_LINES", 160)
		if recoveryPolls < 0 {
			recoveryPolls = 6
		}
		if recoveryInterval < 0 {
			recoveryInterval = 5
		}
		if recoveryLines <= 0 {
			recoveryLines = 160
		}
		for i := 1; i <= recoveryPolls; i++ {
			recoveryPollsUsed = i
			if recoveryInterval > 0 {
				time.Sleep(time.Duration(recoveryInterval) * time.Second)
			}
			content, ok := assistantStepCaptureContent(amuxBin, sessionName, recoveryLines)
			if !ok {
				continue
			}
			captureCompact := assistantStepCompactAgentText(content)
			recoveredLine := assistantStepLastNonemptyLine(captureCompact)
			if strings.TrimSpace(recoveredLine) == "" {
				continue
			}
			recoveredFromCapture = true
			substantiveOutput = true
			if strings.TrimSpace(summary) == "" {
				summary = recoveredLine
			}
			if strings.TrimSpace(latestLine) == "" {
				latestLine = recoveredLine
			}
			if strings.TrimSpace(responseSummary) == "" {
				responseSummary = recoveredLine
			}
			if strings.TrimSpace(deltaCompact) == "" {
				deltaCompact = captureCompact
			}
			if strings.TrimSpace(delta) == "" {
				delta = captureCompact
			}
			changed = true
			break
		}
	}

	deltaSummaryCandidate := assistantStepSanitizeSummaryText(assistantStepExtractDeltaSummaryCandidate(deltaCompact))
	if strings.TrimSpace(deltaSummaryCandidate) != "" {
		if assistantStepLineHasFileSignal(deltaSummaryCandidate) && !assistantStepLineHasFileSignal(summary) {
			summary = deltaSummaryCandidate
		} else if assistantStepSummaryIsWeak(summary) {
			summary = deltaSummaryCandidate
		}
		if assistantStepLineHasFileSignal(deltaSummaryCandidate) && !assistantStepLineHasFileSignal(responseSummary) {
			responseSummary = deltaSummaryCandidate
		} else if assistantStepSummaryIsWeak(responseSummary) {
			responseSummary = deltaSummaryCandidate
		}
		if assistantStepLineHasFileSignal(deltaSummaryCandidate) && !assistantStepLineHasFileSignal(latestLine) {
			latestLine = deltaSummaryCandidate
		} else if assistantStepSummaryIsWeak(latestLine) {
			latestLine = deltaSummaryCandidate
		}
	}

	summary = assistantStepSanitizeSummaryText(summary)
	responseSummary = assistantStepSanitizeSummaryText(responseSummary)
	latestLine = assistantStepSanitizeSummaryText(latestLine)
	if strings.TrimSpace(summary) == "" {
		switch status {
		case "timed_out":
			summary = "Timed out waiting for first visible output; agent may still be starting."
		case "session_exited":
			summary = "Agent session exited while waiting."
		case "needs_input":
			summary = "Agent needs input."
		case "idle":
			summary = "Agent step completed."
		default:
			summary = "Agent step completed with status: " + status + "."
		}
	}

	stepScriptRef := nonEmpty(strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_STEP_CMD_REF")), assistantCompatDefaultScriptRef("assistant-step.sh"))
	stepScriptCmd := assistantCompatShellCommandRef(stepScriptRef)
	blockedPermissionMode := needsInput && strings.Contains(inputHint, "waiting for local permission-mode selection")
	nextAction := ""
	suggestedCommand := ""
	switch {
	case blockedPermissionMode:
		nextAction = "Switch to a non-interactive assistant (e.g. codex) for this step."
	case status == "timed_out":
		if substantiveOutput {
			nextAction = "Send one focused follow-up prompt on the same agent and continue from the latest output."
		} else {
			nextAction = "Agent may still be starting. Run one bounded follow-up send on the same agent to force a short status update."
		}
		if strings.TrimSpace(agentID) != "" {
			suggestedCommand = fmt.Sprintf(`%s send --agent %s --text "Continue from current state and provide a one-line status update." --enter --wait-timeout 60s --idle-threshold 10s`, stepScriptCmd, shellQuoteCommandValue(agentID))
		}
	case status == "session_exited":
		nextAction = "Restart the agent in the same workspace, then continue with a focused follow-up prompt."
		if strings.TrimSpace(workspaceID) != "" && strings.TrimSpace(assistant) != "" {
			suggestedCommand = fmt.Sprintf(`%s run --workspace %s --assistant %s --prompt "Continue from where you left off and provide a concise progress update." --wait-timeout 60s --idle-threshold 10s`, stepScriptCmd, shellQuoteCommandValue(workspaceID), shellQuoteCommandValue(assistant))
		}
	case status == "idle" && !substantiveOutput:
		nextAction = "No substantive output captured yet. Run one bounded follow-up send step on the same agent."
		if strings.TrimSpace(agentID) != "" {
			suggestedCommand = fmt.Sprintf(`%s send --agent %s --text "Provide a one-line progress status." --enter --wait-timeout 60s --idle-threshold 10s`, stepScriptCmd, shellQuoteCommandValue(agentID))
		}
	case status == "needs_input":
		nextAction = "Ask the user to answer the pending prompt, then run one follow-up send step."
	}

	summary = assistantStepRedactSecrets(summary)
	latestLine = assistantStepRedactSecrets(latestLine)
	responseSummary = assistantStepRedactSecrets(responseSummary)
	deltaCompact = assistantStepRedactSecrets(deltaCompact)
	delta = assistantStepRedactSecrets(delta)
	inputHint = assistantStepRedactSecrets(inputHint)

	verbosity := assistantStepNormalizeVerbosity(nonEmpty(strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_STEP_VERBOSITY")), "normal"))
	detailLines := assistantStepDetailLinesForVerbosity(verbosity, strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_STEP_DETAIL_LINES")))
	timedOutStartup := status == "timed_out" && !substantiveOutput
	statusEmoji := assistantStepStatusEmoji(status)

	deltaExcerpt := ""
	if detailLines > 0 && strings.TrimSpace(deltaCompact) != "" {
		deltaExcerpt = assistantStepBuildDeltaExcerpt(deltaCompact, detailLines)
	}
	channelMessage := assistantStepBuildChannelMessage(statusEmoji, summary, nextAction, inputHint, deltaExcerpt, suggestedCommand, verbosity, status, changed, agentID, workspaceID, recoveryAttempted, recoveryPollsUsed)
	chunkChars := assistantStepChunkChars()
	inlineButtonsScope := assistantStepNormalizeInlineButtonsScope(nonEmpty(strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_INLINE_BUTTONS_SCOPE")), "allowlist"))
	inlineButtonsEnabled := inlineButtonsScope != "off"

	actionBundle := assistantStepBuildActionBundle(
		status,
		assistantStepActionContext{
			AgentID:               agentID,
			WorkspaceID:           workspaceID,
			Assistant:             assistant,
			Summary:               summary,
			ResponseSummary:       responseSummary,
			DeltaCompact:          deltaCompact,
			InputHint:             inputHint,
			Changed:               changed,
			BlockedPermissionMode: blockedPermissionMode,
			StepScriptCmd:         stepScriptCmd,
			NextAction:            nextAction,
			SuggestedCommand:      suggestedCommand,
			TimedOutStartup:       timedOutStartup,
		},
	)
	chunks, chunksMeta := assistantStepChunkMessage(channelMessage, chunkChars)
	inlineButtons := [][]assistantStepInlineButton{}
	if inlineButtonsEnabled {
		inlineButtons = assistantStepInlineButtons(actionBundle.QuickActions, 2)
	}

	return assistantStepPayload{
		OK:             true,
		Mode:           opts.Mode,
		Status:         status,
		StatusEmoji:    statusEmoji,
		Verbosity:      verbosity,
		Summary:        summary,
		SessionName:    sessionName,
		AgentID:        agentID,
		WorkspaceID:    workspaceID,
		Assistant:      assistant,
		IdempotencyKey: opts.IdempotencyKey,
		Response: assistantStepResponsePayload{
			LatestLine:        latestLine,
			Summary:           responseSummary,
			Delta:             delta,
			DeltaCompact:      deltaCompact,
			NeedsInput:        needsInput,
			InputHint:         inputHint,
			TimedOut:          timedOut,
			TimedOutStartup:   timedOutStartup,
			SessionExited:     sessionExited,
			Changed:           changed,
			SubstantiveOutput: substantiveOutput,
		},
		BlockedPermissionMode: blockedPermissionMode,
		RecoveredFromCapture:  recoveredFromCapture,
		Recovery: assistantStepRecoveryPayload{
			Attempted: recoveryAttempted,
			PollsUsed: recoveryPollsUsed,
		},
		Delivery:           assistantStepBuildDeliveryPayload(opts.Mode, status, substantiveOutput, timedOutStartup, sessionName, agentID, workspaceID),
		NextAction:         actionBundle.NextAction,
		SuggestedCommand:   actionBundle.SuggestedCommand,
		QuickActions:       actionBundle.QuickActions,
		QuickActionMap:     actionBundle.QuickActionMap,
		QuickActionPrompts: actionBundle.QuickActionPrompts,
		Channel: assistantStepChannelPayload{
			Message:              channelMessage,
			Verbosity:            verbosity,
			ChunkChars:           chunkChars,
			Chunks:               chunks,
			ChunksMeta:           chunksMeta,
			InlineButtonsScope:   inlineButtonsScope,
			InlineButtonsEnabled: inlineButtonsEnabled,
			CallbackDataMaxBytes: 64,
			InlineButtons:        inlineButtons,
			ActionTokens:         actionBundle.ActionTokens,
			ActionsFallback:      assistantStepActionsFallback(actionBundle.ActionTokens),
		},
	}
}
