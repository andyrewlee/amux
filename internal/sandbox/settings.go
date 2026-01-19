package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
)

// SettingsSyncConfig configures which settings to sync to sandboxes
type SettingsSyncConfig struct {
	// Enabled indicates if settings sync is enabled (requires explicit opt-in)
	Enabled bool `json:"enabled"`
	// Files is the explicit list of settings files to sync (e.g., ["~/.claude/settings.json"])
	// When non-empty, only these files are synced (provides transparency)
	Files []string `json:"files,omitempty"`
	// Legacy per-agent flags (still supported for backwards compatibility)
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

// DetectedSetting represents a settings file found on the local system
type DetectedSetting struct {
	Agent       string // Agent name (e.g., "claude", "codex") or "git" for gitconfig
	LocalPath   string // Full path (e.g., "/home/user/.claude/settings.json")
	HomePath    string // Path relative to home (e.g., ".claude/settings.json")
	Description string // Human-readable description
	Exists      bool   // Whether the file exists locally
	Size        int64  // File size in bytes (0 if not exists)
}

// DetectLocalSettings finds all settings files that exist on the local machine.
// It iterates through all registered agent plugins and checks for their settings files.
func DetectLocalSettings() []DetectedSetting {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	var detected []DetectedSetting

	// Check all registered agent plugins
	for _, plugin := range AllAgentPlugins() {
		for _, sp := range plugin.SettingsPaths() {
			fullPath := filepath.Join(homeDir, sp.LocalPath)
			setting := DetectedSetting{
				Agent:       plugin.Name(),
				LocalPath:   fullPath,
				HomePath:    sp.LocalPath,
				Description: sp.Description,
			}

			if info, err := os.Stat(fullPath); err == nil {
				setting.Exists = true
				setting.Size = info.Size()
			}

			detected = append(detected, setting)
		}
	}

	// Check git config separately (not an agent plugin)
	gitConfigPath := filepath.Join(homeDir, ".gitconfig")
	gitSetting := DetectedSetting{
		Agent:       "git",
		LocalPath:   gitConfigPath,
		HomePath:    ".gitconfig",
		Description: "Git config (user identity, aliases)",
	}
	if info, err := os.Stat(gitConfigPath); err == nil {
		gitSetting.Exists = true
		gitSetting.Size = info.Size()
	}
	detected = append(detected, gitSetting)

	return detected
}

// DetectExistingSettings returns only settings files that actually exist locally
func DetectExistingSettings() []DetectedSetting {
	all := DetectLocalSettings()
	var existing []DetectedSetting
	for _, s := range all {
		if s.Exists {
			existing = append(existing, s)
		}
	}
	return existing
}

// PrintSettingsManifest prints the list of settings files being synced
func PrintSettingsManifest(settings []DetectedSetting) {
	if len(settings) == 0 {
		fmt.Println("Settings sync: no settings files found locally")
		return
	}

	fmt.Println("Settings sync:")
	for _, s := range settings {
		if s.Exists {
			note := ""
			if s.Agent == "git" {
				note = " (safe keys only)"
			}
			fmt.Printf("  ~/%s → sandbox%s\n", s.HomePath, note)
		}
	}
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
