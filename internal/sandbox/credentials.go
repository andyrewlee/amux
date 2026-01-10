package sandbox

import (
	"fmt"

	"github.com/andyrewlee/amux/internal/daytona"
)

// CredentialsConfig configures shared credentials.
type CredentialsConfig struct {
	Mode  string
	Agent Agent
}

const (
	CredentialsVolumeName = "amux-credentials"
	CredentialsMountPath  = "/mnt/amux-credentials"
)

func waitForVolumeReady(client *daytona.Daytona, name string) (*daytona.Volume, error) {
	return client.Volume.WaitForReady(name, nil)
}

// GetCredentialsVolumeMount returns the mount spec for the shared credentials volume.
func GetCredentialsVolumeMount(client *daytona.Daytona) (daytona.VolumeMount, error) {
	volume, err := waitForVolumeReady(client, CredentialsVolumeName)
	if err != nil {
		return daytona.VolumeMount{}, err
	}
	return daytona.VolumeMount{VolumeID: volume.ID, MountPath: CredentialsMountPath}, nil
}

func getSandboxHomeDir(sandbox *daytona.Sandbox) string {
	resp, err := sandbox.Process.ExecuteCommand(`sh -lc "USER_NAME=$(id -un 2>/dev/null || echo daytona); HOME_DIR=$(getent passwd \"$USER_NAME\" 2>/dev/null | cut -d: -f6 || true); if [ -z \"$HOME_DIR\" ]; then HOME_DIR=/home/$USER_NAME; fi; printf \"%s\" \"$HOME_DIR\""`)
	if err == nil && resp != nil {
		if resp.Artifacts != nil && resp.Artifacts.Stdout != "" {
			return resp.Artifacts.Stdout
		}
		if resp.Result != "" {
			return resp.Result
		}
	}
	return "/home/daytona"
}

func ensureCredentialDirs(sandbox *daytona.Sandbox) (string, error) {
	homeDir := getSandboxHomeDir(sandbox)
	volPath := CredentialsMountPath
	cmds := []string{
		fmt.Sprintf("mkdir -p %s/claude", volPath),
		fmt.Sprintf("mkdir -p %s/codex", volPath),
		fmt.Sprintf("mkdir -p %s/opencode", volPath),
		fmt.Sprintf("mkdir -p %s/amp", volPath),
		fmt.Sprintf("mkdir -p %s/gemini", volPath),
		fmt.Sprintf("mkdir -p %s/factory", volPath),
		fmt.Sprintf("mkdir -p %s/gh", volPath),
		fmt.Sprintf("mkdir -p %s/git", volPath),
	}
	for _, cmd := range cmds {
		_, _ = sandbox.Process.ExecuteCommand(cmd)
	}
	return homeDir, nil
}

func ensureSymlink(sandbox *daytona.Sandbox, targetPath, sourcePath string) {
	_, _ = sandbox.Process.ExecuteCommand(fmt.Sprintf("mkdir -p $(dirname %s)", targetPath))
	_, _ = sandbox.Process.ExecuteCommand(
		fmt.Sprintf("if [ -e %s ] && [ ! -L %s ]; then rm -rf %s; fi", targetPath, targetPath, targetPath),
	)
	_, _ = sandbox.Process.ExecuteCommand(fmt.Sprintf("ln -sfn %s %s", sourcePath, targetPath))
}

func prepareClaudeHome(sandbox *daytona.Sandbox, homeDir string) {
	volPath := CredentialsMountPath
	claudeHome := fmt.Sprintf("%s/.claude", homeDir)
	_, _ = sandbox.Process.ExecuteCommand(fmt.Sprintf("mkdir -p %s/claude", volPath))
	_, _ = sandbox.Process.ExecuteCommand("mkdir -p /tmp/amux-claude-cache")
	_, _ = sandbox.Process.ExecuteCommand("mkdir -p /tmp/amux-claude-debug")
	_, _ = sandbox.Process.ExecuteCommand(
		fmt.Sprintf("ln -sfn /tmp/amux-claude-cache %s/claude/cache", volPath),
	)
	_, _ = sandbox.Process.ExecuteCommand(
		fmt.Sprintf("ln -sfn /tmp/amux-claude-debug %s/claude/debug", volPath),
	)
	ensureSymlink(sandbox, claudeHome, fmt.Sprintf("%s/claude", volPath))
}

