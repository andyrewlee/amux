package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func cmdAssistantTurn(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	_ = gf
	_ = version

	if len(args) == 0 {
		assistantTurnUsage(wErr)
		payload := assistantTurnErrorPayload{
			OK:               false,
			Status:           "command_error",
			Summary:          "Invalid mode",
			NextAction:       "Fix the command flags and retry.",
			SuggestedCommand: "",
			Error:            "mode must be run or send",
		}
		assistantStepWriteJSON(w, payload)
		return ExitUsage
	}

	opts, parsePayload, exitCode := parseAssistantTurnOptions(args[0], args[1:])
	if parsePayload != nil {
		assistantStepWriteJSON(w, parsePayload)
		return exitCode
	}

	payload, code := runAssistantTurn(opts)
	assistantStepWriteJSON(w, payload)
	return code
}

func assistantTurnUsage(w io.Writer) {
	fmt.Fprint(w, "Usage:\n  assistant-turn.sh run  --workspace <id> --assistant <name> --prompt <text> [--idempotency-key <key>] [--max-steps 3] [--turn-budget 180] [--wait-timeout 60s] [--idle-threshold 10s]\n  assistant-turn.sh send --agent <id> [--text <text>] [--enter] [--idempotency-key <key>] [--max-steps 3] [--turn-budget 180] [--wait-timeout 60s] [--idle-threshold 10s]\n")
}

