package sandbox

import (
	"fmt"
	"path"
	"strings"
)

// CredentialsConfig configures shared credentials.
type CredentialsConfig struct {
	Mode             string
	Agent            Agent
	SettingsSyncMode string // "auto" (use global config), "force" (always sync), "skip" (never sync)
}

func getSandboxHomeDir(sb RemoteSandbox) string {
	resp, err := execCommand(sb, `sh -lc "USER_NAME=$(id -un 2>/dev/null || echo daytona); HOME_DIR=$(getent passwd \"$USER_NAME\" 2>/dev/null | cut -d: -f6 || true); if [ -z \"$HOME_DIR\" ]; then HOME_DIR=/home/$USER_NAME; fi; printf \"%s\" \"$HOME_DIR\""`, nil)
	if err == nil && resp != nil {
		stdout := strings.TrimSpace(resp.Stdout)
		if stdout != "" {
			return stdout
		}
	}
	return "/home/daytona"
}

func persistHomeDir() string {
	return path.Join(persistMountPath, "home")
}

func ensurePersistentDir(sb RemoteSandbox, target, persist string) error {
	if _, err := execCommand(sb, SafeCommands.MkdirP(persist), nil); err != nil {
		return err
	}
	cleanup := fmt.Sprintf("if [ -e %s ] && [ ! -L %s ]; then rm -rf %s; fi", ShellQuote(target), ShellQuote(target), ShellQuote(target))
	_, _ = execCommand(sb, cleanup, nil)
	_, err := execCommand(sb, SafeCommands.LnForce(persist, target), nil)
	return err
}

func ensurePersistentFile(sb RemoteSandbox, target, persist string) error {
	if _, err := execCommand(sb, SafeCommands.MkdirParent(persist), nil); err != nil {
		return err
	}
	if _, err := execCommand(sb, SafeCommands.Touch(persist), nil); err != nil {
		return err
	}
	cleanup := fmt.Sprintf("if [ -e %s ] && [ ! -L %s ]; then rm -f %s; fi", ShellQuote(target), ShellQuote(target), ShellQuote(target))
	_, _ = execCommand(sb, cleanup, nil)
	_, err := execCommand(sb, SafeCommands.LnForce(persist, target), nil)
	return err
}

func ensureNpmConfig(sb RemoteSandbox, homeDir string) {
	npmrc := path.Join(homeDir, ".npmrc")
	prefix := path.Join(homeDir, ".local")
	cache := path.Join(homeDir, ".npm")
	script := fmt.Sprintf("cfg=%s; touch \"$cfg\"; grep -q '^prefix=' \"$cfg\" || echo %s >> \"$cfg\"; grep -q '^cache=' \"$cfg\" || echo %s >> \"$cfg\"",
		ShellQuote(npmrc),
		ShellQuote("prefix="+prefix),
		ShellQuote("cache="+cache),
	)
	_, _ = execCommand(sb, fmt.Sprintf("bash -lc %s", ShellQuote(script)), nil)
}

func ensurePersistentHome(sb RemoteSandbox, homeDir string, verbose bool) {
	if persistMountPath == "" {
		return
	}
	if _, err := execCommand(sb, SafeCommands.MkdirP(persistMountPath), nil); err != nil {
		if verbose {
			fmt.Printf("Warning: persistence root unavailable: %v\n", err)
		}
		return
	}
	persistHome := persistHomeDir()
	if _, err := execCommand(sb, SafeCommands.MkdirP(persistHome), nil); err != nil {
		if verbose {
			fmt.Printf("Warning: persistence home unavailable: %v\n", err)
		}
		return
	}

	persistDirs := []string{
		".config",
		".local",
		".npm",
		".claude",
		".codex",
		".gemini",
		".amp",
		".factory",
	}
	for _, rel := range persistDirs {
		target := path.Join(homeDir, rel)
		persist := path.Join(persistHome, rel)
		if err := ensurePersistentDir(sb, target, persist); err != nil && verbose {
			fmt.Printf("Warning: persistence setup failed for %s: %v\n", rel, err)
		}
	}

	persistFiles := []string{".npmrc"}
	for _, rel := range persistFiles {
		target := path.Join(homeDir, rel)
		persist := path.Join(persistHome, rel)
		if err := ensurePersistentFile(sb, target, persist); err != nil && verbose {
			fmt.Printf("Warning: persistence setup failed for %s: %v\n", rel, err)
		}
	}

	ensureNpmConfig(sb, homeDir)
}

