package cli

import (
	"fmt"
	"io"
	"os"
	"time"
)

// GlobalFlags holds flags that apply to all subcommands.
type GlobalFlags struct {
	JSON      bool
	NoColor   bool
	Quiet     bool
	Cwd       string
	Timeout   time.Duration
	RequestID string
}

// Run is the CLI entry point. Returns an exit code.
func Run(args []string, version, commit, date string) int {
	gf, rest, err := ParseGlobalFlags(args)
	w := os.Stdout
	wErr := os.Stderr
	setResponseContext(gf.RequestID, commandFromArgs(rest))
	defer clearResponseContext()
	if err != nil {
		if parseErrorWantsJSON(args, gf) {
			ReturnError(w, "usage_error", err.Error(), nil, version)
		} else {
			Errorf(wErr, "%v", err)
		}
		return ExitUsage
	}
	restore, err := applyRunGlobals(gf)
	if err != nil {
		if gf.JSON {
			details := map[string]any{"cwd": gf.Cwd}
			ReturnError(w, "invalid_cwd", err.Error(), details, version)
		} else {
			Errorf(wErr, "invalid --cwd: %v", err)
		}
		return ExitUsage
	}
	defer restore()

	if len(rest) == 0 {
		if gf.JSON {
			ReturnError(w, "usage_error", "Usage: amux <command> [flags]", nil, version)
		} else {
			PrintUsage(wErr)
		}
		return ExitUsage
	}

	cmd := rest[0]
	cmdArgs := rest[1:]

	switch cmd {
	case "status":
		return cmdStatus(w, wErr, gf, cmdArgs, version)
	case "doctor":
		return cmdDoctor(w, wErr, gf, cmdArgs, version)
	case "capabilities":
		return cmdCapabilities(w, wErr, gf, cmdArgs, version)
	case "logs":
		return cmdLogs(w, wErr, gf, cmdArgs, version)
	case "workspace":
		return routeWorkspace(w, wErr, gf, cmdArgs, version)
	case "agent":
		return routeAgent(w, wErr, gf, cmdArgs, version)
	case "session":
		return routeSession(w, wErr, gf, cmdArgs, version)
	case "version":
		if gf.JSON {
			PrintJSON(w, map[string]string{
				"version": version,
				"commit":  commit,
				"date":    date,
			}, version)
			return ExitOK
		}
		fmt.Fprintf(w, "amux %s (commit: %s, built: %s)\n", version, commit, date)
		return ExitOK
	case "help":
		if gf.JSON {
			PrintJSON(w, map[string]string{
				"usage": usageText(),
			}, version)
			return ExitOK
		}
		PrintUsage(w)
		return ExitOK
	default:
		if gf.JSON {
			ReturnError(w, "unknown_command", "Unknown command: "+cmd, nil, version)
		} else {
			fmt.Fprintf(wErr, "Unknown command: %s\n\n", cmd)
			PrintUsage(wErr)
		}
		return ExitUsage
	}
}

func applyRunGlobals(gf GlobalFlags) (func(), error) {
	prevTimeout := setCLITmuxTimeoutOverride(gf.Timeout)

	wdChanged := false
	prevWD := ""
	if gf.Cwd != "" {
		var err error
		prevWD, err = os.Getwd()
		if err != nil {
			setCLITmuxTimeoutOverride(prevTimeout)
			return nil, err
		}
		if err := os.Chdir(gf.Cwd); err != nil {
			setCLITmuxTimeoutOverride(prevTimeout)
			return nil, err
		}
		wdChanged = true
	}

	restore := func() {
		setCLITmuxTimeoutOverride(prevTimeout)
		if wdChanged {
			_ = os.Chdir(prevWD)
		}
	}
	return restore, nil
}

func routeWorkspace(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	if len(args) == 0 {
		if gf.JSON {
			ReturnError(w, "usage_error", "Usage: amux workspace <list|create|remove> [flags]", nil, version)
		} else {
			fmt.Fprintln(wErr, "Usage: amux workspace <list|create|remove> [flags]")
		}
		return ExitUsage
	}
	sub := args[0]
	subArgs := args[1:]
	switch sub {
	case "list", "ls":
		return cmdWorkspaceList(w, wErr, gf, subArgs, version)
	case "create":
		return cmdWorkspaceCreate(w, wErr, gf, subArgs, version)
	case "remove", "rm":
		return cmdWorkspaceRemove(w, wErr, gf, subArgs, version)
	default:
		if gf.JSON {
			ReturnError(w, "unknown_command", "Unknown workspace subcommand: "+sub, nil, version)
		} else {
			fmt.Fprintf(wErr, "Unknown workspace subcommand: %s\n", sub)
		}
		return ExitUsage
	}
}

func routeAgent(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	if len(args) == 0 {
		if gf.JSON {
			ReturnError(w, "usage_error", "Usage: amux agent <list|capture|run|send|stop|watch|job> [flags]", nil, version)
		} else {
			fmt.Fprintln(wErr, "Usage: amux agent <list|capture|run|send|stop|watch|job> [flags]")
		}
		return ExitUsage
	}
	sub := args[0]
	subArgs := args[1:]
	switch sub {
	case "list", "ls":
		return cmdAgentList(w, wErr, gf, subArgs, version)
	case "capture":
		return cmdAgentCapture(w, wErr, gf, subArgs, version)
	case "run":
		return cmdAgentRun(w, wErr, gf, subArgs, version)
	case "send":
		return cmdAgentSend(w, wErr, gf, subArgs, version)
	case "stop":
		return cmdAgentStop(w, wErr, gf, subArgs, version)
	case "watch":
		return cmdAgentWatch(w, wErr, gf, subArgs, version)
	case "job":
		return routeAgentJob(w, wErr, gf, subArgs, version)
	default:
		if gf.JSON {
			ReturnError(w, "unknown_command", "Unknown agent subcommand: "+sub, nil, version)
		} else {
			fmt.Fprintf(wErr, "Unknown agent subcommand: %s\n", sub)
		}
		return ExitUsage
	}
}

// PrintUsage writes CLI help text.
func PrintUsage(w io.Writer) {
	fmt.Fprint(w, usageText())
}

func usageText() string {
	return `Usage: amux <command> [flags]

Commands:
  status              Health check and summary
  doctor              Diagnostics check list
  capabilities        Machine-readable CLI capabilities
  logs tail           Tail the amux log file
  workspace list      List workspaces
  workspace create    Create a workspace
  workspace remove    Remove a workspace
  agent list          List running agents
  agent capture       Capture agent pane output
  agent run           Start an agent
  agent send          Send text to an agent
  agent stop          Stop an agent
  agent watch         Watch agent output (NDJSON stream)
  agent job status    Get queued send job status
  agent job cancel    Cancel queued send job (pending only)
  agent job wait      Wait for queued send job completion
  session list        List all tmux sessions
  session prune       Clean up stale sessions
  version             Print version info
  help                Show this help
  tui                 Launch TUI (default when TTY)

Global Flags:
  --json              Output as JSON envelope
  --request-id <id>   Caller-provided request correlation ID
  --no-color          Disable color output
  --quiet, -q         Suppress non-essential output
  --cwd <path>        Set working directory
  --timeout <dur>     Command timeout (e.g. 30s)
`
}

func commandFromArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	cmd := args[0]
	if len(args) < 2 {
		return cmd
	}
	switch cmd {
	case "agent":
		if len(args) >= 3 && args[1] == "job" {
			return cmd + " " + args[1] + " " + args[2]
		}
		return cmd + " " + args[1]
	case "workspace", "logs", "session":
		return cmd + " " + args[1]
	default:
		return cmd
	}
}