func prepareCodexHome(sandbox *daytona.Sandbox, homeDir string) {
	volPath := CredentialsMountPath
	codexHome := fmt.Sprintf("%s/.codex", homeDir)
	codexConfigHome := fmt.Sprintf("%s/.config/codex", homeDir)
	ensureSymlink(sandbox, codexHome, fmt.Sprintf("%s/codex", volPath))
	ensureSymlink(sandbox, codexConfigHome, fmt.Sprintf("%s/codex", volPath))
	ensureFileStore := func(path string) string {
		return fmt.Sprintf(`if [ -f %s ]; then if grep -q '^cli_auth_credentials_store' %s; then sed -i 's/^cli_auth_credentials_store.*/cli_auth_credentials_store = "file"/' %s; else echo 'cli_auth_credentials_store = "file"' >> %s; fi; else mkdir -p $(dirname %s); echo 'cli_auth_credentials_store = "file"' > %s; fi`, path, path, path, path, path, path)
	}
	_, _ = sandbox.Process.ExecuteCommand(ensureFileStore(fmt.Sprintf("%s/codex/config.toml", volPath)))
}

func prepareOpenCodeHome(sandbox *daytona.Sandbox, homeDir string) {
	volPath := CredentialsMountPath
	dataDir := fmt.Sprintf("%s/.local/share/opencode", homeDir)
	configDir := fmt.Sprintf("%s/.config/opencode", homeDir)
	legacyConfig := fmt.Sprintf("%s/.opencode.json", homeDir)
	ensureSymlink(sandbox, dataDir, fmt.Sprintf("%s/opencode", volPath))
	ensureSymlink(sandbox, configDir, fmt.Sprintf("%s/opencode", volPath))
	_, _ = sandbox.Process.ExecuteCommand(fmt.Sprintf("ln -sfn %s/opencode/.opencode.json %s", volPath, legacyConfig))
}

func prepareAmpHome(sandbox *daytona.Sandbox, homeDir string) {
	volPath := CredentialsMountPath
	ampConfig := fmt.Sprintf("%s/.config/amp", homeDir)
	ampData := fmt.Sprintf("%s/.local/share/amp", homeDir)
	ensureSymlink(sandbox, ampConfig, fmt.Sprintf("%s/amp", volPath))
	ensureSymlink(sandbox, ampData, fmt.Sprintf("%s/amp", volPath))
}

func prepareGeminiHome(sandbox *daytona.Sandbox, homeDir string) {
	volPath := CredentialsMountPath
	geminiHome := fmt.Sprintf("%s/.gemini", homeDir)
	ensureSymlink(sandbox, geminiHome, fmt.Sprintf("%s/gemini", volPath))
}

func prepareFactoryHome(sandbox *daytona.Sandbox, homeDir string) {
	volPath := CredentialsMountPath
	factoryHome := fmt.Sprintf("%s/.factory", homeDir)
	ensureSymlink(sandbox, factoryHome, fmt.Sprintf("%s/factory", volPath))
}

func prepareGhHome(sandbox *daytona.Sandbox, homeDir string) {
	volPath := CredentialsMountPath
	ghConfig := fmt.Sprintf("%s/.config/gh", homeDir)
	ensureSymlink(sandbox, ghConfig, fmt.Sprintf("%s/gh", volPath))
}

func symlinkGitConfig(sandbox *daytona.Sandbox, homeDir string) {
	volPath := CredentialsMountPath
	_, _ = sandbox.Process.ExecuteCommand(fmt.Sprintf("ln -sf %s/git/.gitconfig %s/.gitconfig || true", volPath, homeDir))
}