func ensureCredentialDirs(sb RemoteSandbox) (string, error) {
	homeDir := getSandboxHomeDir(sb)
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
		if _, err := execCommand(sb, SafeCommands.MkdirP(fmt.Sprintf("%s/%s", homeDir, dir)), nil); err != nil {
			LogDebug("failed to create credential directory", "dir", dir, "error", err)
			lastErr = err
		}
	}
	if lastErr != nil {
		LogDebug("some credential directories may not have been created", "lastError", lastErr)
	}
	return homeDir, nil
}

func prepareClaudeHome(sb RemoteSandbox, homeDir string) {
	claudeHome := fmt.Sprintf("%s/.claude", homeDir)
	_, _ = execCommand(sb, SafeCommands.MkdirP(claudeHome), nil)
	// Symlink cache and debug to /tmp for performance (these are ephemeral)
	_, _ = execCommand(sb, SafeCommands.MkdirP("/tmp/amux-claude-cache"), nil)
	_, _ = execCommand(sb, SafeCommands.MkdirP("/tmp/amux-claude-debug"), nil)
	_, _ = execCommand(sb, SafeCommands.LnForce("/tmp/amux-claude-cache", fmt.Sprintf("%s/cache", claudeHome)), nil)
	_, _ = execCommand(sb, SafeCommands.LnForce("/tmp/amux-claude-debug", fmt.Sprintf("%s/debug", claudeHome)), nil)
}

func prepareCodexHome(sb RemoteSandbox, homeDir string) {
	codexHome := fmt.Sprintf("%s/.codex", homeDir)
	codexConfigHome := fmt.Sprintf("%s/.config/codex", homeDir)
	_, _ = execCommand(sb, SafeCommands.MkdirP(codexHome), nil)
	_, _ = execCommand(sb, SafeCommands.MkdirP(codexConfigHome), nil)
	// Ensure file-based credential store for codex
	ensureFileStore := func(path string) string {
		return fmt.Sprintf(`if [ -f %s ]; then if grep -q '^cli_auth_credentials_store' %s; then sed -i 's/^cli_auth_credentials_store.*/cli_auth_credentials_store = "file"/' %s; else echo 'cli_auth_credentials_store = "file"' >> %s; fi; else mkdir -p $(dirname %s); echo 'cli_auth_credentials_store = "file"' > %s; fi`, path, path, path, path, path, path)
	}
	_, _ = execCommand(sb, ensureFileStore(fmt.Sprintf("%s/config.toml", codexConfigHome)), nil)
}

func prepareOpenCodeHome(sb RemoteSandbox, homeDir string) {
	dataDir := fmt.Sprintf("%s/.local/share/opencode", homeDir)
	configDir := fmt.Sprintf("%s/.config/opencode", homeDir)
	_, _ = execCommand(sb, SafeCommands.MkdirP(dataDir), nil)
	_, _ = execCommand(sb, SafeCommands.MkdirP(configDir), nil)
}

func prepareAmpHome(sb RemoteSandbox, homeDir string) {
	ampConfig := fmt.Sprintf("%s/.config/amp", homeDir)
	ampData := fmt.Sprintf("%s/.local/share/amp", homeDir)
	_, _ = execCommand(sb, SafeCommands.MkdirP(ampConfig), nil)
	_, _ = execCommand(sb, SafeCommands.MkdirP(ampData), nil)
}

func prepareGeminiHome(sb RemoteSandbox, homeDir string) {
	geminiHome := fmt.Sprintf("%s/.gemini", homeDir)
	_, _ = execCommand(sb, SafeCommands.MkdirP(geminiHome), nil)
}

func prepareFactoryHome(sb RemoteSandbox, homeDir string) {
	factoryHome := fmt.Sprintf("%s/.factory", homeDir)
	_, _ = execCommand(sb, SafeCommands.MkdirP(factoryHome), nil)
}

func prepareGhHome(sb RemoteSandbox, homeDir string) {
	ghConfig := fmt.Sprintf("%s/.config/gh", homeDir)
	_, _ = execCommand(sb, SafeCommands.MkdirP(ghConfig), nil)
}

