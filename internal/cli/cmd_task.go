package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/validation"
)

type taskQuickAction struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Command string `json:"command"`
	Prompt  string `json:"prompt,omitempty"`
}

type taskCommandResult struct {
	Mode             string            `json:"mode"`
	Status           string            `json:"status"`
	OverallStatus    string            `json:"overall_status"`
	Summary          string            `json:"summary"`
	AgentID          string            `json:"agent_id,omitempty"`
	SessionName      string            `json:"session_name,omitempty"`
	WorkspaceID      string            `json:"workspace_id"`
	Assistant        string            `json:"assistant"`
	LatestLine       string            `json:"latest_line,omitempty"`
	NeedsInput       bool              `json:"needs_input,omitempty"`
	InputHint        string            `json:"input_hint,omitempty"`
	NextAction       string            `json:"next_action,omitempty"`
	SuggestedCommand string            `json:"suggested_command,omitempty"`
	Prompt           string            `json:"prompt,omitempty"`
	QuickActions     []taskQuickAction `json:"quick_actions,omitempty"`
}

func routeTask(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	return routeSubcommand(w, wErr, gf, args, version, subcommandRouter{
		scope: "task",
		usage: "Usage: amux task <start|status> [flags]",
		handlers: map[string]commandHandler{
			"start":  cmdTaskStart,
			"status": cmdTaskStatus,
		},
	})
}

func cmdTaskStart(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux task start --workspace <id> [--assistant <name>] --prompt <text> [--wait-timeout <duration>] [--idle-threshold <duration>] [--start-lock-ttl <duration>] [--allow-new-run] [--idempotency-key <key>] [--json]"
	fs := newFlagSet("task start")
	workspace := fs.String("workspace", "", "workspace ID (required)")
	assistant := fs.String("assistant", "codex", "assistant name")
	prompt := fs.String("prompt", "", "task prompt (required)")
	waitTimeout := fs.Duration("wait-timeout", 30*time.Second, "max wait for first bounded task step")
	idleThreshold := fs.Duration("idle-threshold", 10*time.Second, "idle threshold for wait")
	startLockTTL := fs.Duration("start-lock-ttl", 120*time.Second, "startup lock TTL")
	allowNewRun := fs.Bool("allow-new-run", false, "allow starting a new task while another agent tab is active")
	idempotencyKey := fs.String("idempotency-key", "", "idempotency key for safe retries")
	if err := fs.Parse(args); err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if fs.NArg() > 0 {
		return returnUsageError(w, wErr, gf, usage, version, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " ")))
	}
	if strings.TrimSpace(*workspace) == "" || strings.TrimSpace(*prompt) == "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	if *waitTimeout <= 0 {
		return returnUsageError(w, wErr, gf, usage, version, errors.New("--wait-timeout must be > 0"))
	}
	if *idleThreshold <= 0 {
		return returnUsageError(w, wErr, gf, usage, version, errors.New("--idle-threshold must be > 0"))
	}
	if *startLockTTL <= 0 {
		return returnUsageError(w, wErr, gf, usage, version, errors.New("--start-lock-ttl must be > 0"))
	}

	assistantName := strings.ToLower(strings.TrimSpace(*assistant))
	if err := validation.ValidateAssistant(assistantName); err != nil {
		return returnUsageError(w, wErr, gf, usage, version, fmt.Errorf("invalid --assistant: %w", err))
	}
	wsID, err := parseWorkspaceIDFlag(*workspace)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}

	svc, err := NewServices(version)
	if err != nil {
		return taskReturnError(w, wErr, gf, version, ExitInternalError, "init_failed", err.Error(), nil)
	}
	if _, err := svc.Store.Load(wsID); err != nil {
		return taskReturnError(w, wErr, gf, version, ExitNotFound, "not_found", fmt.Sprintf("workspace %s not found", wsID), nil)
	}
	if _, ok := svc.Config.Assistants[assistantName]; !ok {
		return taskReturnError(w, wErr, gf, version, ExitUsage, "unknown_assistant", "unknown assistant: "+assistantName, nil)
	}

	if !*allowNewRun {
		candidate, snap, err := findLatestTaskAgentSnapshot(svc.TmuxOpts, string(wsID), assistantName)
		if err != nil {
			return taskReturnError(w, wErr, gf, version, ExitInternalError, "agent_lookup_failed", err.Error(), nil)
		}
		if candidate != nil {
			result := buildTaskNeedsConfirmationResult(string(wsID), assistantName, *prompt, *candidate, snap)
			return emitTaskResult(w, gf, version, result)
		}
	}

	lock, acquired, err := acquireTaskStartLock(svc.Config.Paths.Home, string(wsID), assistantName, *startLockTTL)
	if err != nil {
		return taskReturnError(w, wErr, gf, version, ExitInternalError, "start_lock_failed", err.Error(), nil)
	}
	if !acquired {
		result := taskCommandResult{
			Mode:             "run",
			Status:           "attention",
			OverallStatus:    "in_progress",
			Summary:          "Task startup is already in progress for this workspace.",
			WorkspaceID:      string(wsID),
			Assistant:        assistantName,
			NextAction:       "Wait briefly, then check task status.",
			SuggestedCommand: fmt.Sprintf("amux --json task status --workspace %s --assistant %s", string(wsID), assistantName),
			QuickActions: []taskQuickAction{
				{ID: "status", Label: "Status", Command: fmt.Sprintf("amux --json task status --workspace %s --assistant %s", string(wsID), assistantName), Prompt: "Check active task status"},
			},
		}
		return emitTaskResult(w, gf, version, result)
	}
	defer lock.release()

	runResult, err := taskRunAgent(svc, wsID, assistantName, *prompt, *waitTimeout, *idleThreshold, strings.TrimSpace(*idempotencyKey), version)
	if err != nil {
		return taskReturnError(w, wErr, gf, version, ExitInternalError, "task_start_failed", err.Error(), nil)
	}
	result := buildTaskStartResult(string(wsID), assistantName, *prompt, runResult)
	return emitTaskResult(w, gf, version, result)
}

