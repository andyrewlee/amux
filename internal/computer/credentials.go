package computer

import (
	"fmt"
	"strings"
)

// CredentialsConfig configures shared credentials.
type CredentialsConfig struct {
	Mode             string
	Agent            Agent
	SettingsSyncMode string // "auto" (use global config), "force" (always sync), "skip" (never sync)
}

func getSandboxHomeDir(sandbox RemoteComputer) string {
	resp, err := execCommand(sandbox, `sh -lc "USER_NAME=$(id -un 2>/dev/null || echo daytona); HOME_DIR=$(getent passwd \"$USER_NAME\" 2>/dev/null | cut -d: -f6 || true); if [ -z \"$HOME_DIR\" ]; then HOME_DIR=/home/$USER_NAME; fi; printf \"%s\" \"$HOME_DIR\""`, nil)
	if err == nil && resp != nil {
		stdout := strings.TrimSpace(resp.Stdout)
		if stdout != "" {
			return stdout
		}
	}
	return "/home/daytona"
}

func ensureCredentialDirs(sandbox RemoteComputer) (string, error) {
	homeDir := getSandboxHomeDir(sandbox)
	// Create credential directories directly in home
	dirs := []string{
		".claude",
		".codex",
		".config/codex",
		".config/opencode",
		".local/share/opencode",
		".config/amp",
		".local/share/amp",
		".gemini",
		".factory",
		".config/gh",
	}
	var lastErr error
	for _, dir := range dirs {
		if _, err := execCommand(sandbox, SafeCommands.MkdirP(fmt.Sprintf("%s/%s", homeDir, dir)), nil); err != nil {
			LogDebug("failed to create credential directory", "dir", dir, "error", err)
			lastErr = err
		}
	}
	if lastErr != nil {
		LogDebug("some credential directories may not have been created", "lastError", lastErr)
	}
	return homeDir, nil
}

func prepareClaudeHome(sandbox RemoteComputer, homeDir string) {
	claudeHome := fmt.Sprintf("%s/.claude", homeDir)
	_, _ = execCommand(sandbox, SafeCommands.MkdirP(claudeHome), nil)
	// Symlink cache and debug to /tmp for performance (these are ephemeral)
	_, _ = execCommand(sandbox, SafeCommands.MkdirP("/tmp/amux-claude-cache"), nil)
	_, _ = execCommand(sandbox, SafeCommands.MkdirP("/tmp/amux-claude-debug"), nil)
	_, _ = execCommand(sandbox, SafeCommands.LnForce("/tmp/amux-claude-cache", fmt.Sprintf("%s/cache", claudeHome)), nil)
	_, _ = execCommand(sandbox, SafeCommands.LnForce("/tmp/amux-claude-debug", fmt.Sprintf("%s/debug", claudeHome)), nil)
}

func prepareCodexHome(sandbox RemoteComputer, homeDir string) {
	codexHome := fmt.Sprintf("%s/.codex", homeDir)
	codexConfigHome := fmt.Sprintf("%s/.config/codex", homeDir)
	_, _ = execCommand(sandbox, SafeCommands.MkdirP(codexHome), nil)
	_, _ = execCommand(sandbox, SafeCommands.MkdirP(codexConfigHome), nil)
	// Ensure file-based credential store for codex
	ensureFileStore := func(path string) string {
		return fmt.Sprintf(`if [ -f %s ]; then if grep -q '^cli_auth_credentials_store' %s; then sed -i 's/^cli_auth_credentials_store.*/cli_auth_credentials_store = "file"/' %s; else echo 'cli_auth_credentials_store = "file"' >> %s; fi; else mkdir -p $(dirname %s); echo 'cli_auth_credentials_store = "file"' > %s; fi`, path, path, path, path, path, path)
	}
	_, _ = execCommand(sandbox, ensureFileStore(fmt.Sprintf("%s/config.toml", codexConfigHome)), nil)
}

func prepareOpenCodeHome(sandbox RemoteComputer, homeDir string) {
	dataDir := fmt.Sprintf("%s/.local/share/opencode", homeDir)
	configDir := fmt.Sprintf("%s/.config/opencode", homeDir)
	_, _ = execCommand(sandbox, SafeCommands.MkdirP(dataDir), nil)
	_, _ = execCommand(sandbox, SafeCommands.MkdirP(configDir), nil)
}

func prepareAmpHome(sandbox RemoteComputer, homeDir string) {
	ampConfig := fmt.Sprintf("%s/.config/amp", homeDir)
	ampData := fmt.Sprintf("%s/.local/share/amp", homeDir)
	_, _ = execCommand(sandbox, SafeCommands.MkdirP(ampConfig), nil)
	_, _ = execCommand(sandbox, SafeCommands.MkdirP(ampData), nil)
}

func prepareGeminiHome(sandbox RemoteComputer, homeDir string) {
	geminiHome := fmt.Sprintf("%s/.gemini", homeDir)
	_, _ = execCommand(sandbox, SafeCommands.MkdirP(geminiHome), nil)
}

func prepareFactoryHome(sandbox RemoteComputer, homeDir string) {
	factoryHome := fmt.Sprintf("%s/.factory", homeDir)
	_, _ = execCommand(sandbox, SafeCommands.MkdirP(factoryHome), nil)
}

func prepareGhHome(sandbox RemoteComputer, homeDir string) {
	ghConfig := fmt.Sprintf("%s/.config/gh", homeDir)
	_, _ = execCommand(sandbox, SafeCommands.MkdirP(ghConfig), nil)
}