// SetupCredentials prepares credential directories on the sandbox.
// Credentials are stored in the home directory (symlinked to the persistent volume).
func SetupCredentials(sb RemoteSandbox, cfg CredentialsConfig, verbose bool) error {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode == "" {
		mode = "auto"
	}
	switch mode {
	case "sandbox", "none", "auto":
	default:
		return fmt.Errorf("unsupported credentials mode: %s", mode)
	}
	if mode == "none" {
		if verbose {
			fmt.Println("Credentials mode: none")
		}
		return nil
	}
	if verbose {
		if mode == "auto" {
			fmt.Println("Credentials mode: sandbox (auto)")
		} else {
			fmt.Printf("Credentials mode: %s\n", mode)
		}
	}
	homeDir := getSandboxHomeDir(sb)
	ensurePersistentHome(sb, homeDir, verbose)

	resolvedHome, err := ensureCredentialDirs(sb)
	if err != nil {
		return err
	}
	prepareClaudeHome(sb, resolvedHome)
	prepareCodexHome(sb, resolvedHome)
	prepareOpenCodeHome(sb, resolvedHome)
	prepareAmpHome(sb, resolvedHome)
	prepareGeminiHome(sb, resolvedHome)
	prepareFactoryHome(sb, resolvedHome)
	prepareGhHome(sb, resolvedHome)

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
		if err := SyncSettingsToVolume(sb, amuxCfg.SettingsSync, verbose); err != nil {
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
// on the sandbox.
type AgentCredentialStatus struct {
	Agent         Agent
	HasCredential bool
	CredentialAge string // e.g., "2 days ago" or empty if unknown
}

// CheckAgentCredentials checks if credentials exist for an agent on the sandbox.
func CheckAgentCredentials(sb RemoteSandbox, agent Agent) AgentCredentialStatus {
	status := AgentCredentialStatus{Agent: agent, HasCredential: false}
	homeDir := getSandboxHomeDir(sb)

	switch agent {
	case AgentClaude:
		resp, err := execCommand(sb, fmt.Sprintf(
			"test -f %s/.claude/.credentials.json && echo exists",
			homeDir,
		), nil)
		if err == nil && resp != nil && resp.ExitCode == 0 {
			status.HasCredential = true
		}

	case AgentCodex:
		resp, err := execCommand(sb, fmt.Sprintf(
			"test -f %s/.codex/auth.json && echo exists",
			homeDir,
		), nil)
		if err == nil && resp != nil && resp.ExitCode == 0 {
			status.HasCredential = true
		}

	case AgentOpenCode:
		resp, err := execCommand(sb, fmt.Sprintf(
			"test -f %s/.local/share/opencode/auth.json && echo exists",
			homeDir,
		), nil)
		if err == nil && resp != nil && resp.ExitCode == 0 {
			status.HasCredential = true
		}

	case AgentAmp:
		resp, err := execCommand(sb, fmt.Sprintf(
			"test -f %s/.config/amp/secrets.json && echo exists",
			homeDir,
		), nil)
		if err == nil && resp != nil && resp.ExitCode == 0 {
			status.HasCredential = true
		}

	case AgentGemini:
		resp, err := execCommand(sb, fmt.Sprintf(
			"test -f %s/.gemini/oauth_creds.json && echo exists",
			homeDir,
		), nil)
		if err == nil && resp != nil && resp.ExitCode == 0 {
			status.HasCredential = true
		}

	case AgentDroid:
		resp, err := execCommand(sb, fmt.Sprintf(
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
func CheckAllAgentCredentials(sb RemoteSandbox) []AgentCredentialStatus {
	agents := []Agent{AgentClaude, AgentCodex, AgentOpenCode, AgentAmp, AgentGemini, AgentDroid}
	results := make([]AgentCredentialStatus, 0, len(agents))

	for _, agent := range agents {
		results = append(results, CheckAgentCredentials(sb, agent))
	}

	return results
}

// HasGitHubCredentials checks if GitHub CLI is authenticated.
func HasGitHubCredentials(sb RemoteSandbox) bool {
	homeDir := getSandboxHomeDir(sb)
	resp, err := execCommand(sb, fmt.Sprintf(
		"test -f %s/.config/gh/hosts.yml && echo exists",
		homeDir,
	), nil)
	return err == nil && resp != nil && resp.ExitCode == 0
}