func cmdTaskStatus(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux task status --workspace <id> [--assistant <name>] [--json]"
	fs := newFlagSet("task status")
	workspace := fs.String("workspace", "", "workspace ID (required)")
	assistant := fs.String("assistant", "codex", "assistant name")
	if err := fs.Parse(args); err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if fs.NArg() > 0 {
		return returnUsageError(w, wErr, gf, usage, version, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " ")))
	}
	if strings.TrimSpace(*workspace) == "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	assistantName := strings.ToLower(strings.TrimSpace(*assistant))
	if err := validation.ValidateAssistant(assistantName); err != nil {
		return returnUsageError(w, wErr, gf, usage, version, fmt.Errorf("invalid --assistant: %w", err))
	}
	wsID, err := parseWorkspaceIDFlag(*workspace)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}

	svc, err := NewServices(version)
	if err != nil {
		return taskReturnError(w, wErr, gf, version, ExitInternalError, "init_failed", err.Error(), nil)
	}
	if _, err := svc.Store.Load(wsID); err != nil {
		return taskReturnError(w, wErr, gf, version, ExitNotFound, "not_found", fmt.Sprintf("workspace %s not found", wsID), nil)
	}

	candidate, snap, err := findLatestTaskAgentSnapshot(svc.TmuxOpts, string(wsID), assistantName)
	if err != nil {
		return taskReturnError(w, wErr, gf, version, ExitInternalError, "agent_lookup_failed", err.Error(), nil)
	}
	if candidate == nil {
		result := taskCommandResult{
			Mode:             "status",
			Status:           "idle",
			OverallStatus:    "completed",
			Summary:          "No active assistant task run found for this workspace/assistant.",
			WorkspaceID:      string(wsID),
			Assistant:        assistantName,
			NextAction:       "Start a task when ready.",
			SuggestedCommand: fmt.Sprintf("amux --json task start --workspace %s --assistant %s --prompt %s", quoteCommandValue(string(wsID)), quoteCommandValue(assistantName), quoteCommandValue("Continue from current state and report status plus next action.")),
		}
		return emitTaskResult(w, gf, version, result)
	}

	result := buildTaskStatusResult(string(wsID), assistantName, *candidate, snap)
	return emitTaskResult(w, gf, version, result)
}

func emitTaskResult(w io.Writer, gf GlobalFlags, version string, result taskCommandResult) int {
	if gf.JSON {
		PrintJSON(w, result, version)
		return ExitOK
	}
	PrintHuman(w, func(w io.Writer) {
		fmt.Fprintf(w, "%s\n", result.Summary)
		if strings.TrimSpace(result.AgentID) != "" {
			fmt.Fprintf(w, "Agent: %s\n", result.AgentID)
		}
		if strings.TrimSpace(result.NextAction) != "" {
			fmt.Fprintf(w, "Next: %s\n", result.NextAction)
		}
		if strings.TrimSpace(result.SuggestedCommand) != "" {
			fmt.Fprintf(w, "Command: %s\n", result.SuggestedCommand)
		}
	})
	return ExitOK
}

func taskReturnError(w, wErr io.Writer, gf GlobalFlags, version string, code int, errCode, message string, details map[string]any) int {
	if gf.JSON {
		ReturnError(w, errCode, message, details, version)
		return code
	}
	Errorf(wErr, "%s", message)
	return code
}
