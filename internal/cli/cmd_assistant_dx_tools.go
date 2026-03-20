package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (runner assistantDXRunner) guide(args []string) assistantDXPayload {
	workspace := ""
	assistant := "codex"
	task := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--workspace", "--assistant", "--task":
			if i+1 >= len(args) {
				return assistantDXErrorPayload("guide", "missing value for "+args[i], "")
			}
			switch args[i] {
			case "--workspace":
				workspace = args[i+1]
			case "--assistant":
				assistant = args[i+1]
			case "--task":
				task = args[i+1]
			}
			i++
		case "--project":
			if i+1 >= len(args) {
				return assistantDXErrorPayload("guide", "missing value for --project", "")
			}
			i++
		default:
			return assistantDXErrorPayload("guide", "unknown flag: "+args[i], "")
		}
	}

	lowerTask := strings.ToLower(strings.TrimSpace(task))
	summary := ""
	nextAction := ""
	suggested := ""
	actions := []assistantDXQuickAction{}

	switch {
	case strings.Contains(lowerTask, "review") && strings.TrimSpace(workspace) != "":
		summary = "Guide: run bounded review task"
		nextAction = "Run one bounded task step and inspect the result."
		suggested = runner.buildTaskStartCmd(workspace, assistant, assistantDXDefaultReviewPrompt, "", "")
	case strings.Contains(lowerTask, "status") || strings.Contains(lowerTask, "active"):
		summary = "Guide: check status first"
		nextAction = "Use status to decide continue/start actions."
		if strings.TrimSpace(workspace) != "" {
			suggested = fmt.Sprintf("%s status --workspace %s --assistant %s", assistantDXQuote(runner.selfScript), assistantDXQuote(workspace), assistantDXQuote(assistant))
		} else {
			suggested = assistantDXQuote(runner.selfScript) + " status"
		}
	case strings.Contains(lowerTask, "ship") || strings.Contains(lowerTask, "commit") || strings.Contains(lowerTask, "push"):
		summary = "Guide: ship workspace changes"
		nextAction = "Commit/push current workspace changes if clean."
		if strings.TrimSpace(workspace) != "" {
			suggested = fmt.Sprintf("%s git ship --workspace %s --push", assistantDXQuote(runner.selfScript), assistantDXQuote(workspace))
		} else {
			suggested = assistantDXQuote(runner.selfScript) + " workspace list --all"
		}
	case strings.TrimSpace(workspace) != "" && strings.TrimSpace(task) != "":
		summary = "Guide: run bounded task"
		nextAction = "Run one bounded task step."
		suggested = runner.buildTaskStartCmd(workspace, assistant, task, "", "")
	default:
		summary = "Guide: choose workspace then run task"
		nextAction = "Pick workspace, then run task start."
		suggested = assistantDXQuote(runner.selfScript) + " workspace list --all"
	}

	if strings.TrimSpace(workspace) != "" {
		statusCmd := fmt.Sprintf("%s status --workspace %s --assistant %s", assistantDXQuote(runner.selfScript), assistantDXQuote(workspace), assistantDXQuote(assistant))
		reviewCmd := runner.buildTaskStartCmd(workspace, assistant, assistantDXDefaultReviewPrompt, "", "")
		actions = append(actions, assistantDXNewAction("status", "Status", statusCmd, "primary", "Check workspace status"))
		actions = append(actions, assistantDXNewAction("review", "Review", reviewCmd, "primary", "Run review task"))
	} else {
		actions = append(actions, assistantDXNewAction("workspaces", "Workspaces", assistantDXQuote(runner.selfScript)+" workspace list --all", "primary", "List all workspaces"))
	}

	return assistantDXBuildPayload(
		true,
		"guide",
		"ok",
		summary,
		nextAction,
		suggested,
		map[string]any{"workspace": workspace, "assistant": assistant, "task": task},
		actions,
		"✅ "+summary,
	)
}

