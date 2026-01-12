package computer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// settingsUploadTimeout is the timeout for uploading settings files.
const settingsUploadTimeout = 30 * time.Second

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
			fmt.Printf("  ~/%s → computer%s\n", s.HomePath, note)
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

// SyncSettingsToVolume copies enabled local settings to the computer home directory.
// This is called during computer setup if settings sync is enabled.
// It always displays a manifest of files being synced for transparency.
func SyncSettingsToVolume(sandbox RemoteComputer, syncCfg SettingsSyncConfig, verbose bool) error {
	if !syncCfg.Enabled {
		return nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	sandboxHome := getSandboxHomeDir(sandbox)

	// Determine which settings to sync
	settingsToSync := getSettingsToSync(syncCfg, homeDir)

	// Always show manifest for transparency
	PrintSettingsManifest(settingsToSync)

	if len(settingsToSync) == 0 {
		return nil
	}

	var syncedCount int

	for _, setting := range settingsToSync {
		if !setting.Exists {
			continue
		}

		var syncErr error
		if setting.Agent == "git" {
			syncErr = syncGitConfig(sandbox, homeDir, sandboxHome, verbose)
		} else {
			agent := Agent(setting.Agent)
			syncErr = syncAgentSettings(sandbox, homeDir, sandboxHome, agent, verbose)
		}

		if syncErr != nil {
			if verbose {
				fmt.Printf("  Warning: could not sync %s settings: %v\n", setting.Agent, syncErr)
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

// getSettingsToSync determines which settings files should be synced based on config
func getSettingsToSync(syncCfg SettingsSyncConfig, homeDir string) []DetectedSetting {
	// If explicit Files list is set, use only those
	if len(syncCfg.Files) > 0 {
		return getSettingsFromFileList(syncCfg.Files, homeDir)
	}

	// Fall back to legacy per-agent flags
	return getSettingsFromLegacyFlags(syncCfg, homeDir)
}

// getSettingsFromFileList returns DetectedSettings for an explicit file list
func getSettingsFromFileList(files []string, homeDir string) []DetectedSetting {
	var settings []DetectedSetting

	for _, file := range files {
		// Expand ~ to home directory
		path := file
		if strings.HasPrefix(path, "~/") {
			path = filepath.Join(homeDir, path[2:])
		} else if strings.HasPrefix(path, ".") {
			path = filepath.Join(homeDir, path)
		}

		// Determine agent from path
		agent := agentFromPath(file)
		homePath := strings.TrimPrefix(file, "~/")
		if strings.HasPrefix(homePath, homeDir) {
			homePath = strings.TrimPrefix(homePath, homeDir+"/")
		}

		setting := DetectedSetting{
			Agent:     agent,
			LocalPath: path,
			HomePath:  homePath,
		}

		if info, err := os.Stat(path); err == nil {
			setting.Exists = true
			setting.Size = info.Size()
		}

		settings = append(settings, setting)
	}

	return settings
}

// getSettingsFromLegacyFlags returns settings based on legacy per-agent boolean flags
func getSettingsFromLegacyFlags(syncCfg SettingsSyncConfig, homeDir string) []DetectedSetting {
	var settings []DetectedSetting

	if syncCfg.Claude {
		path := filepath.Join(homeDir, ".claude", "settings.json")
		s := DetectedSetting{Agent: "claude", LocalPath: path, HomePath: ".claude/settings.json"}
		if info, err := os.Stat(path); err == nil {
			s.Exists = true
			s.Size = info.Size()
		}
		settings = append(settings, s)
	}

	if syncCfg.Codex {
		path := filepath.Join(homeDir, ".codex", "config.toml")
		s := DetectedSetting{Agent: "codex", LocalPath: path, HomePath: ".codex/config.toml"}
		if info, err := os.Stat(path); err == nil {
			s.Exists = true
			s.Size = info.Size()
		}
		settings = append(settings, s)
	}

	if syncCfg.Git {
		path := filepath.Join(homeDir, ".gitconfig")
		s := DetectedSetting{Agent: "git", LocalPath: path, HomePath: ".gitconfig"}
		if info, err := os.Stat(path); err == nil {
			s.Exists = true
			s.Size = info.Size()
		}
		settings = append(settings, s)
	}

	return settings
}

// agentFromPath determines the agent name from a settings file path
func agentFromPath(path string) string {
	if strings.Contains(path, ".claude") {
		return "claude"
	}
	if strings.Contains(path, ".codex") || strings.Contains(path, "codex") {
		return "codex"
	}
	if strings.Contains(path, "opencode") {
		return "opencode"
	}
	if strings.Contains(path, "amp") {
		return "amp"
	}
	if strings.Contains(path, ".gemini") {
		return "gemini"
	}
	if strings.Contains(path, ".gitconfig") {
		return "git"
	}
	return "unknown"
}

// syncAgentSettings syncs settings for a specific agent
func syncAgentSettings(sandbox RemoteComputer, homeDir, sandboxHome string, agent Agent, verbose bool) error {
	var localPath, remotePath string

	switch agent {
	case AgentClaude:
		localPath = filepath.Join(homeDir, ".claude", "settings.json")
		remotePath = fmt.Sprintf("%s/.claude/settings.json", sandboxHome)
	case AgentCodex:
		localPath = filepath.Join(homeDir, ".codex", "config.toml")
		remotePath = fmt.Sprintf("%s/.config/codex/config.toml", sandboxHome)
	case AgentOpenCode:
		localPath = filepath.Join(homeDir, ".config", "opencode", "config.json")
		remotePath = fmt.Sprintf("%s/.config/opencode/config.json", sandboxHome)
	case AgentAmp:
		localPath = filepath.Join(homeDir, ".config", "amp", "config.json")
		remotePath = fmt.Sprintf("%s/.config/amp/config.json", sandboxHome)
	case AgentGemini:
		localPath = filepath.Join(homeDir, ".gemini", "settings.json")
		remotePath = fmt.Sprintf("%s/.gemini/settings.json", sandboxHome)
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

	ctx, cancel := context.WithTimeout(context.Background(), settingsUploadTimeout)
	defer cancel()
	if err := uploadBytes(ctx, sandbox, data, remotePath); err != nil {
		return fmt.Errorf("failed to upload settings: %w", err)
	}

	if verbose {
		fmt.Printf("  Synced %s settings\n", agent)
	}

	return nil
}

// syncGitConfig syncs safe git configuration (no credentials)
func syncGitConfig(sandbox RemoteComputer, homeDir, sandboxHome string, verbose bool) error {
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

	remotePath := fmt.Sprintf("%s/.gitconfig", sandboxHome)
	ctx, cancel := context.WithTimeout(context.Background(), settingsUploadTimeout)
	defer cancel()
	if err := uploadBytes(ctx, sandbox, []byte(safeConfig), remotePath); err != nil {
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
