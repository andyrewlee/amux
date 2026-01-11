package sandbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/andyrewlee/amux/internal/daytona"
)

// SettingsSyncConfig configures which settings to sync to sandboxes
type SettingsSyncConfig struct {
	// Enabled indicates if settings sync is enabled (requires explicit opt-in)
	Enabled bool `json:"enabled"`
	// Claude syncs ~/.claude/settings.json and related config
	Claude bool `json:"claude,omitempty"`
	// Codex syncs ~/.codex/config.toml
	Codex bool `json:"codex,omitempty"`
	// Git syncs ~/.gitconfig (name, email, aliases - NOT credentials)
	Git bool `json:"git,omitempty"`
	// Shell syncs shell preferences (prompt, aliases from a safe subset)
	Shell bool `json:"shell,omitempty"`
}

// AgentSettingsPath describes where an agent stores its settings locally
type AgentSettingsPath struct {
	Agent       Agent
	LocalPath   string   // Path relative to home directory
	Description string   // Human-readable description for consent UI
	SafeKeys    []string // If non-empty, only sync these keys (for partial sync)
}

// KnownAgentSettings returns the settings paths for all supported agents
func KnownAgentSettings() []AgentSettingsPath {
	return []AgentSettingsPath{
		{
			Agent:       AgentClaude,
			LocalPath:   ".claude/settings.json",
			Description: "Claude Code settings (model preferences, features, permissions)",
		},
		{
			Agent:       AgentCodex,
			LocalPath:   ".codex/config.toml",
			Description: "Codex CLI settings (model preferences, editor config)",
		},
		{
			Agent:       AgentOpenCode,
			LocalPath:   ".config/opencode/config.json",
			Description: "OpenCode settings (model preferences, keybindings)",
		},
		{
			Agent:       AgentAmp,
			LocalPath:   ".config/amp/config.json",
			Description: "Amp settings (model preferences, workspace config)",
		},
		{
			Agent:       AgentGemini,
			LocalPath:   ".gemini/settings.json",
			Description: "Gemini CLI settings (model preferences)",
		},
	}
}

// GitConfigSafeKeys are the .gitconfig keys that are safe to sync (no credentials)
var GitConfigSafeKeys = []string{
	"user.name",
	"user.email",
	"core.editor",
	"core.autocrlf",
	"init.defaultBranch",
	"pull.rebase",
	"push.default",
	"alias.*",
}

// LoadSettingsSyncConfig loads the settings sync configuration
func LoadSettingsSyncConfig() (SettingsSyncConfig, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return SettingsSyncConfig{}, err
	}

	// Settings sync config is stored in the main config file
	return cfg.SettingsSync, nil
}

// SaveSettingsSyncConfig saves the settings sync configuration
func SaveSettingsSyncConfig(syncCfg SettingsSyncConfig) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	cfg.SettingsSync = syncCfg
	return SaveConfig(cfg)
}

// SyncSettingsToVolume copies enabled local settings to the credentials volume
// This is called during sandbox setup if settings sync is enabled
func SyncSettingsToVolume(sandbox *daytona.Sandbox, syncCfg SettingsSyncConfig, verbose bool) error {
	if !syncCfg.Enabled {
		return nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	volPath := CredentialsMountPath
	sandboxHome := getSandboxHomeDir(sandbox)

	var syncedCount int

	// Sync Claude settings
	if syncCfg.Claude {
		if err := syncAgentSettings(sandbox, homeDir, volPath, sandboxHome, AgentClaude, verbose); err != nil {
			if verbose {
				fmt.Printf("  Warning: could not sync Claude settings: %v\n", err)
			}
		} else {
			syncedCount++
		}
	}

	// Sync Codex settings
	if syncCfg.Codex {
		if err := syncAgentSettings(sandbox, homeDir, volPath, sandboxHome, AgentCodex, verbose); err != nil {
			if verbose {
				fmt.Printf("  Warning: could not sync Codex settings: %v\n", err)
			}
		} else {
			syncedCount++
		}
	}

	// Sync Git config (safe keys only)
	if syncCfg.Git {
		if err := syncGitConfig(sandbox, homeDir, volPath, verbose); err != nil {
			if verbose {
				fmt.Printf("  Warning: could not sync Git config: %v\n", err)
			}
		} else {
			syncedCount++
		}
	}

	if verbose && syncedCount > 0 {
		fmt.Printf("  Synced %d settings configuration(s)\n", syncedCount)
	}

	return nil
}

// syncAgentSettings syncs settings for a specific agent
func syncAgentSettings(sandbox *daytona.Sandbox, homeDir, volPath, sandboxHome string, agent Agent, verbose bool) error {
	var localPath, volSubdir string

	switch agent {
	case AgentClaude:
		localPath = filepath.Join(homeDir, ".claude", "settings.json")
		volSubdir = "claude"
	case AgentCodex:
		localPath = filepath.Join(homeDir, ".codex", "config.toml")
		volSubdir = "codex"
	case AgentOpenCode:
		localPath = filepath.Join(homeDir, ".config", "opencode", "config.json")
		volSubdir = "opencode"
	case AgentAmp:
		localPath = filepath.Join(homeDir, ".config", "amp", "config.json")
		volSubdir = "amp"
	case AgentGemini:
		localPath = filepath.Join(homeDir, ".gemini", "settings.json")
		volSubdir = "gemini"
	default:
		return nil
	}

	// Check if local settings file exists
	data, err := os.ReadFile(localPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // No local settings, skip
		}
		return err
	}

	// For JSON files, filter out any sensitive keys
	if strings.HasSuffix(localPath, ".json") {
		data, err = filterSensitiveJSON(data)
		if err != nil {
			return err
		}
	}

	// Upload to sandbox volume
	remotePath := fmt.Sprintf("%s/%s/settings.json", volPath, volSubdir)
	if strings.HasSuffix(localPath, ".toml") {
		remotePath = fmt.Sprintf("%s/%s/config.toml", volPath, volSubdir)
	}

	if err := sandbox.FS.UploadFile(data, remotePath, 0); err != nil {
		return fmt.Errorf("failed to upload settings: %w", err)
	}

	if verbose {
		fmt.Printf("  Synced %s settings\n", agent)
	}

	return nil
}