// SetupCredentials prepares credential directories on the computer.
// Credentials are stored directly in the home directory (e.g., ~/.claude/, ~/.codex/).
func SetupCredentials(sandbox RemoteComputer, cfg CredentialsConfig, verbose bool) error {
	if cfg.Mode != "computer" && cfg.Mode != "none" && cfg.Mode != "auto" {
		return fmt.Errorf("unsupported credentials mode: %s", cfg.Mode)
	}
	if cfg.Mode == "none" {
		if verbose {
			fmt.Println("Credentials mode: none")
		}
		return nil
	}
	if verbose {
		if cfg.Mode == "auto" {
			fmt.Println("Credentials mode: computer (auto)")
		} else {
			fmt.Printf("Credentials mode: %s\n", cfg.Mode)
		}
	}
	homeDir, err := ensureCredentialDirs(sandbox)
	if err != nil {
		return err
	}
	prepareClaudeHome(sandbox, homeDir)
	prepareCodexHome(sandbox, homeDir)
	prepareOpenCodeHome(sandbox, homeDir)
	prepareAmpHome(sandbox, homeDir)
	prepareGeminiHome(sandbox, homeDir)
	prepareFactoryHome(sandbox, homeDir)
	prepareGhHome(sandbox, homeDir)

	// Sync local settings based on mode and global config
	amuxCfg, _ := LoadConfig()
	shouldSync := false
	switch cfg.SettingsSyncMode {
	case "force":
		shouldSync = true
	case "skip":
		shouldSync = false
	default: // "auto" or empty - use global config
		shouldSync = amuxCfg.SettingsSync.Enabled
	}

	if shouldSync {
		if verbose {
			fmt.Println("Syncing local settings...")
		}
		if err := SyncSettingsToVolume(sandbox, amuxCfg.SettingsSync, verbose); err != nil {
			if verbose {
				fmt.Printf("Warning: settings sync failed: %v\n", err)
			}
			// Don't fail the whole setup for settings sync errors
		}
	}

	if verbose {
		fmt.Println("Credentials ready")
	}
	return nil
}

// AgentCredentialStatus represents whether an agent has credentials stored
type AgentCredentialStatus struct {
	Agent         Agent
	HasCredential bool
	CredentialAge string // e.g., "2 days ago" or empty if unknown
}

// CheckAgentCredentials checks if credentials exist for an agent on the computer
func CheckAgentCredentials(sandbox RemoteComputer, agent Agent) AgentCredentialStatus {
	status := AgentCredentialStatus{Agent: agent, HasCredential: false}
	homeDir := getSandboxHomeDir(sandbox)

	switch agent {
	case AgentClaude:
		resp, err := execCommand(sandbox, fmt.Sprintf(
			"test -f %s/.claude/.credentials.json && echo exists",
			homeDir,
		), nil)
		if err == nil && resp != nil && resp.ExitCode == 0 {
			status.HasCredential = true
		}

	case AgentCodex:
		resp, err := execCommand(sandbox, fmt.Sprintf(
			"test -f %s/.codex/auth.json && echo exists",
			homeDir,
		), nil)
		if err == nil && resp != nil && resp.ExitCode == 0 {
			status.HasCredential = true
		}

	case AgentOpenCode:
		resp, err := execCommand(sandbox, fmt.Sprintf(
			"test -f %s/.local/share/opencode/auth.json && echo exists",
			homeDir,
		), nil)
		if err == nil && resp != nil && resp.ExitCode == 0 {
			status.HasCredential = true
		}

	case AgentAmp:
		resp, err := execCommand(sandbox, fmt.Sprintf(
			"test -f %s/.config/amp/secrets.json && echo exists",
			homeDir,
		), nil)
		if err == nil && resp != nil && resp.ExitCode == 0 {
			status.HasCredential = true
		}

	case AgentGemini:
		resp, err := execCommand(sandbox, fmt.Sprintf(
			"test -f %s/.gemini/oauth_creds.json && echo exists",
			homeDir,
		), nil)
		if err == nil && resp != nil && resp.ExitCode == 0 {
			status.HasCredential = true
		}

	case AgentDroid:
		resp, err := execCommand(sandbox, fmt.Sprintf(
			"test -f %s/.factory/config.json && echo exists",
			homeDir,
		), nil)
		if err == nil && resp != nil && resp.ExitCode == 0 {
			status.HasCredential = true
		}
	}

	return status
}

// CheckAllAgentCredentials returns credential status for all agents.
func CheckAllAgentCredentials(sandbox RemoteComputer) []AgentCredentialStatus {
	agents := []Agent{AgentClaude, AgentCodex, AgentOpenCode, AgentAmp, AgentGemini, AgentDroid}
	results := make([]AgentCredentialStatus, 0, len(agents))

	for _, agent := range agents {
		results = append(results, CheckAgentCredentials(sandbox, agent))
	}

	return results
}

// HasGitHubCredentials checks if GitHub CLI is authenticated.
func HasGitHubCredentials(sandbox RemoteComputer) bool {
	homeDir := getSandboxHomeDir(sandbox)
	resp, err := execCommand(sandbox, fmt.Sprintf(
		"test -f %s/.config/gh/hosts.yml && echo exists",
		homeDir,
	), nil)
	return err == nil && resp != nil && resp.ExitCode == 0
}
