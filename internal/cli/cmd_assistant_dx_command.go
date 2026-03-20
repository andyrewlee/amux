package cli

import (
	"io"
	"strings"
)

func cmdAssistantDX(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	_ = gf
	_ = wErr

	runner := assistantDXRunner{
		version:    version,
		selfScript: assistantDXSelfScriptRef(),
		invoker:    newAssistantDXInvoker(version),
	}

	payload := runner.run(args)
	assistantDXWriteJSON(w, payload)
	if !payload.OK {
		if strings.TrimSpace(payload.Status) == "command_error" {
			return ExitUsage
		}
		return ExitInternalError
	}
	return ExitOK
}

func (runner assistantDXRunner) run(args []string) assistantDXPayload {
	if len(args) == 0 {
		return assistantDXErrorPayload("usage", "missing command", "")
	}

	command := strings.TrimSpace(args[0])
	rest := args[1:]
	switch command {
	case "task":
		if len(rest) == 0 {
			return assistantDXErrorPayload("task", "missing task subcommand", "")
		}
		subcommand := strings.TrimSpace(rest[0])
		switch subcommand {
		case "start", "run":
			return runner.taskStartLike("task.start", rest[1:])
		case "status":
			return runner.taskStatus(rest[1:])
		default:
			return assistantDXErrorPayload("task", "unknown task subcommand: "+subcommand, "")
		}
	case "start":
		return runner.taskStartLike("start", rest)
	case "review":
		return runner.review(rest)
	case "continue":
		return runner.continueTask(rest)
	case "status":
		return runner.status(rest)
	case "alerts":
		return runner.status(rest)
	case "guide":
		return runner.guide(rest)
	case "assistants":
		return runner.assistants(rest)
	case "cleanup":
		return runner.cleanup(rest)
	case "workflow":
		return assistantDXErrorPayload("workflow", "workflow commands were removed; use task start/status plus continue for explicit control", "")
	case "help", "-h", "--help":
		return runner.help()
	case "project":
		if len(rest) == 0 {
			return assistantDXErrorPayload("project", "missing project subcommand", "")
		}
		switch strings.TrimSpace(rest[0]) {
		case "list":
			return runner.projectList(rest[1:])
		case "add":
			return runner.projectAdd(rest[1:])
		default:
			return assistantDXErrorPayload("project", "unknown project subcommand: "+strings.TrimSpace(rest[0]), "")
		}
	case "workspace":
		if len(rest) == 0 {
			return assistantDXErrorPayload("workspace", "missing workspace subcommand", "")
		}
		switch strings.TrimSpace(rest[0]) {
		case "list":
			return runner.workspaceList(rest[1:])
		case "create":
			return runner.workspaceCreate(rest[1:])
		default:
			return assistantDXErrorPayload("workspace", "unknown workspace subcommand: "+strings.TrimSpace(rest[0]), "")
		}
	case "terminal":
		if len(rest) == 0 {
			return assistantDXErrorPayload("terminal", "missing terminal subcommand", "")
		}
		switch strings.TrimSpace(rest[0]) {
		case "run":
			return runner.terminalRun(rest[1:])
		case "logs":
			return runner.terminalLogs(rest[1:])
		default:
			return assistantDXErrorPayload("terminal", "unknown terminal subcommand: "+strings.TrimSpace(rest[0]), "")
		}
	case "git":
		if len(rest) == 0 {
			return assistantDXErrorPayload("git", "missing git subcommand", "")
		}
		switch strings.TrimSpace(rest[0]) {
		case "ship":
			return runner.gitShip(rest[1:])
		default:
			return assistantDXErrorPayload("git", "unknown git subcommand: "+strings.TrimSpace(rest[0]), "")
		}
	default:
		return assistantDXErrorPayload(command, "unknown command: "+command, "")
	}
}

func assistantDXUsageText() string {
	return `Usage:
  assistant-dx.sh task start --workspace <id> [--assistant <name>] --prompt <text> [--wait-timeout <dur>] [--idle-threshold <dur>] [--start-lock-ttl <dur>] [--allow-new-run] [--idempotency-key <key>]
  assistant-dx.sh task status --workspace <id> [--assistant <name>]
  assistant-dx.sh start --workspace <id> [--assistant <name>] --prompt <text> [task flags...]
  assistant-dx.sh review --workspace <id> [--assistant <name>] [--prompt <text>] [task flags...] [--monitor-timeout <dur>] [--poll-interval <dur>] [--no-monitor]
  assistant-dx.sh continue [--agent <id> | --workspace <id>] [--assistant <name>] [--text <text>] [--enter] [--max-steps 1] [--turn-budget 90]

  assistant-dx.sh status [--workspace <id>] [--assistant <name>] [--include-stale]
  assistant-dx.sh alerts [same flags as status]
  assistant-dx.sh guide [--workspace <id>] [--assistant <name>] [--task <text>]

  assistant-dx.sh project list [--query <text>]
  assistant-dx.sh project add [--path <repo> | --cwd]
  assistant-dx.sh workspace list [--project <repo> | --repo <repo> | --all] [--archived]
  assistant-dx.sh workspace create <name> --project <repo> [--assistant <name>] [--base <ref>]

  assistant-dx.sh terminal run --workspace <id> --text <cmd> [--enter]
  assistant-dx.sh terminal logs --workspace <id> [--lines <n>]
  assistant-dx.sh git ship --workspace <id> [--message <msg>] [--push]
  assistant-dx.sh assistants
  assistant-dx.sh cleanup [--older-than <dur>] [--yes]

Notes:
  - workflow commands were removed; use task start/status + continue explicitly.
`
}

func (runner assistantDXRunner) help() assistantDXPayload {
	return assistantDXBuildPayload(
		true,
		"help",
		"ok",
		"assistant-dx help",
		"Run a command from usage.",
		assistantDXQuote(runner.selfScript)+" status",
		map[string]any{"usage": assistantDXUsageText()},
		[]assistantDXQuickAction{},
		"assistant-dx help",
	)
}