func (runner assistantDXRunner) terminalRun(args []string) assistantDXPayload {
	workspace := ""
	text := ""
	enter := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--workspace", "--text":
			if i+1 >= len(args) {
				return assistantDXErrorPayload("terminal.run", "missing value for "+args[i], "")
			}
			if args[i] == "--workspace" {
				workspace = args[i+1]
			} else {
				text = args[i+1]
			}
			i++
		case "--enter":
			enter = true
		default:
			return assistantDXErrorPayload("terminal.run", "unknown flag: "+args[i], "")
		}
	}
	if strings.TrimSpace(workspace) == "" || strings.TrimSpace(text) == "" {
		return assistantDXErrorPayload("terminal.run", "missing required flags: --workspace and --text", "")
	}
	commandArgs := []string{"terminal", "run", "--workspace", workspace, "--text", text}
	if enter {
		commandArgs = append(commandArgs, "--enter")
	}
	env, errPayload := runner.invokeOK("terminal.run", commandArgs...)
	if errPayload != nil {
		return *errPayload
	}
	return assistantDXBuildPayload(
		true,
		"terminal.run",
		"ok",
		"Terminal command sent.",
		"Inspect logs if needed.",
		fmt.Sprintf("%s terminal logs --workspace %s --lines 120", assistantDXQuote(runner.selfScript), assistantDXQuote(workspace)),
		assistantDXObject(env.Data),
		[]assistantDXQuickAction{},
		"✅ Terminal command sent.",
	)
}

func (runner assistantDXRunner) terminalLogs(args []string) assistantDXPayload {
	workspace := ""
	lines := "120"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--workspace", "--lines":
			if i+1 >= len(args) {
				return assistantDXErrorPayload("terminal.logs", "missing value for "+args[i], "")
			}
			if args[i] == "--workspace" {
				workspace = args[i+1]
			} else {
				lines = args[i+1]
			}
			i++
		default:
			return assistantDXErrorPayload("terminal.logs", "unknown flag: "+args[i], "")
		}
	}
	if strings.TrimSpace(workspace) == "" {
		return assistantDXErrorPayload("terminal.logs", "missing required flag: --workspace", "")
	}
	env, errPayload := runner.invokeOK("terminal.logs", "terminal", "logs", "--workspace", workspace, "--lines", lines)
	if errPayload != nil {
		return *errPayload
	}
	return assistantDXBuildPayload(
		true,
		"terminal.logs",
		"ok",
		"Terminal logs captured.",
		"Continue based on logs.",
		fmt.Sprintf("%s status --workspace %s", assistantDXQuote(runner.selfScript), assistantDXQuote(workspace)),
		assistantDXObject(env.Data),
		[]assistantDXQuickAction{},
		"✅ Terminal logs captured.",
	)
}

func (runner assistantDXRunner) assistants(args []string) assistantDXPayload {
	if len(args) > 0 {
		return assistantDXErrorPayload("assistants", "unknown flag: "+args[0], "")
	}
	configPath := strings.TrimSpace(os.Getenv("AMUX_CONFIG"))
	if configPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			configPath = ""
		} else {
			configPath = filepath.Join(home, ".amux", "config.json")
		}
	}
	assistants := assistantDXConfigAssistants(configPath)
	summary := fmt.Sprintf("%d configured assistant alias(es)", len(assistants))
	return assistantDXBuildPayload(
		true,
		"assistants",
		"ok",
		summary,
		"Use configured alias with task start/status.",
		assistantDXQuote(runner.selfScript)+" status",
		map[string]any{"config_path": configPath, "assistants": assistants},
		[]assistantDXQuickAction{},
		"✅ "+summary,
	)
}

func (runner assistantDXRunner) cleanup(args []string) assistantDXPayload {
	olderThan := "24h"
	yes := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--older-than":
			if i+1 >= len(args) {
				return assistantDXErrorPayload("cleanup", "missing value for --older-than", "")
			}
			olderThan = args[i+1]
			i++
		case "--yes":
			yes = true
		default:
			return assistantDXErrorPayload("cleanup", "unknown flag: "+args[i], "")
		}
	}
	commandArgs := []string{"session", "prune", "--older-than", olderThan}
	if yes {
		commandArgs = append(commandArgs, "--yes")
	}
	env, errPayload := runner.invokeOK("cleanup", commandArgs...)
	if errPayload != nil {
		return *errPayload
	}
	return assistantDXBuildPayload(
		true,
		"cleanup",
		"ok",
		"Cleanup completed.",
		"Refresh status.",
		assistantDXQuote(runner.selfScript)+" status",
		assistantDXObject(env.Data),
		[]assistantDXQuickAction{},
		"✅ Cleanup completed.",
	)
}

