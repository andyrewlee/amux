package sandbox

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	agentInstallBasePath = "/amux/.installed"
	agentInstallTTL      = 24 * time.Hour // Re-check for updates after 24 hours
)

func agentInstallMarker(agent string) string {
	return fmt.Sprintf("%s/%s", agentInstallBasePath, agent)
}

// isAgentInstallFresh checks if the agent was installed recently (within TTL).
// Returns true if the marker exists and is fresh, false if missing or stale.
func isAgentInstallFresh(computer RemoteSandbox, agent string) bool {
	marker := agentInstallMarker(agent)
	// Check if marker exists and get its age in seconds
	resp, err := execCommand(computer, fmt.Sprintf(
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
func touchAgentMarker(computer RemoteSandbox, agent string) {
	marker := agentInstallMarker(agent)
	_, _ = execCommand(computer, SafeCommands.MkdirP(agentInstallBasePath), nil)
	_, _ = execCommand(computer, SafeCommands.Touch(marker), nil)
}

// isAgentInstalled checks if the agent has been installed (marker file exists).
// This is a simpler check than isAgentInstallFresh - it doesn't check the TTL.
// Used for agents that auto-update themselves.
func isAgentInstalled(computer RemoteSandbox, agent string) bool {
	marker := agentInstallMarker(agent)
	resp, err := execCommand(computer, "test -f "+marker, nil)
	return err == nil && resp != nil && resp.ExitCode == 0
}

func installClaude(computer RemoteSandbox, verbose, forceUpdate bool) error {
	if verbose {
		fmt.Fprintln(sandboxStdout, "Installing Claude Code...")
	}
	// Check for native installation first (~/.local/bin/claude), then fall back to PATH
	home := getHomeDir(computer)
	nativeCheck := fmt.Sprintf("test -x %s/.local/bin/claude", home)
	resp, _ := execCommand(computer, nativeCheck, nil)
	nativeInstalled := resp != nil && resp.ExitCode == 0

	pathResp, _ := execCommand(computer, "which claude", nil)
	pathInstalled := pathResp != nil && pathResp.ExitCode == 0

	alreadyInstalled := nativeInstalled || pathInstalled
	if alreadyInstalled && !forceUpdate {
		if verbose {
			fmt.Fprintln(sandboxStdout, "Claude Code already installed")
		}
	} else {
		action := "Installing"
		if alreadyInstalled {
			action = "Updating"
		}
		if verbose {
			fmt.Fprintf(sandboxStdout, "%s Claude Code...\n", action)
		}
		// Use native installer (recommended by Anthropic)
		// Installs to ~/.local/bin/claude with binary at ~/.local/share/claude/versions/{version}
		resp, err := execCommand(computer, `bash -lc "curl -fsSL https://claude.ai/install.sh | bash"`, nil)
		if err != nil || resp.ExitCode != 0 {
			return errors.New("failed to install claude code via native installer")
		}
		if verbose {
			fmt.Fprintln(sandboxStdout, "Claude Code installed")
		}
	}
	touchAgentMarker(computer, "claude")
	return nil
}

func installCodex(computer RemoteSandbox, verbose, forceUpdate bool) error {
	if verbose {
		fmt.Fprintln(sandboxStdout, "Installing Codex CLI...")
	}
	resp, _ := execCommand(computer, "which codex", nil)
	alreadyInstalled := resp != nil && resp.ExitCode == 0
	if alreadyInstalled && !forceUpdate {
		if verbose {
			fmt.Fprintln(sandboxStdout, "Codex CLI already installed")
		}
	} else {
		action := "Installing"
		if alreadyInstalled {
			action = "Updating"
		}
		if verbose {
			fmt.Fprintf(sandboxStdout, "%s Codex CLI...\n", action)
		}
		resp, err := execCommand(computer, "npm install -g @openai/codex@latest", nil)
		if err != nil || resp.ExitCode != 0 {
			return errors.New("failed to install codex cli in sandbox")
		}
		if verbose {
			fmt.Fprintln(sandboxStdout, "Codex CLI installed")
		}
	}
	touchAgentMarker(computer, "codex")
	return nil
}

func installOpenCode(computer RemoteSandbox, verbose, forceUpdate bool) error {
	if verbose {
		fmt.Fprintln(sandboxStdout, "Installing OpenCode CLI...")
	}
	resp, _ := execCommand(computer, "which opencode", nil)
	alreadyInstalled := resp != nil && resp.ExitCode == 0
	if alreadyInstalled && !forceUpdate {
		if verbose {
			fmt.Fprintln(sandboxStdout, "OpenCode CLI already installed")
		}
	} else {
		action := "Installing"
		if alreadyInstalled {
			action = "Updating"
		}
		if verbose {
			fmt.Fprintf(sandboxStdout, "%s OpenCode CLI...\n", action)
		}
		resp, err := execCommand(computer, `bash -lc "curl -fsSL https://opencode.ai/install | bash"`, nil)
		if err != nil || resp.ExitCode != 0 {
			if verbose {
				fmt.Fprintln(sandboxStdout, "OpenCode install script failed, trying npm...")
			}
			resp, err = execCommand(computer, "npm install -g opencode-ai@latest", nil)
			if err != nil || resp.ExitCode != 0 {
				return errors.New("failed to install opencode cli in sandbox")
			}
		}
		if verbose {
			fmt.Fprintln(sandboxStdout, "OpenCode CLI installed")
		}
	}
	touchAgentMarker(computer, "opencode")
	return nil
}

func installAmp(computer RemoteSandbox, verbose, forceUpdate bool) error {
	if verbose {
		fmt.Fprintln(sandboxStdout, "Installing Amp CLI...")
	}
	home := getHomeDir(computer)
	ampBin := home + "/.amp/bin/amp"
	resp, _ := execCommand(computer, fmt.Sprintf("sh -lc \"command -v amp >/dev/null 2>&1 || test -x %s\"", quoteForShell(ampBin)), nil)
	alreadyInstalled := resp != nil && resp.ExitCode == 0
	if alreadyInstalled && !forceUpdate {
		if verbose {
			fmt.Fprintln(sandboxStdout, "Amp CLI already installed")
		}
	} else {
		action := "Installing"
		if alreadyInstalled {
			action = "Updating"
		}
		if verbose {
			fmt.Fprintf(sandboxStdout, "%s Amp CLI...\n", action)
		}
		resp, err := execCommand(computer, `bash -lc "curl -fsSL https://ampcode.com/install.sh | bash"`, nil)
		if err != nil || resp.ExitCode != 0 {
			if verbose {
				fmt.Fprintln(sandboxStdout, "Amp install script failed, trying npm...")
			}
			resp, err = execCommand(computer, "npm install -g @sourcegraph/amp@latest", nil)
			if err != nil || resp.ExitCode != 0 {
				return errors.New("failed to install amp cli in sandbox")
			}
		}
		if verbose {
			fmt.Fprintln(sandboxStdout, "Amp CLI installed")
		}
	}
	touchAgentMarker(computer, "amp")
	return nil
}

func installGemini(computer RemoteSandbox, verbose, forceUpdate bool) error {
	if verbose {
		fmt.Fprintln(sandboxStdout, "Installing Gemini CLI...")
	}
	resp, _ := execCommand(computer, "which gemini", nil)
	alreadyInstalled := resp != nil && resp.ExitCode == 0
	if alreadyInstalled && !forceUpdate {
		if verbose {
			fmt.Fprintln(sandboxStdout, "Gemini CLI already installed")
		}
	} else {
		action := "Installing"
		if alreadyInstalled {
			action = "Updating"
		}
		if verbose {
			fmt.Fprintf(sandboxStdout, "%s Gemini CLI...\n", action)
		}
		resp, err := execCommand(computer, "npm install -g @google/gemini-cli@latest", nil)
		if err != nil || resp.ExitCode != 0 {
			return errors.New("failed to install gemini cli in sandbox")
		}
		if verbose {
			fmt.Fprintln(sandboxStdout, "Gemini CLI installed")
		}
	}
	touchAgentMarker(computer, "gemini")
	return nil
}

func installDroid(computer RemoteSandbox, verbose, forceUpdate bool) error {
	if verbose {
		fmt.Fprintln(sandboxStdout, "Installing Droid CLI...")
	}
	resp, _ := execCommand(computer, "which droid", nil)
	alreadyInstalled := resp != nil && resp.ExitCode == 0
	if alreadyInstalled && !forceUpdate {
		if verbose {
			fmt.Fprintln(sandboxStdout, "Droid CLI already installed")
		}
	} else {
		action := "Installing"
		if alreadyInstalled {
			action = "Updating"
		}
		if verbose {
			fmt.Fprintf(sandboxStdout, "%s Droid CLI...\n", action)
		}
		resp, err := execCommand(computer, `bash -lc "curl -fsSL https://app.factory.ai/cli | sh"`, nil)
		if err != nil || resp.ExitCode != 0 {
			return errors.New("failed to install droid cli in sandbox")
		}
		if verbose {
			fmt.Fprintln(sandboxStdout, "Droid CLI installed")
		}
	}
	touchAgentMarker(computer, "droid")
	return nil
}

// EnsureAgentInstalled installs the requested agent if missing or stale.
// If forceUpdate is true, always reinstalls to get the latest version.
// For agents that auto-update (Claude, OpenCode, Amp, Gemini, Droid): just check if installed.
// For agents that don't auto-update (Codex): use TTL-based caching (24h).
func EnsureAgentInstalled(computer RemoteSandbox, agent Agent, verbose, forceUpdate bool) error {
	if agent == AgentShell {
		return nil
	}

	// Check if we can skip installation based on agent's auto-update capability
	if !forceUpdate {
		if AgentAutoUpdates[agent] {
			// Agent handles its own updates - just check if installed
			if isAgentInstalled(computer, agent.String()) {
				if verbose {
					fmt.Fprintf(sandboxStdout, "%s already installed (auto-updates itself)\n", agent)
				}
				return nil
			}
		} else {
			// Agent doesn't auto-update - use TTL-based checking
			if isAgentInstallFresh(computer, agent.String()) {
				if verbose {
					fmt.Fprintf(sandboxStdout, "%s already installed (checked within 24h)\n", agent)
				}
				return nil
			}
		}
	}

	// Determine if this is an update (for messaging)
	needsUpdate := forceUpdate && isAgentInstalled(computer, agent.String())
	if needsUpdate && verbose {
		fmt.Fprintf(sandboxStdout, "Checking for %s updates...\n", agent)
	}

	switch agent {
	case AgentClaude:
		return installClaude(computer, verbose, forceUpdate)
	case AgentCodex:
		return installCodex(computer, verbose, forceUpdate)
	case AgentOpenCode:
		return installOpenCode(computer, verbose, forceUpdate)
	case AgentAmp:
		return installAmp(computer, verbose, forceUpdate)
	case AgentGemini:
		return installGemini(computer, verbose, forceUpdate)
	case AgentDroid:
		return installDroid(computer, verbose, forceUpdate)
	default:
		return nil
	}
}

// UpdateAgent forces a reinstall of the agent to get the latest version.
func UpdateAgent(computer RemoteSandbox, agent Agent, verbose bool) error {
	return EnsureAgentInstalled(computer, agent, verbose, true)
}

// UpdateAllAgents updates all supported agents to their latest versions.
func UpdateAllAgents(computer RemoteSandbox, verbose bool) error {
	agents := []Agent{AgentClaude, AgentCodex, AgentOpenCode, AgentAmp, AgentGemini, AgentDroid}
	for _, agent := range agents {
		if err := UpdateAgent(computer, agent, verbose); err != nil {
			if verbose {
				fmt.Fprintf(sandboxStdout, "Warning: failed to update %s: %v\n", agent, err)
			}
			// Continue with other agents
		}
	}
	return nil
}