func parseAssistantTurnOptions(mode string, args []string) (assistantTurnOptions, *assistantTurnErrorPayload, int) {
	opts := assistantTurnOptions{
		Mode:          strings.TrimSpace(mode),
		WaitTimeout:   "60s",
		IdleThreshold: "10s",
		MaxSteps:      nonEmpty(strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_TURN_MAX_STEPS")), "3"),
		TurnBudget:    nonEmpty(strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_TURN_BUDGET_SECONDS")), "180"),
		FollowupText: nonEmpty(
			strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_TURN_FOLLOWUP_TEXT")),
			"Continue from current state and provide a concise status update and next action.",
		),
	}

	if opts.Mode != assistantStepModeRun && opts.Mode != assistantStepModeSend {
		return opts, &assistantTurnErrorPayload{
			OK:               false,
			Mode:             opts.Mode,
			Status:           "command_error",
			Summary:          "Invalid mode",
			NextAction:       "Fix the command flags and retry.",
			SuggestedCommand: "",
			Error:            "mode must be run or send",
		}, ExitUsage
	}

	for i := 0; i < len(args); {
		token := args[i]
		switch token {
		case "--wait-timeout":
			value, errPayload := assistantStepRequireValue(opts.Mode, token, args, i, false)
			if errPayload != nil {
				return opts, assistantTurnParseError(opts.Mode, errPayload.Error), ExitUsage
			}
			opts.WaitTimeout = value
			i += 2
		case "--idle-threshold":
			value, errPayload := assistantStepRequireValue(opts.Mode, token, args, i, false)
			if errPayload != nil {
				return opts, assistantTurnParseError(opts.Mode, errPayload.Error), ExitUsage
			}
			opts.IdleThreshold = value
			i += 2
		case "--max-steps":
			value, errPayload := assistantStepRequireValue(opts.Mode, token, args, i, false)
			if errPayload != nil {
				return opts, assistantTurnParseError(opts.Mode, errPayload.Error), ExitUsage
			}
			opts.MaxSteps = value
			i += 2
		case "--turn-budget":
			value, errPayload := assistantStepRequireValue(opts.Mode, token, args, i, false)
			if errPayload != nil {
				return opts, assistantTurnParseError(opts.Mode, errPayload.Error), ExitUsage
			}
			opts.TurnBudget = value
			i += 2
		case "--followup-text":
			value, errPayload := assistantStepRequireValue(opts.Mode, token, args, i, true)
			if errPayload != nil {
				return opts, assistantTurnParseError(opts.Mode, errPayload.Error), ExitUsage
			}
			opts.FollowupText = value
			i += 2
		case "--workspace":
			value, errPayload := assistantStepRequireValue(opts.Mode, token, args, i, false)
			if errPayload != nil {
				return opts, assistantTurnParseError(opts.Mode, errPayload.Error), ExitUsage
			}
			opts.Workspace = value
			i += 2
		case "--assistant":
			value, errPayload := assistantStepRequireValue(opts.Mode, token, args, i, false)
			if errPayload != nil {
				return opts, assistantTurnParseError(opts.Mode, errPayload.Error), ExitUsage
			}
			opts.Assistant = value
			i += 2
		case "--prompt":
			value, errPayload := assistantStepRequireValue(opts.Mode, token, args, i, true)
			if errPayload != nil {
				return opts, assistantTurnParseError(opts.Mode, errPayload.Error), ExitUsage
			}
			opts.Prompt = value
			i += 2
		case "--agent":
			value, errPayload := assistantStepRequireValue(opts.Mode, token, args, i, false)
			if errPayload != nil {
				return opts, assistantTurnParseError(opts.Mode, errPayload.Error), ExitUsage
			}
			opts.AgentID = value
			i += 2
		case "--text":
			value, errPayload := assistantStepRequireValue(opts.Mode, token, args, i, true)
			if errPayload != nil {
				return opts, assistantTurnParseError(opts.Mode, errPayload.Error), ExitUsage
			}
			opts.Text = value
			i += 2
		case "--enter":
			opts.Enter = true
			i++
		case "--idempotency-key":
			value, errPayload := assistantStepRequireValue(opts.Mode, token, args, i, false)
			if errPayload != nil {
				return opts, assistantTurnParseError(opts.Mode, errPayload.Error), ExitUsage
			}
			opts.IdempotencyKey = value
			i += 2
		default:
			return opts, &assistantTurnErrorPayload{
				OK:               false,
				Mode:             opts.Mode,
				Status:           "command_error",
				Summary:          "Invalid flag",
				NextAction:       "Fix the command flags and retry.",
				SuggestedCommand: "",
				Error:            "unknown flag: " + token,
			}, ExitUsage
		}
	}

	switch opts.Mode {
	case assistantStepModeRun:
		if strings.TrimSpace(opts.Workspace) == "" || strings.TrimSpace(opts.Assistant) == "" || strings.TrimSpace(opts.Prompt) == "" {
			return opts, &assistantTurnErrorPayload{
				OK:               false,
				Mode:             opts.Mode,
				Status:           "command_error",
				Summary:          "Missing required flags",
				NextAction:       "Fix the command flags and retry.",
				SuggestedCommand: "",
				Error:            "run requires --workspace, --assistant, --prompt",
			}, ExitUsage
		}
	case assistantStepModeSend:
		if strings.TrimSpace(opts.AgentID) == "" {
			return opts, &assistantTurnErrorPayload{
				OK:               false,
				Mode:             opts.Mode,
				Status:           "command_error",
				Summary:          "Missing required flags",
				NextAction:       "Fix the command flags and retry.",
				SuggestedCommand: "",
				Error:            "send requires --agent",
			}, ExitUsage
		}
	}

	return opts, nil, ExitOK
}

func assistantTurnParseError(mode, detail string) *assistantTurnErrorPayload {
	return &assistantTurnErrorPayload{
		OK:               false,
		Mode:             mode,
		Status:           "command_error",
		Summary:          "Missing value for flag",
		NextAction:       "Fix the command flags and retry.",
		SuggestedCommand: "",
		Error:            detail,
	}
}

func runAssistantTurn(opts assistantTurnOptions) (any, int) {
	runtime := assistantTurnRuntime{
		MaxSteps:            assistantTurnEnvInt("AMUX_ASSISTANT_TURN_MAX_STEPS", 3, opts.MaxSteps, 3),
		TurnBudgetSeconds:   assistantStepDurationToSeconds(nonEmpty(strings.TrimSpace(opts.TurnBudget), "180"), 180),
		FollowupText:        nonEmpty(strings.TrimSpace(opts.FollowupText), "Continue from current state and provide a concise status update and next action."),
		TimeoutStreakLimit:  assistantTurnEnvInt("AMUX_ASSISTANT_TURN_TIMEOUT_STREAK_LIMIT", 2, "", 2),
		CoalesceMilestones:  strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_TURN_COALESCE_MILESTONES")) != "false",
		FinalReserveSeconds: assistantTurnEnvInt("AMUX_ASSISTANT_TURN_FINAL_RESERVE_SECONDS", 20, "", 20),
		ChunkChars:          assistantTurnEnvInt("AMUX_ASSISTANT_TURN_CHUNK_CHARS", 1200, "", 1200),
		Verbosity:           assistantStepNormalizeVerbosity(nonEmpty(strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_TURN_VERBOSITY")), "normal")),
		InlineButtonsScope:  assistantStepNormalizeInlineButtonsScope(nonEmpty(strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_INLINE_BUTTONS_SCOPE")), "allowlist")),
		StepScriptPath:      assistantTurnStepScriptPath(),
		StepScriptCmdRef: nonEmpty(
			strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_TURN_STEP_CMD_REF")),
			assistantCompatDefaultScriptRef("assistant-step.sh"),
		),
		TurnScriptCmdRef: nonEmpty(
			strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_TURN_CMD_REF")),
			assistantCompatDefaultScriptRef("assistant-turn.sh"),
		),
		AMUXBin: assistantStepAMUXBin(),
	}
	runtime.InlineButtonsEnabled = runtime.InlineButtonsScope != "off"
	if runtime.MaxSteps <= 0 {
		runtime.MaxSteps = 3
	}
	if runtime.TimeoutStreakLimit <= 0 {
		runtime.TimeoutStreakLimit = 2
	}
	if runtime.FinalReserveSeconds < 0 {
		runtime.FinalReserveSeconds = 20
	}
	if runtime.ChunkChars <= 0 {
		runtime.ChunkChars = 1200
	}
	if runtime.TurnBudgetSeconds <= 0 {
		runtime.TurnBudgetSeconds = 180
	}
	if assistantTurnHasExplicitStepScriptConfig() {
		validatedStepScriptPath, err := assistantTurnValidateStepScriptPath(runtime.StepScriptPath)
		if err != nil {
			return assistantTurnErrorPayload{
				OK:               false,
				Mode:             opts.Mode,
				Status:           "command_error",
				Summary:          "assistant-step.sh is not executable",
				NextAction:       "Fix the command flags and retry.",
				SuggestedCommand: "",
				Error:            err.Error(),
			}, ExitUsage
		}
		runtime.StepScriptPath = validatedStepScriptPath
	} else if assistantTurnUsesNativeBinaryMode() {
		runtime.StepScriptPath = ""
	} else if validatedStepScriptPath, err := assistantTurnValidateStepScriptPath(runtime.StepScriptPath); err == nil {
		runtime.StepScriptPath = validatedStepScriptPath
	} else {
		runtime.StepScriptPath = ""
	}

	payload, err := assistantTurnRunLoop(opts, runtime)
	if err != nil {
		return assistantTurnErrorPayload{
			OK:               false,
			Mode:             opts.Mode,
			Status:           "command_error",
			Summary:          "assistant-turn failed",
			NextAction:       "Fix the command flags and retry.",
			SuggestedCommand: "",
			Error:            err.Error(),
		}, ExitInternalError
	}
	return payload, ExitOK
}

func assistantTurnEnvInt(envName string, fallback int, override string, overrideFallback int) int {
	if strings.TrimSpace(override) != "" {
		n, err := strconv.Atoi(strings.TrimSpace(override))
		if err == nil {
			return n
		}
		return overrideFallback
	}
	return assistantStepEnvInt(envName, fallback)
}

func assistantTurnStepScriptPath() string {
	return assistantCompatScriptPath("assistant-step.sh", "AMUX_ASSISTANT_TURN_STEP_SCRIPT", "AMUX_ASSISTANT_TURN_SCRIPT_DIR")
}

func assistantTurnHasExplicitStepScriptConfig() bool {
	return strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_TURN_STEP_SCRIPT")) != "" ||
		strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_TURN_SCRIPT_DIR")) != ""
}

func assistantTurnUsesNativeBinaryMode() bool {
	return strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_NATIVE_BIN")) != ""
}

func assistantTurnValidateStepScriptPath(path string) (string, error) {
	value := strings.TrimSpace(path)
	if value == "" {
		return "", errors.New("invalid step script path")
	}
	if !strings.ContainsAny(value, `/\`) {
		resolved, err := exec.LookPath(value)
		if err != nil {
			return "", err
		}
		value = resolved
	}
	info, err := os.Stat(value)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", value)
	}
	if info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("%s is not executable", value)
	}
	return value, nil
}

type assistantTurnState struct {
	TurnID                string
	StartTime             time.Time
	StepsUsed             int
	TimeoutStreak         int
	BudgetExhausted       bool
	Events                []assistantTurnEvent
	Milestones            []assistantTurnMilestone
	LastMilestone         string
	CurrentMode           string
	CurrentWorkspace      string
	CurrentAssistant      string
	CurrentPrompt         string
	CurrentAgent          string
	CurrentText           string
	CurrentEnter          bool
	StepIdempotencyBase   string
	LastStep              assistantStepPayload
	LastStatus            string
	LastSummary           string
	LastNextAction        string
	LastSuggestedCommand  string
	LastAgentID           string
	LastWorkspaceID       string
	LastAssistantOut      string
	LastSubstantiveOutput bool
	LastNeedsInput        bool
}

func assistantTurnRunLoop(opts assistantTurnOptions, runtime assistantTurnRuntime) (assistantTurnPayload, error) {
	state := assistantTurnState{
		TurnID:              "tgturn-" + strconv.FormatInt(time.Now().Unix(), 10) + "-" + strconv.Itoa(os.Getpid()),
		StartTime:           time.Now(),
		CurrentMode:         opts.Mode,
		CurrentWorkspace:    opts.Workspace,
		CurrentAssistant:    opts.Assistant,
		CurrentPrompt:       opts.Prompt,
		CurrentAgent:        opts.AgentID,
		CurrentText:         opts.Text,
		CurrentEnter:        opts.Enter,
		LastStatus:          "unknown",
		StepIdempotencyBase: nonEmpty(strings.TrimSpace(opts.IdempotencyKey), ""),
	}
	if state.StepIdempotencyBase == "" {
		state.StepIdempotencyBase = state.TurnID
	}

	for state.StepsUsed < runtime.MaxSteps {
		elapsed := int(time.Since(state.StartTime).Seconds())
		remaining := runtime.TurnBudgetSeconds - elapsed
		if remaining <= runtime.FinalReserveSeconds && state.StepsUsed > 0 {
			state.BudgetExhausted = true
			break
		}

		stepIndex := state.StepsUsed + 1
		stepPayload, err := assistantTurnRunStep(stepIndex, state, runtime, opts.WaitTimeout, opts.IdleThreshold)
		if err != nil {
			return assistantTurnPayload{}, err
		}
		state.StepsUsed = stepIndex
		state.LastStep = stepPayload

		event := assistantTurnEvent{
			OK:               stepPayload.OK,
			Mode:             stepPayload.Mode,
			Status:           nonEmpty(stepPayload.Status, "unknown"),
			Summary:          stepPayload.Summary,
			NextAction:       stepPayload.NextAction,
			SuggestedCommand: stepPayload.SuggestedCommand,
			AgentID:          stepPayload.AgentID,
			WorkspaceID:      stepPayload.WorkspaceID,
			Assistant:        stepPayload.Assistant,
			Response: assistantTurnEventResponse{
				SubstantiveOutput: stepPayload.Response.SubstantiveOutput,
				NeedsInput:        stepPayload.Response.NeedsInput,
				TimedOut:          stepPayload.Response.TimedOut,
				SessionExited:     stepPayload.Response.SessionExited,
				Changed:           stepPayload.Response.Changed,
			},
		}
		state.Events = append(state.Events, event)

		state.LastStatus = event.Status
		state.LastSummary = assistantStepRedactSecrets(event.Summary)
		state.LastNextAction = assistantStepRedactSecrets(event.NextAction)
		state.LastSuggestedCommand = assistantStepRedactSecrets(event.SuggestedCommand)
		state.LastAgentID = event.AgentID
		state.LastWorkspaceID = event.WorkspaceID
		state.LastAssistantOut = event.Assistant
		state.LastSubstantiveOutput = event.Response.SubstantiveOutput
		state.LastNeedsInput = event.Response.NeedsInput

		addMilestone := true
		if runtime.CoalesceMilestones && state.LastSummary != "" && state.LastSummary == state.LastMilestone {
			addMilestone = false
		}
		if addMilestone {
			state.Milestones = append(state.Milestones, assistantTurnMilestone{
				Step:             stepIndex,
				Status:           state.LastStatus,
				Summary:          state.LastSummary,
				NextAction:       state.LastNextAction,
				SuggestedCommand: state.LastSuggestedCommand,
			})
			state.LastMilestone = state.LastSummary
		}

		if state.LastStatus == "timed_out" {
			state.TimeoutStreak++
		} else {
			state.TimeoutStreak = 0
		}

		switch {
		case state.LastStatus == "needs_input" || state.LastNeedsInput:
			return buildAssistantTurnPayload(opts, runtime, state)
		case state.LastStatus == "session_exited":
			return buildAssistantTurnPayload(opts, runtime, state)
		case state.LastStatus == "idle" && state.LastSubstantiveOutput:
			return buildAssistantTurnPayload(opts, runtime, state)
		case state.TimeoutStreak >= runtime.TimeoutStreakLimit:
			return buildAssistantTurnPayload(opts, runtime, state)
		case strings.TrimSpace(state.LastAgentID) == "":
			return buildAssistantTurnPayload(opts, runtime, state)
		}

		state.CurrentMode = assistantStepModeSend
		state.CurrentAgent = state.LastAgentID
		state.CurrentText = runtime.FollowupText
		state.CurrentEnter = true
	}

	return buildAssistantTurnPayload(opts, runtime, state)
}

func assistantTurnRunStep(
	stepIndex int,
	state assistantTurnState,
	runtime assistantTurnRuntime,
	waitTimeout string,
	idleThreshold string,
) (assistantStepPayload, error) {
	stepID := state.StepIdempotencyBase + "-step-" + strconv.Itoa(stepIndex)
	args := []string{}
	if state.CurrentMode == assistantStepModeRun {
		args = append(args,
			"run",
			"--workspace", state.CurrentWorkspace,
			"--assistant", state.CurrentAssistant,
			"--prompt", state.CurrentPrompt,
		)
	} else {
		args = append(args, "send", "--agent", state.CurrentAgent)
		if strings.TrimSpace(state.CurrentText) != "" {
			args = append(args, "--text", state.CurrentText)
		}
		if state.CurrentEnter {
			args = append(args, "--enter")
		}
	}
	args = append(args,
		"--wait-timeout", waitTimeout,
		"--idle-threshold", idleThreshold,
		"--idempotency-key", stepID,
	)
	stepPayload, err := assistantTurnInvokeStepScript(runtime.StepScriptPath, args)
	if err != nil {
		return assistantStepPayload{}, err
	}
	return stepPayload, nil
}