// SetupCredentials mounts/symlinks the shared credentials volume inside the sandbox.
func SetupCredentials(client *daytona.Daytona, sandbox *daytona.Sandbox, cfg CredentialsConfig, verbose bool) error {
	if cfg.Mode != "sandbox" && cfg.Mode != "none" && cfg.Mode != "auto" {
		return fmt.Errorf("unsupported credentials mode: %s", cfg.Mode)
	}
	if cfg.Mode == "none" {
		if verbose {
			fmt.Println("Credentials mode: none")
		}
		return nil
	}
	if _, err := waitForVolumeReady(client, CredentialsVolumeName); err != nil {
		return err
	}
	if verbose {
		if cfg.Mode == "auto" {
			fmt.Println("Credentials mode: sandbox (auto)")
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
	symlinkGitConfig(sandbox, homeDir)

	// Sync local settings if enabled (opt-in feature)
	amuxCfg, _ := LoadConfig()
	if amuxCfg.SettingsSync.Enabled {
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
		fmt.Println("Credentials volume ready")
	}
	return nil
}

// SyncCredentialsFromSandbox is a no-op (credentials live in the shared volume).
func SyncCredentialsFromSandbox(_ *daytona.Sandbox, cfg CredentialsConfig) error {
	if cfg.Mode == "none" {
		return nil
	}
	return nil
}

// AgentCredentialStatus represents whether an agent has credentials stored
type AgentCredentialStatus struct {
	Agent         Agent
	HasCredential bool
	CredentialAge string // e.g., "2 days ago" or empty if unknown
}

// CheckAgentCredentials checks if credentials exist for an agent in the sandbox volume
func CheckAgentCredentials(sandbox *daytona.Sandbox, agent Agent) AgentCredentialStatus {
	status := AgentCredentialStatus{Agent: agent, HasCredential: false}

	switch agent {
	case AgentClaude:
		// Claude stores auth in ~/.claude/.credentials.json or uses ANTHROPIC_AUTH_TOKEN
		resp, err := sandbox.Process.ExecuteCommand(fmt.Sprintf(
			"test -f %s/claude/.credentials.json && echo exists",
			CredentialsMountPath,
		))
		if err == nil && resp != nil && resp.ExitCode == 0 {
			status.HasCredential = true
		}

	case AgentCodex:
		// Codex stores auth in ~/.codex/auth.json
		resp, err := sandbox.Process.ExecuteCommand(fmt.Sprintf(
			"test -f %s/codex/auth.json && echo exists",
			CredentialsMountPath,
		))
		if err == nil && resp != nil && resp.ExitCode == 0 {
			status.HasCredential = true
		}

	case AgentOpenCode:
		// OpenCode stores auth in ~/.local/share/opencode/auth.json
		resp, err := sandbox.Process.ExecuteCommand(fmt.Sprintf(
			"test -f %s/opencode/auth.json && echo exists",
			CredentialsMountPath,
		))
		if err == nil && resp != nil && resp.ExitCode == 0 {
			status.HasCredential = true
		}

	case AgentAmp:
		// Amp stores auth in ~/.local/share/amp/secrets.json
		resp, err := sandbox.Process.ExecuteCommand(fmt.Sprintf(
			"test -f %s/amp/secrets.json && echo exists",
			CredentialsMountPath,
		))
		if err == nil && resp != nil && resp.ExitCode == 0 {
			status.HasCredential = true
		}

	case AgentGemini:
		// Gemini stores OAuth in ~/.gemini/oauth_creds.json
		resp, err := sandbox.Process.ExecuteCommand(fmt.Sprintf(
			"test -f %s/gemini/oauth_creds.json && echo exists",
			CredentialsMountPath,
		))
		if err == nil && resp != nil && resp.ExitCode == 0 {
			status.HasCredential = true
		}

	case AgentDroid:
		// Droid stores config in ~/.factory/config.json
		resp, err := sandbox.Process.ExecuteCommand(fmt.Sprintf(
			"test -f %s/factory/config.json && echo exists",
			CredentialsMountPath,
		))
		if err == nil && resp != nil && resp.ExitCode == 0 {
			status.HasCredential = true
		}
	}

	return status
}

// CheckAllAgentCredentials returns credential status for all agents
func CheckAllAgentCredentials(sandbox *daytona.Sandbox) []AgentCredentialStatus {
	agents := []Agent{AgentClaude, AgentCodex, AgentOpenCode, AgentAmp, AgentGemini, AgentDroid}
	results := make([]AgentCredentialStatus, 0, len(agents))

	for _, agent := range agents {
		results = append(results, CheckAgentCredentials(sandbox, agent))
	}

	return results
}

// HasGitHubCredentials checks if GitHub CLI is authenticated
func HasGitHubCredentials(sandbox *daytona.Sandbox) bool {
	resp, err := sandbox.Process.ExecuteCommand(fmt.Sprintf(
		"test -f %s/gh/hosts.yml && echo exists",
		CredentialsMountPath,
	))
	return err == nil && resp != nil && resp.ExitCode == 0
}
