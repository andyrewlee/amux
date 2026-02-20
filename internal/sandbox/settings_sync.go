package sandbox

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

// SyncSettingsToVolume copies enabled local settings to the sandbox home directory.
// This is called during sandbox setup if settings sync is enabled.
// It always displays a manifest of files being synced for transparency.
func SyncSettingsToVolume(computer RemoteSandbox, syncCfg SettingsSyncConfig, verbose bool) error {
	if !syncCfg.Enabled {
		return nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	computerHome := getSandboxHomeDir(computer)

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
			syncErr = syncGitConfig(computer, homeDir, computerHome, verbose)
		} else {
			agent := Agent(setting.Agent)
			syncErr = syncAgentSettings(computer, homeDir, computerHome, agent, verbose)
		}

		if syncErr != nil {
			if verbose {
				fmt.Fprintf(sandboxStdout, "  Warning: could not sync %s settings: %v\n", setting.Agent, syncErr)
			}
		} else {
			syncedCount++
		}
	}

	if verbose && syncedCount > 0 {
		fmt.Fprintf(sandboxStdout, "  Synced %d settings configuration(s)\n", syncedCount)
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
func syncAgentSettings(computer RemoteSandbox, homeDir, computerHome string, agent Agent, verbose bool) error {
	var localPath, remotePath string

	switch agent {
	case AgentClaude:
		localPath = filepath.Join(homeDir, ".claude", "settings.json")
		remotePath = fmt.Sprintf("%s/.claude/settings.json", computerHome)
	case AgentCodex:
		localPath = filepath.Join(homeDir, ".codex", "config.toml")
		remotePath = fmt.Sprintf("%s/.config/codex/config.toml", computerHome)
	case AgentOpenCode:
		localPath = filepath.Join(homeDir, ".config", "opencode", "config.json")
		remotePath = fmt.Sprintf("%s/.config/opencode/config.json", computerHome)
	case AgentAmp:
		localPath = filepath.Join(homeDir, ".config", "amp", "config.json")
		remotePath = fmt.Sprintf("%s/.config/amp/config.json", computerHome)
	case AgentGemini:
		localPath = filepath.Join(homeDir, ".gemini", "settings.json")
		remotePath = fmt.Sprintf("%s/.gemini/settings.json", computerHome)
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
	if err := uploadBytes(ctx, computer, data, remotePath); err != nil {
		return fmt.Errorf("failed to upload settings: %w", err)
	}

	if verbose {
		fmt.Fprintf(sandboxStdout, "  Synced %s settings\n", agent)
	}

	return nil
}

// syncGitConfig syncs safe git configuration (no credentials)
func syncGitConfig(computer RemoteSandbox, homeDir, computerHome string, verbose bool) error {
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

	remotePath := fmt.Sprintf("%s/.gitconfig", computerHome)
	ctx, cancel := context.WithTimeout(context.Background(), settingsUploadTimeout)
	defer cancel()
	if err := uploadBytes(ctx, computer, []byte(safeConfig), remotePath); err != nil {
		return fmt.Errorf("failed to upload git config: %w", err)
	}

	if verbose {
		fmt.Fprintln(sandboxStdout, "  Synced git config (name, email, aliases)")
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
