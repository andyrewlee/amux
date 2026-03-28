package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// settingsUploadTimeout is the timeout for uploading settings files.
const settingsUploadTimeout = 30 * time.Second

// agentSyncDef maps an agent to its local and remote settings paths.
// LocalHome is relative to $HOME (used for reading local files).
// RemoteHome is relative to the remote home (may differ, e.g. Codex).
type agentSyncDef struct {
	Agent      Agent
	LocalHome  string // e.g. ".claude/settings.json"
	RemoteHome string // e.g. ".claude/settings.json"
	ConfigFlag func(SettingsSyncConfig) bool
}

var agentSyncDefs = []agentSyncDef{
	{AgentClaude, ".claude/settings.json", ".claude/settings.json", func(c SettingsSyncConfig) bool { return c.Claude }},
	{AgentCodex, ".codex/config.toml", ".config/codex/config.toml", func(c SettingsSyncConfig) bool { return c.Codex }},
	{AgentOpenCode, ".config/opencode/config.json", ".config/opencode/config.json", nil},
	{AgentAmp, ".config/amp/config.json", ".config/amp/config.json", nil},
	{AgentGemini, ".gemini/settings.json", ".gemini/settings.json", nil},
}

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
	explicitFiles := len(syncCfg.Files) > 0

	for _, setting := range settingsToSync {
		if !setting.Exists {
			continue
		}

		var syncErr error
		if explicitFiles {
			syncErr = syncExplicitSetting(computer, computerHome, setting, verbose)
		} else if setting.Agent == "git" {
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

	for _, def := range agentSyncDefs {
		if def.ConfigFlag == nil || !def.ConfigFlag(syncCfg) {
			continue
		}
		path := filepath.Join(homeDir, def.LocalHome)
		s := DetectedSetting{Agent: string(def.Agent), LocalPath: path, HomePath: def.LocalHome}
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
	if strings.Contains(path, ".codex") {
		return "codex"
	}
	if pathContainsComponent(path, "codex") {
		return "codex"
	}
	if pathContainsComponent(path, "opencode") {
		return "opencode"
	}
	if pathContainsComponent(path, "amp") {
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

func pathContainsComponent(path, component string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(path), "/") {
		if seg == component || seg == "."+component {
			return true
		}
	}
	return false
}

func syncExplicitSetting(computer RemoteSandbox, computerHome string, setting DetectedSetting, verbose bool) error {
	data, err := os.ReadFile(setting.LocalPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	remotePath := resolveExplicitSettingRemotePath(computerHome, setting)
	data, err = sanitizeSettingData(setting, remotePath, data)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), settingsUploadTimeout)
	defer cancel()
	if err := uploadBytes(ctx, computer, data, remotePath); err != nil {
		return fmt.Errorf("failed to upload settings: %w", err)
	}

	if verbose {
		fmt.Fprintf(sandboxStdout, "  Synced %s\n", setting.HomePath)
	}
	return nil
}

func resolveExplicitSettingRemotePath(computerHome string, setting DetectedSetting) string {
	if remotePath, ok := defaultRemotePathForSetting(computerHome, setting); ok {
		return remotePath
	}

	trimmed := strings.TrimSpace(setting.HomePath)
	if trimmed == "" {
		return computerHome
	}
	if strings.HasPrefix(trimmed, "/") {
		return trimmed
	}
	return computerHome + "/" + strings.TrimPrefix(trimmed, "/")
}

func defaultRemotePathForSetting(computerHome string, setting DetectedSetting) (string, bool) {
	homePath := strings.TrimPrefix(strings.TrimSpace(setting.HomePath), "~/")
	agent := Agent(strings.TrimSpace(setting.Agent))
	for _, def := range agentSyncDefs {
		if agent == def.Agent && homePath == def.LocalHome {
			return computerHome + "/" + def.RemoteHome, true
		}
	}
	if strings.TrimSpace(setting.Agent) == "git" && homePath == ".gitconfig" {
		return computerHome + "/.gitconfig", true
	}
	return "", false
}

// syncAgentSettings syncs settings for a specific agent
func syncAgentSettings(computer RemoteSandbox, homeDir, computerHome string, agent Agent, verbose bool) error {
	var localPath, remotePath string
	for _, def := range agentSyncDefs {
		if def.Agent == agent {
			localPath = filepath.Join(homeDir, def.LocalHome)
			remotePath = computerHome + "/" + def.RemoteHome
			break
		}
	}
	if localPath == "" {
		return nil
	}

	data, err := os.ReadFile(localPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	data, err = sanitizeSettingData(DetectedSetting{
		Agent:     string(agent),
		LocalPath: localPath,
		HomePath:  strings.TrimPrefix(remotePath, computerHome+"/"),
	}, remotePath, data)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
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

func sanitizeSettingData(setting DetectedSetting, remotePath string, data []byte) ([]byte, error) {
	switch {
	case strings.TrimSpace(setting.Agent) == "git" || filepath.Base(setting.LocalPath) == ".gitconfig":
		safeConfig := filterGitConfig(string(data))
		if safeConfig == "" {
			return nil, nil
		}
		return []byte(safeConfig), nil
	case strings.HasSuffix(strings.ToLower(setting.LocalPath), ".json"):
		return filterSensitiveJSON(data)
	case isCodexConfigPath(remotePath):
		return ensureCodexFileStoreSetting(data), nil
	default:
		return data, nil
	}
}

func isCodexConfigPath(path string) bool {
	normalized := filepath.ToSlash(strings.TrimSpace(path))
	return strings.HasSuffix(normalized, "/.config/codex/config.toml")
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

	remotePath := computerHome + "/.gitconfig"
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
