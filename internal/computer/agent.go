package computer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultSSHHost       = "ssh.app.daytona.io"
	sshReadyTimeout      = 15 * time.Second
	sshReadyInterval     = 1 * time.Second
	agentInstallBasePath = "/amux/.installed"
	agentInstallTTL      = 24 * time.Hour // Re-check for updates after 24 hours
)

// AgentConfig configures interactive agent sessions.
type AgentConfig struct {
	Agent         Agent
	WorkspacePath string
	Args          []string
	Env           map[string]string
	RawMode       *bool
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

func getNodeBinDir(sandbox RemoteComputer) string {
	resp, err := execCommand(sandbox, "command -v node", nil)
	if err == nil && resp.ExitCode == 0 {
		path := strings.TrimSpace(getStdoutFromResponse(resp))
		if path != "" {
			resp, err = execCommand(sandbox, fmt.Sprintf("dirname %s", quoteForShell(path)), nil)
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

func getHomeDir(sandbox RemoteComputer) string {
	resp, err := execCommand(sandbox, `sh -lc "USER_NAME=$(id -un 2>/dev/null || echo daytona); HOME_DIR=$(getent passwd \"$USER_NAME\" 2>/dev/null | cut -d: -f6 || true); if [ -z \"$HOME_DIR\" ]; then HOME_DIR=/home/$USER_NAME; fi; printf \"%s\" \"$HOME_DIR\""`, nil)
	if err == nil {
		stdout := strings.TrimSpace(getStdoutFromResponse(resp))
		if stdout != "" {
			return stdout
		}
	}
	return "/home/daytona"
}

func resolveAgentCommandPath(sandbox RemoteComputer, command string) string {
	home := getHomeDir(sandbox)

	// Check native installation locations first (before PATH lookup)
	if command == "claude" {
		// Native installer puts claude at ~/.local/bin/claude
		candidate := fmt.Sprintf("%s/.local/bin/claude", home)
		resp, err := execCommand(sandbox, fmt.Sprintf("test -x %s", quoteForShell(candidate)), nil)
		if err == nil && resp.ExitCode == 0 {
			return candidate
		}
	}
	if command == "amp" {
		candidate := fmt.Sprintf("%s/.amp/bin/amp", home)
		resp, err := execCommand(sandbox, fmt.Sprintf("test -x %s", quoteForShell(candidate)), nil)
		if err == nil && resp.ExitCode == 0 {
			return candidate
		}
	}
	if command == "droid" {
		candidate := fmt.Sprintf("%s/.factory/bin/droid", home)
		resp, err := execCommand(sandbox, fmt.Sprintf("test -x %s", quoteForShell(candidate)), nil)
		if err == nil && resp.ExitCode == 0 {
			return candidate
		}
	}

	// Check PATH
	resp, err := execCommand(sandbox, fmt.Sprintf("command -v %s", command), nil)
	if err == nil && resp.ExitCode == 0 {
		path := strings.TrimSpace(getStdoutFromResponse(resp))
		if path != "" {
			return path
		}
	}

	// Check node bin directory (for npm-installed tools)
	if nodeBin := getNodeBinDir(sandbox); nodeBin != "" {
		candidate := fmt.Sprintf("%s/%s", nodeBin, command)
		resp, err = execCommand(sandbox, fmt.Sprintf("test -x %s", quoteForShell(candidate)), nil)
		if err == nil && resp.ExitCode == 0 {
			return candidate
		}
	}

	return command
}

func hasScript(sandbox RemoteComputer) bool {
	resp, err := execCommand(sandbox, "command -v script", nil)
	return err == nil && resp.ExitCode == 0 && strings.TrimSpace(getStdoutFromResponse(resp)) != ""
}

func agentInstallMarker(agent string) string {
	return fmt.Sprintf("%s/%s", agentInstallBasePath, agent)
}

// isAgentInstallFresh checks if the agent was installed recently (within TTL).
// Returns true if the marker exists and is fresh, false if missing or stale.
func isAgentInstallFresh(sandbox RemoteComputer, agent string) bool {
	marker := agentInstallMarker(agent)
	// Check if marker exists and get its age in seconds
	resp, err := execCommand(sandbox, fmt.Sprintf(
		`if [ -f %s ]; then stat -c %%Y %s 2>/dev/null || stat -f %%m %s 2>/dev/null; else echo 0; fi`,
		marker, marker, marker,
	), nil)
	if err != nil {
		return false
	}
	stdout := strings.TrimSpace(getStdoutFromResponse(resp))
	if stdout == "" || stdout == "0" {
		return false
	}
	// Parse the modification timestamp
	modTime, err := strconv.ParseInt(stdout, 10, 64)
	if err != nil || modTime == 0 {
		return false
	}
	// Check if within TTL
	age := time.Since(time.Unix(modTime, 0))
	return age < agentInstallTTL
}

// touchAgentMarker creates or updates the install marker timestamp.
func touchAgentMarker(sandbox RemoteComputer, agent string) {
	marker := agentInstallMarker(agent)
	_, _ = execCommand(sandbox, SafeCommands.MkdirP(agentInstallBasePath), nil)
	_, _ = execCommand(sandbox, SafeCommands.Touch(marker), nil)
}

func installClaude(sandbox RemoteComputer, verbose bool, forceUpdate bool) error {
	if verbose {
		fmt.Println("Installing Claude Code...")
	}
	// Check for native installation first (~/.local/bin/claude), then fall back to PATH
	home := getHomeDir(sandbox)
	nativeCheck := fmt.Sprintf("test -x %s/.local/bin/claude", home)
	resp, _ := execCommand(sandbox, nativeCheck, nil)
	nativeInstalled := resp != nil && resp.ExitCode == 0

	pathResp, _ := execCommand(sandbox, "which claude", nil)
	pathInstalled := pathResp != nil && pathResp.ExitCode == 0

	alreadyInstalled := nativeInstalled || pathInstalled
	if alreadyInstalled && !forceUpdate {
		if verbose {
			fmt.Println("Claude Code already installed")
		}
	} else {
		action := "Installing"
		if alreadyInstalled {
			action = "Updating"
		}
		if verbose {
			fmt.Printf("%s Claude Code...\n", action)
		}
		// Use native installer (recommended by Anthropic)
		// Installs to ~/.local/bin/claude with binary at ~/.local/share/claude/versions/{version}
		resp, err := execCommand(sandbox, `bash -lc "curl -fsSL https://claude.ai/install.sh | bash"`, nil)
		if err != nil || resp.ExitCode != 0 {
			return errors.New("failed to install claude code via native installer")
		}
		if verbose {
			fmt.Println("Claude Code installed")
		}
	}
	touchAgentMarker(sandbox, "claude")
	return nil
}

func installCodex(sandbox RemoteComputer, verbose bool, forceUpdate bool) error {
	if verbose {
		fmt.Println("Installing Codex CLI...")
	}
	resp, _ := execCommand(sandbox, "which codex", nil)
	alreadyInstalled := resp != nil && resp.ExitCode == 0
	if alreadyInstalled && !forceUpdate {
		if verbose {
			fmt.Println("Codex CLI already installed")
		}
	} else {
		action := "Installing"
		if alreadyInstalled {
			action = "Updating"
		}
		if verbose {
			fmt.Printf("%s Codex CLI...\n", action)
		}
		resp, err := execCommand(sandbox, "npm install -g @openai/codex@latest", nil)
		if err != nil || resp.ExitCode != 0 {
			return errors.New("failed to install codex cli in computer")
		}
		if verbose {
			fmt.Println("Codex CLI installed")
		}
	}
	touchAgentMarker(sandbox, "codex")
	return nil
}

func installOpenCode(sandbox RemoteComputer, verbose bool, forceUpdate bool) error {
	if verbose {
		fmt.Println("Installing OpenCode CLI...")
	}
	resp, _ := execCommand(sandbox, "which opencode", nil)
	alreadyInstalled := resp != nil && resp.ExitCode == 0
	if alreadyInstalled && !forceUpdate {
		if verbose {
			fmt.Println("OpenCode CLI already installed")
		}
	} else {
		action := "Installing"
		if alreadyInstalled {
			action = "Updating"
		}
		if verbose {
			fmt.Printf("%s OpenCode CLI...\n", action)
		}
		resp, err := execCommand(sandbox, `bash -lc "curl -fsSL https://opencode.ai/install | bash"`, nil)
		if err != nil || resp.ExitCode != 0 {
			if verbose {
				fmt.Println("OpenCode install script failed, trying npm...")
			}
			resp, err = execCommand(sandbox, "npm install -g opencode-ai@latest", nil)
			if err != nil || resp.ExitCode != 0 {
				return errors.New("failed to install opencode cli in computer")
			}
		}
		if verbose {
			fmt.Println("OpenCode CLI installed")
		}
	}
	touchAgentMarker(sandbox, "opencode")
	return nil
}

func installAmp(sandbox RemoteComputer, verbose bool, forceUpdate bool) error {
	if verbose {
		fmt.Println("Installing Amp CLI...")
	}
	home := getHomeDir(sandbox)
	ampBin := fmt.Sprintf("%s/.amp/bin/amp", home)
	resp, _ := execCommand(sandbox, fmt.Sprintf("sh -lc \"command -v amp >/dev/null 2>&1 || test -x %s\"", quoteForShell(ampBin)), nil)
	alreadyInstalled := resp != nil && resp.ExitCode == 0
	if alreadyInstalled && !forceUpdate {
		if verbose {
			fmt.Println("Amp CLI already installed")
		}
	} else {
		action := "Installing"
		if alreadyInstalled {
			action = "Updating"
		}
		if verbose {
			fmt.Printf("%s Amp CLI...\n", action)
		}
		resp, err := execCommand(sandbox, `bash -lc "curl -fsSL https://ampcode.com/install.sh | bash"`, nil)
		if err != nil || resp.ExitCode != 0 {
			if verbose {
				fmt.Println("Amp install script failed, trying npm...")
			}
			resp, err = execCommand(sandbox, "npm install -g @sourcegraph/amp@latest", nil)
			if err != nil || resp.ExitCode != 0 {
				return errors.New("failed to install amp cli in computer")
			}
		}
		if verbose {
			fmt.Println("Amp CLI installed")
		}
	}
	touchAgentMarker(sandbox, "amp")
	return nil
}

func installGemini(sandbox RemoteComputer, verbose bool, forceUpdate bool) error {
	if verbose {
		fmt.Println("Installing Gemini CLI...")
	}
	resp, _ := execCommand(sandbox, "which gemini", nil)
	alreadyInstalled := resp != nil && resp.ExitCode == 0
	if alreadyInstalled && !forceUpdate {
		if verbose {
			fmt.Println("Gemini CLI already installed")
		}
	} else {
		action := "Installing"
		if alreadyInstalled {
			action = "Updating"
		}
		if verbose {
			fmt.Printf("%s Gemini CLI...\n", action)
		}
		resp, err := execCommand(sandbox, "npm install -g @google/gemini-cli@latest", nil)
		if err != nil || resp.ExitCode != 0 {
			return errors.New("failed to install gemini cli in computer")
		}
		if verbose {
			fmt.Println("Gemini CLI installed")
		}
	}
	touchAgentMarker(sandbox, "gemini")
	return nil
}

func installDroid(sandbox RemoteComputer, verbose bool, forceUpdate bool) error {
	if verbose {
		fmt.Println("Installing Droid CLI...")
	}
	resp, _ := execCommand(sandbox, "which droid", nil)
	alreadyInstalled := resp != nil && resp.ExitCode == 0
	if alreadyInstalled && !forceUpdate {
		if verbose {
			fmt.Println("Droid CLI already installed")
		}
	} else {
		action := "Installing"
		if alreadyInstalled {
			action = "Updating"
		}
		if verbose {
			fmt.Printf("%s Droid CLI...\n", action)
		}
		resp, err := execCommand(sandbox, `bash -lc "curl -fsSL https://app.factory.ai/cli | sh"`, nil)
		if err != nil || resp.ExitCode != 0 {
			return errors.New("failed to install droid cli in computer")
		}
		if verbose {
			fmt.Println("Droid CLI installed")
		}
	}
	touchAgentMarker(sandbox, "droid")
	return nil
}

// EnsureAgentInstalled installs the requested agent if missing or stale.
// If forceUpdate is true, always reinstalls to get the latest version.
// Otherwise, uses TTL-based caching (24h) to avoid unnecessary reinstalls.
func EnsureAgentInstalled(sandbox RemoteComputer, agent Agent, verbose bool, forceUpdate bool) error {
	if agent == AgentShell {
		return nil
	}

	// Check if we can skip installation (marker is fresh and not forcing update)
	if !forceUpdate && isAgentInstallFresh(sandbox, agent.String()) {
		if verbose {
			fmt.Printf("%s already installed (checked within 24h)\n", agent)
		}
		return nil
	}

	// Determine if this is an update (for messaging)
	needsUpdate := forceUpdate && isAgentInstallFresh(sandbox, agent.String())
	if needsUpdate && verbose {
		fmt.Printf("Checking for %s updates...\n", agent)
	}

	switch agent {
	case AgentClaude:
		return installClaude(sandbox, verbose, forceUpdate)
	case AgentCodex:
		return installCodex(sandbox, verbose, forceUpdate)
	case AgentOpenCode:
		return installOpenCode(sandbox, verbose, forceUpdate)
	case AgentAmp:
		return installAmp(sandbox, verbose, forceUpdate)
	case AgentGemini:
		return installGemini(sandbox, verbose, forceUpdate)
	case AgentDroid:
		return installDroid(sandbox, verbose, forceUpdate)
	default:
		return nil
	}
}

// UpdateAgent forces a reinstall of the agent to get the latest version.
func UpdateAgent(sandbox RemoteComputer, agent Agent, verbose bool) error {
	return EnsureAgentInstalled(sandbox, agent, verbose, true)
}

// UpdateAllAgents updates all supported agents to their latest versions.
func UpdateAllAgents(sandbox RemoteComputer, verbose bool) error {
	agents := []Agent{AgentClaude, AgentCodex, AgentOpenCode, AgentAmp, AgentGemini, AgentDroid}
	for _, agent := range agents {
		if err := UpdateAgent(sandbox, agent, verbose); err != nil {
			if verbose {
				fmt.Printf("Warning: failed to update %s: %v\n", agent, err)
			}
			// Continue with other agents
		}
	}
	return nil
}

type agentInteractiveRunner interface {
	RunAgentInteractive(cfg AgentConfig) (int, error)
}

// RunAgentInteractive runs the agent in an interactive session.
func RunAgentInteractive(sandbox RemoteComputer, cfg AgentConfig) (int, error) {
	if runner, ok := sandbox.(agentInteractiveRunner); ok {
		return runner.RunAgentInteractive(cfg)
	}
	return runAgentInteractiveGeneric(sandbox, cfg)
}

func runAgentInteractiveGeneric(sandbox RemoteComputer, cfg AgentConfig) (int, error) {
	if sandbox == nil {
		return 1, errors.New("computer is nil")
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
	return sandbox.ExecInteractive(context.Background(), cmdLine, os.Stdin, os.Stdout, os.Stderr, nil)
}

// RunAgentCommand executes a non-interactive command for an agent.
func RunAgentCommand(sandbox RemoteComputer, cfg AgentConfig, args []string) (int32, string, error) {
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
	resolved := resolveAgentCommandPath(sandbox, command)
	allArgs := strings.Join(args, " ")
	cmdLine := resolved
	if allArgs != "" {
		cmdLine = fmt.Sprintf("%s %s", resolved, allArgs)
	}
	envAssignments := buildEnvAssignmentsLocal(cfg.Env)
	if envAssignments != "" {
		cmdLine = fmt.Sprintf("%s %s", envAssignments, cmdLine)
	}
	resp, err := execCommand(sandbox, cmdLine, &ExecOptions{Cwd: cfg.WorkspacePath})
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