// syncGitConfig syncs safe git configuration (no credentials)
func syncGitConfig(sandbox *daytona.Sandbox, homeDir, volPath string, verbose bool) error {
	gitConfigPath := filepath.Join(homeDir, ".gitconfig")

	data, err := os.ReadFile(gitConfigPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // No gitconfig, skip
		}
		return err
	}

	// Filter to only safe keys (very basic INI parsing for safety)
	safeConfig := filterGitConfig(string(data))
	if safeConfig == "" {
		return nil
	}

	remotePath := fmt.Sprintf("%s/git/.gitconfig", volPath)
	if err := sandbox.FS.UploadFile([]byte(safeConfig), remotePath, 0); err != nil {
		return fmt.Errorf("failed to upload git config: %w", err)
	}

	if verbose {
		fmt.Println("  Synced git config (name, email, aliases)")
	}

	return nil
}

// filterSensitiveJSON removes potentially sensitive keys from JSON config
func filterSensitiveJSON(data []byte) ([]byte, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return data, nil // Not valid JSON, return as-is
	}

	// Remove keys that might contain sensitive data
	sensitiveKeys := []string{
		"apiKey", "api_key", "apikey",
		"token", "auth_token", "authToken",
		"secret", "password", "credential",
		"key", "private",
	}

	filtered := filterMapKeys(obj, sensitiveKeys)
	return json.MarshalIndent(filtered, "", "  ")
}

// filterMapKeys recursively removes sensitive keys from a map
func filterMapKeys(obj map[string]interface{}, sensitiveKeys []string) map[string]interface{} {
	result := make(map[string]interface{})

	for k, v := range obj {
		// Check if key contains sensitive words
		isSensitive := false
		lowerKey := strings.ToLower(k)
		for _, sensitive := range sensitiveKeys {
			if strings.Contains(lowerKey, strings.ToLower(sensitive)) {
				isSensitive = true
				break
			}
		}

		if isSensitive {
			continue
		}

		// Recursively filter nested maps
		if nested, ok := v.(map[string]interface{}); ok {
			result[k] = filterMapKeys(nested, sensitiveKeys)
		} else {
			result[k] = v
		}
	}

	return result
}

// filterGitConfig extracts only safe configuration from gitconfig
func filterGitConfig(content string) string {
	lines := strings.Split(content, "\n")
	var result []string
	inSafeSection := false

	safeSections := map[string]bool{
		"[user]":   true,
		"[core]":   true,
		"[init]":   true,
		"[pull]":   true,
		"[push]":   true,
		"[alias]":  true,
		"[color]":  true,
		"[diff]":   true,
		"[merge]":  true,
		"[branch]": true,
	}

	unsafeSections := map[string]bool{
		"[credential]": true,
		"[http]":       true,
		"[url":         true, // Catches [url "..."]
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check if this is a section header
		if strings.HasPrefix(trimmed, "[") {
			// Check if section is unsafe
			isUnsafe := false
			for unsafe := range unsafeSections {
				if strings.HasPrefix(trimmed, unsafe) {
					isUnsafe = true
					break
				}
			}

			if isUnsafe {
				inSafeSection = false
				continue
			}

			// Check if section is explicitly safe
			isSafe := false
			for safe := range safeSections {
				if strings.HasPrefix(trimmed, safe) {
					isSafe = true
					break
				}
			}

			inSafeSection = isSafe
			if isSafe {
				result = append(result, line)
			}
			continue
		}

		// Include line if we're in a safe section
		if inSafeSection && trimmed != "" {
			// Extra safety: skip any line that looks like it contains credentials
			lowerLine := strings.ToLower(trimmed)
			if strings.Contains(lowerLine, "token") ||
				strings.Contains(lowerLine, "password") ||
				strings.Contains(lowerLine, "credential") ||
				strings.Contains(lowerLine, "oauth") {
				continue
			}
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// PromptSettingsSync displays the settings sync consent prompt
// Returns true if user consents, false otherwise
func PromptSettingsSync() (SettingsSyncConfig, bool) {
	// This function would be called from the CLI to get user consent
	// The actual prompting is done in the CLI layer
	return SettingsSyncConfig{}, false
}

// GetLocalSettingsStatus returns which settings files exist locally
func GetLocalSettingsStatus() map[Agent]bool {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	status := make(map[Agent]bool)
	for _, setting := range KnownAgentSettings() {
		path := filepath.Join(homeDir, setting.LocalPath)
		if _, err := os.Stat(path); err == nil {
			status[setting.Agent] = true
		}
	}

	// Check git config
	gitConfigPath := filepath.Join(homeDir, ".gitconfig")
	if _, err := os.Stat(gitConfigPath); err == nil {
		status["git"] = true
	}

	return status
}
