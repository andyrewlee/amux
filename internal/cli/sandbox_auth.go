package cli

import (
	"fmt"

	"github.com/andyrewlee/amux/internal/sandbox"
)

// checkNeedsLogin determines if an agent needs login based on stored credentials
func checkNeedsLogin(sb sandbox.RemoteSandbox, agent sandbox.Agent, envMap map[string]string) bool {
	// Check if credentials already exist on the sandbox
	credStatus := sandbox.CheckAgentCredentials(sb, agent)
	if credStatus.HasCredential {
		return false
	}

	// Check if API key is provided via environment
	switch agent {
	case sandbox.AgentClaude:
		if envMap["ANTHROPIC_API_KEY"] != "" || envMap["CLAUDE_API_KEY"] != "" || envMap["ANTHROPIC_AUTH_TOKEN"] != "" {
			return false
		}
	case sandbox.AgentCodex:
		if envMap["OPENAI_API_KEY"] != "" {
			return false
		}
	case sandbox.AgentGemini:
		if envMap["GEMINI_API_KEY"] != "" || envMap["GOOGLE_API_KEY"] != "" || envMap["GOOGLE_APPLICATION_CREDENTIALS"] != "" {
			return false
		}
	case sandbox.AgentDroid:
		if envMap["FACTORY_API_KEY"] != "" {
			return false
		}
	case sandbox.AgentAmp:
		if envMap["AMP_API_KEY"] != "" {
			return false
		}
	}

	// Agents that need explicit login
	switch agent {
	case sandbox.AgentCodex, sandbox.AgentOpenCode, sandbox.AgentAmp:
		return true
	}

	return false
}

// handleAgentLogin runs the login flow for agents that need it
func handleAgentLogin(sb sandbox.RemoteSandbox, agent sandbox.Agent, workspacePath string, envMap map[string]string) (int, error) {
	fmt.Fprintf(cliStdout, "\n%s requires authentication (first run)\n", agent)
	fmt.Fprintln(cliStdout, "Credentials will persist for future sessions.")
	fmt.Fprintln(cliStdout)

	var loginArgs []string
	switch agent {
	case sandbox.AgentCodex:
		loginArgs = []string{"login"}
		if getenvFallback("AMUX_CODEX_DEVICE_AUTH") != "0" {
			loginArgs = append(loginArgs, "--device-auth")
		}
	case sandbox.AgentOpenCode:
		loginArgs = []string{"auth", "login"}
	case sandbox.AgentAmp:
		loginArgs = []string{"login"}
	default:
		return 0, nil
	}

	raw := false
	exitCode, err := sandbox.RunAgentInteractive(sb, sandbox.AgentConfig{
		Agent:         agent,
		WorkspacePath: workspacePath,
		Args:          loginArgs,
		Env:           envMap,
		RawMode:       &raw,
	})
	if err != nil {
		return 1, err
	}

	if exitCode == 0 {
		fmt.Fprintln(cliStdout, "\n✓ Authentication complete")
	}

	return exitCode, nil
}

// handleAgentExit handles post-exit tasks (workspace download, exit tips)
func handleAgentExit(sb sandbox.RemoteSandbox, agent sandbox.Agent, exitCode int, syncEnabled bool, cwd string) error {
	// Show tips for exit code 127 (command not found)
	if exitCode == 127 {
		showAgentTips(agent)
	}

	// Show exit code if non-zero
	if exitCode != 0 && exitCode != 127 {
		fmt.Fprintf(cliStdout, "\nExited with code %d\n", exitCode)
	}

	// Sync workspace back
	if syncEnabled {
		worktreeID := sandbox.ComputeWorktreeID(cwd)
		if Verbose {
			fmt.Fprintln(cliStdout, "\nSyncing changes...")
			if err := sandbox.DownloadWorkspace(sb, sandbox.SyncOptions{Cwd: cwd, WorktreeID: worktreeID}, Verbose); err != nil {
				return err
			}
			fmt.Fprintln(cliStdout, "Done")
		} else {
			spinner := NewSpinner("Syncing changes")
			spinner.Start()
			if err := sandbox.DownloadWorkspace(sb, sandbox.SyncOptions{Cwd: cwd, WorktreeID: worktreeID}, false); err != nil {
				spinner.StopWithMessage("✗ Sync failed")
				return err
			}
			spinner.StopWithMessage("✓ Changes synced")
		}
	}

	if exitCode != 0 {
		return exitError{code: exitCode}
	}
	return nil
}

// showAgentTips displays helpful tips when an agent fails to start
func showAgentTips(agent sandbox.Agent) {
	fmt.Fprintln(cliStdout)
	switch agent {
	case sandbox.AgentClaude:
		fmt.Fprintln(cliStdout, "Tip: Claude requires authentication. Run `claude` and complete login,")
		fmt.Fprintln(cliStdout, "     or pass --env ANTHROPIC_API_KEY=...")
	case sandbox.AgentCodex:
		fmt.Fprintln(cliStdout, "Tip: Codex requires OpenAI credentials. Login will start automatically,")
		fmt.Fprintln(cliStdout, "     or pass --env OPENAI_API_KEY=...")
	case sandbox.AgentOpenCode:
		fmt.Fprintln(cliStdout, "Tip: OpenCode requires authentication. Login will start automatically,")
		fmt.Fprintln(cliStdout, "     or pass API keys via --env")
	case sandbox.AgentAmp:
		fmt.Fprintln(cliStdout, "Tip: Amp requires authentication. Login will start automatically,")
		fmt.Fprintln(cliStdout, "     or pass --env AMP_API_KEY=...")
	case sandbox.AgentGemini:
		fmt.Fprintln(cliStdout, "Tip: Gemini requires authentication. Choose a login method in the CLI,")
		fmt.Fprintln(cliStdout, "     or pass --env GEMINI_API_KEY=...")
	case sandbox.AgentDroid:
		fmt.Fprintln(cliStdout, "Tip: Droid requires authentication. Run `/login` inside Droid,")
		fmt.Fprintln(cliStdout, "     or pass --env FACTORY_API_KEY=...")
	}
}