func (runner assistantDXRunner) workspaceRoot(workspace string) (string, *assistantDXPayload) {
	env, _, errPayload := runner.visibleWorkspaceListProbe("git.ship", true)
	if errPayload != nil {
		return "", errPayload
	}
	rows := assistantDXArray(env.Data)
	if archivedRows, archivedProbeState := runner.archivedWorkspaceListProbe(); archivedProbeState == assistantDXProbeOK {
		rows = assistantDXMergeWorkspaceRows(rows, archivedRows)
	}
	for _, row := range rows {
		if assistantDXWorkspaceID(row) == strings.TrimSpace(workspace) {
			return assistantDXFieldString(row, "root"), nil
		}
	}
	return "", nil
}

func (runner assistantDXRunner) gitShip(args []string) assistantDXPayload {
	workspace := ""
	message := ""
	push := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--workspace", "--message":
			if i+1 >= len(args) {
				return assistantDXErrorPayload("git.ship", "missing value for "+args[i], "")
			}
			if args[i] == "--workspace" {
				workspace = args[i+1]
			} else {
				message = args[i+1]
			}
			i++
		case "--push":
			push = true
		default:
			return assistantDXErrorPayload("git.ship", "unknown flag: "+args[i], "")
		}
	}
	if strings.TrimSpace(workspace) == "" {
		return assistantDXErrorPayload("git.ship", "missing required flag: --workspace", "")
	}
	root, errPayload := runner.workspaceRoot(workspace)
	if errPayload != nil {
		return *errPayload
	}
	if strings.TrimSpace(root) == "" {
		return assistantDXErrorPayload("git.ship", "workspace root unavailable", root)
	}
	if stat, err := os.Stat(root); err != nil || !stat.IsDir() {
		return assistantDXErrorPayload("git.ship", "workspace root unavailable", root)
	}

	statusOut, _ := exec.Command("git", "-C", root, "status", "--porcelain", "--untracked-files=all").Output()
	if strings.TrimSpace(string(statusOut)) == "" {
		return assistantDXBuildPayload(
			true,
			"git.ship",
			"ok",
			"No changes to commit.",
			"Run review or continue coding.",
			runner.buildTaskStartCmd(workspace, "codex", assistantDXDefaultReviewPrompt, "", ""),
			map[string]any{"workspace": workspace, "root": root, "changed": false},
			[]assistantDXQuickAction{},
			"✅ No changes to commit.",
		)
	}

	if output, err := exec.Command("git", "-C", root, "add", "-A").CombinedOutput(); err != nil {
		return assistantDXErrorPayload("git.ship", "git add failed", strings.TrimSpace(string(output)))
	}
	if strings.TrimSpace(message) == "" {
		message = "chore(amux): update " + workspace
	}
	commitOut, err := exec.Command("git", "-C", root, "commit", "-m", message).CombinedOutput()
	if err != nil {
		return assistantDXErrorPayload("git.ship", "git commit failed", strings.TrimSpace(string(commitOut)))
	}

	pushed := false
	pushError := ""
	if push {
		if output, err := exec.Command("git", "-C", root, "push").CombinedOutput(); err == nil {
			pushed = true
		} else {
			pushError = strings.TrimSpace(string(output))
			if pushError == "" {
				pushError = "git push failed"
			}
		}
	}

	status := "ok"
	if strings.TrimSpace(pushError) != "" {
		status = "attention"
	}
	return assistantDXBuildPayload(
		true,
		"git.ship",
		status,
		"Commit created.",
		"Run review or continue implementation.",
		runner.buildTaskStartCmd(workspace, "codex", assistantDXDefaultReviewPrompt, "", ""),
		map[string]any{
			"workspace":  workspace,
			"root":       root,
			"message":    message,
			"pushed":     pushed,
			"push_error": pushError,
			"changed":    true,
		},
		[]assistantDXQuickAction{},
		"✅ Commit created.",
	)
}
