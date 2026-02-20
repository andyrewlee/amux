package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	defaultSSHHost   = "ssh.app.daytona.io"
	sshReadyTimeout  = 15 * time.Second
	sshReadyInterval = 1 * time.Second
)

// AgentConfig configures interactive agent sessions.
type AgentConfig struct {
	Agent         Agent
	WorkspacePath string
	Args          []string
	Env           map[string]string
	RawMode       *bool
	RecordPath    string
}

func getSSHHost() string {
	host := envFirst("AMUX_SSH_HOST", "DAYTONA_SSH_HOST")
	if host == "" {
		return defaultSSHHost
	}
	return host
}

// quoteForShell quotes a string for safe use in shell commands.
// Uses the central ShellQuote function for consistency.
func quoteForShell(value string) string {
	return ShellQuote(value)
}

func buildEnvExportsLocal(env map[string]string) []string {
	return BuildEnvExports(env)
}

func buildEnvAssignmentsLocal(env map[string]string) string {
	return BuildEnvAssignments(env)
}

func redactExports(input string) string {
	return RedactSecrets(input)
}

func getStdoutFromResponse(resp *ExecResult) string {
	if resp == nil {
		return ""
	}
	return resp.Stdout
}

func getNodeBinDir(computer RemoteSandbox) string {
	resp, err := execCommand(computer, "command -v node", nil)
	if err == nil && resp.ExitCode == 0 {
		path := strings.TrimSpace(getStdoutFromResponse(resp))
		if path != "" {
			resp, err = execCommand(computer, "dirname "+quoteForShell(path), nil)
			if err == nil && resp.ExitCode == 0 {
				dir := strings.TrimSpace(getStdoutFromResponse(resp))
				if dir != "" {
					return dir
				}
			}
		}
	}
	return ""
}

func getHomeDir(computer RemoteSandbox) string {
	resp, err := execCommand(computer, `sh -lc "USER_NAME=$(id -un 2>/dev/null || echo daytona); HOME_DIR=$(getent passwd \"$USER_NAME\" 2>/dev/null | cut -d: -f6 || true); if [ -z \"$HOME_DIR\" ]; then HOME_DIR=/home/$USER_NAME; fi; printf \"%s\" \"$HOME_DIR\""`, nil)
	if err == nil {
		stdout := strings.TrimSpace(getStdoutFromResponse(resp))
		if stdout != "" {
			return stdout
		}
	}
	return "/home/daytona"
}

func resolveAgentCommandPath(computer RemoteSandbox, command string) string {
	home := getHomeDir(computer)

	// Check native installation locations first (before PATH lookup)
	if command == "claude" {
		// Native installer puts claude at ~/.local/bin/claude
		candidate := home + "/.local/bin/claude"
		resp, err := execCommand(computer, "test -x "+quoteForShell(candidate), nil)
		if err == nil && resp.ExitCode == 0 {
			return candidate
		}
	}
	if command == "amp" {
		candidate := home + "/.amp/bin/amp"
		resp, err := execCommand(computer, "test -x "+quoteForShell(candidate), nil)
		if err == nil && resp.ExitCode == 0 {
			return candidate
		}
	}
	if command == "droid" {
		candidate := home + "/.factory/bin/droid"
		resp, err := execCommand(computer, "test -x "+quoteForShell(candidate), nil)
		if err == nil && resp.ExitCode == 0 {
			return candidate
		}
	}

	// Check PATH
	resp, err := execCommand(computer, "command -v "+command, nil)
	if err == nil && resp.ExitCode == 0 {
		path := strings.TrimSpace(getStdoutFromResponse(resp))
		if path != "" {
			return path
		}
	}

	// Check node bin directory (for npm-installed tools)
	if nodeBin := getNodeBinDir(computer); nodeBin != "" {
		candidate := fmt.Sprintf("%s/%s", nodeBin, command)
		resp, err = execCommand(computer, "test -x "+quoteForShell(candidate), nil)
		if err == nil && resp.ExitCode == 0 {
			return candidate
		}
	}

	return command
}

func hasScript(computer RemoteSandbox) bool {
	resp, err := execCommand(computer, "command -v script", nil)
	return err == nil && resp.ExitCode == 0 && strings.TrimSpace(getStdoutFromResponse(resp)) != ""
}

type agentInteractiveRunner interface {
	RunAgentInteractive(cfg AgentConfig) (int, error)
}

// RunAgentInteractive runs the agent in an interactive session.
func RunAgentInteractive(computer RemoteSandbox, cfg AgentConfig) (int, error) {
	if runner, ok := computer.(agentInteractiveRunner); ok {
		return runner.RunAgentInteractive(cfg)
	}
	return runAgentInteractiveGeneric(computer, cfg)
}

func runAgentInteractiveGeneric(computer RemoteSandbox, cfg AgentConfig) (int, error) {
	if computer == nil {
		return 1, errors.New("sandbox is nil")
	}
	command := "bash"
	switch cfg.Agent {
	case AgentClaude:
		command = "claude"
	case AgentCodex:
		command = "codex"
	case AgentOpenCode:
		command = "opencode"
	case AgentAmp:
		command = "amp"
	case AgentGemini:
		command = "gemini"
	case AgentDroid:
		command = "droid"
	case AgentShell:
		command = "bash"
	}
	args := cfg.Args
	if args == nil {
		args = []string{}
	}
	cmdLine := command
	if len(args) > 0 {
		cmdLine = fmt.Sprintf("%s %s", command, ShellJoin(args))
	}
	if envAssignments := buildEnvAssignmentsLocal(cfg.Env); envAssignments != "" {
		cmdLine = fmt.Sprintf("%s %s", envAssignments, cmdLine)
	}
	if cfg.WorkspacePath != "" {
		cmdLine = fmt.Sprintf("cd %s && %s", ShellQuote(cfg.WorkspacePath), cmdLine)
	}
	if strings.TrimSpace(cfg.RecordPath) != "" {
		if hasScript(computer) {
			cmdLine = fmt.Sprintf("script -q -f %s -c %s", ShellQuote(cfg.RecordPath), ShellQuote(cmdLine))
		} else {
			fmt.Fprintln(os.Stderr, "Warning: recording requested but `script` is unavailable; proceeding without recording.")
		}
	}
	return computer.ExecInteractive(context.Background(), cmdLine, os.Stdin, os.Stdout, os.Stderr, nil)
}

// RunAgentCommand executes a non-interactive command for an agent.
func RunAgentCommand(computer RemoteSandbox, cfg AgentConfig, args []string) (int32, string, error) {
	command := "bash"
	switch cfg.Agent {
	case AgentClaude:
		command = "claude"
	case AgentCodex:
		command = "codex"
	case AgentOpenCode:
		command = "opencode"
	case AgentAmp:
		command = "amp"
	case AgentGemini:
		command = "gemini"
	case AgentDroid:
		command = "droid"
	}
	resolved := resolveAgentCommandPath(computer, command)
	allArgs := strings.Join(args, " ")
	cmdLine := resolved
	if allArgs != "" {
		cmdLine = fmt.Sprintf("%s %s", resolved, allArgs)
	}
	envAssignments := buildEnvAssignmentsLocal(cfg.Env)
	if envAssignments != "" {
		cmdLine = fmt.Sprintf("%s %s", envAssignments, cmdLine)
	}
	resp, err := execCommand(computer, cmdLine, &ExecOptions{Cwd: cfg.WorkspacePath})
	if err != nil {
		return 1, "", err
	}
	return int32(resp.ExitCode), getStdoutFromResponse(resp), nil
}

func sortStrings(values []string) {
	for i := 0; i < len(values)-1; i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}
