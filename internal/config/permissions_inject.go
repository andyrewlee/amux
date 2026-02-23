package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// InjectGlobalPermissions merges global permissions into a profile's settings.json.
// Creates the file if it does not exist.
func InjectGlobalPermissions(profileDir string, global *GlobalPermissions) error {
	if global == nil || (len(global.Allow) == 0 && len(global.Deny) == 0) {
		return nil
	}

	settingsPath := filepath.Join(profileDir, "settings.json")

	var settings map[string]any
	if existing, err := os.ReadFile(settingsPath); err == nil {
		_ = json.Unmarshal(existing, &settings)
	}
	if settings == nil {
		settings = make(map[string]any)
	}

	perms, _ := settings["permissions"].(map[string]any)
	if perms == nil {
		perms = make(map[string]any)
	}

	// Merge allow list using set-based deduplication
	perms["allow"] = mergeUnique(toStringSlice(perms["allow"]), global.Allow)

	// Merge deny list using set-based deduplication
	perms["deny"] = mergeUnique(toStringSlice(perms["deny"]), global.Deny)

	settings["permissions"] = perms

	if err := os.MkdirAll(profileDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, data, 0644)
}

// InjectAdditionalDirectories writes additionalDirectories into
// {primaryRoot}/.claude/settings.local.json → permissions.additionalDirectories.
func InjectAdditionalDirectories(primaryRoot string, additionalRoots []string) error {
	if len(additionalRoots) == 0 {
		return nil
	}

	claudeDir := filepath.Join(primaryRoot, ".claude")
	settingsPath := filepath.Join(claudeDir, "settings.local.json")

	var settings map[string]any
	if existing, err := os.ReadFile(settingsPath); err == nil {
		_ = json.Unmarshal(existing, &settings)
	}
	if settings == nil {
		settings = make(map[string]any)
	}

	perms, _ := settings["permissions"].(map[string]any)
	if perms == nil {
		perms = make(map[string]any)
	}

	// Replace entirely to avoid stale entries
	dirs := make([]any, len(additionalRoots))
	for i, root := range additionalRoots {
		dirs[i] = root
	}
	perms["additionalDirectories"] = dirs
	settings["permissions"] = perms

	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, data, 0644)
}

// InjectAllowEdits adds Edit(**) to a workspace's .claude/settings.local.json.
// This pre-grants the Edit permission for this specific workspace only.
func InjectAllowEdits(workspaceRoot string) error {
	claudeDir := filepath.Join(workspaceRoot, ".claude")
	settingsPath := filepath.Join(claudeDir, "settings.local.json")

	var settings map[string]any
	if existing, err := os.ReadFile(settingsPath); err == nil {
		_ = json.Unmarshal(existing, &settings)
	}
	if settings == nil {
		settings = make(map[string]any)
	}

	perms, _ := settings["permissions"].(map[string]any)
	if perms == nil {
		perms = make(map[string]any)
	}

	perms["allow"] = mergeUnique(toStringSlice(perms["allow"]), []string{"Edit(**)"})
	settings["permissions"] = perms

	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, data, 0644)
}

// InjectSkipPermissionPrompt sets skipDangerousModePermissionPrompt=true
// in the profile's settings.json so Claude Code doesn't show the bypass
// permissions confirmation dialog when --dangerously-skip-permissions is used.
func InjectSkipPermissionPrompt(profileDir string) error {
	settingsPath := filepath.Join(profileDir, "settings.json")

	var settings map[string]any
	if existing, err := os.ReadFile(settingsPath); err == nil {
		_ = json.Unmarshal(existing, &settings)
	}
	if settings == nil {
		settings = make(map[string]any)
	}

	settings["skipDangerousModePermissionPrompt"] = true

	if err := os.MkdirAll(profileDir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, data, 0644)
}

// InjectTrustedDirectory adds a directory to Claude's trusted projects.
// If configDir is empty, uses ~/.claude.json. Otherwise uses configDir/.claude.json.
// This prevents the "do you want to trust this directory" prompt when Claude starts.
func InjectTrustedDirectory(workspaceRoot string, configDir string) error {
	var claudeConfigPath string
	if configDir != "" {
		claudeConfigPath = filepath.Join(configDir, ".claude.json")
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		claudeConfigPath = filepath.Join(home, ".claude.json")
	}

	var config map[string]any
	if existing, err := os.ReadFile(claudeConfigPath); err == nil {
		_ = json.Unmarshal(existing, &config)
	}
	if config == nil {
		config = make(map[string]any)
	}

	// Get or create the projects map
	projects, _ := config["projects"].(map[string]any)
	if projects == nil {
		projects = make(map[string]any)
	}

	// Get or create the project entry for this workspace
	projectEntry, _ := projects[workspaceRoot].(map[string]any)
	if projectEntry == nil {
		projectEntry = map[string]any{
			"allowedTools":            []any{},
			"mcpContextUris":          []any{},
			"mcpServers":              map[string]any{},
			"enabledMcpjsonServers":   []any{},
			"disabledMcpjsonServers":  []any{},
			"hasTrustDialogAccepted":  true,
		}
	} else {
		// Update existing entry to mark as trusted
		projectEntry["hasTrustDialogAccepted"] = true
	}

	projects[workspaceRoot] = projectEntry
	config["projects"] = projects

	// Ensure config directory exists
	if configDir != "" {
		if err := os.MkdirAll(configDir, 0755); err != nil {
			return err
		}
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(claudeConfigPath, data, 0600)
}

// InjectIntoAllProfiles iterates all profile directories and merges global
// permissions into each one's settings.json.
func InjectIntoAllProfiles(profilesRoot string, global *GlobalPermissions) error {
	if global == nil || (len(global.Allow) == 0 && len(global.Deny) == 0) {
		return nil
	}
	entries, err := os.ReadDir(profilesRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "shared" {
			continue
		}
		profileDir := filepath.Join(profilesRoot, entry.Name())
		if err := InjectGlobalPermissions(profileDir, global); err != nil {
			return err
		}
	}
	return nil
}

func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// mergeUnique merges two string slices, returning a deduplicated result.
// Preserves order: existing entries first, then new entries not in existing.
func mergeUnique(existing, additions []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(existing)+len(additions))

	// Add existing entries (deduplicated)
	for _, s := range existing {
		trimmed := strings.TrimSpace(s)
		if trimmed != "" && !seen[trimmed] {
			seen[trimmed] = true
			result = append(result, trimmed)
		}
	}

	// Add new entries not already present
	for _, s := range additions {
		trimmed := strings.TrimSpace(s)
		if trimmed != "" && !seen[trimmed] {
			seen[trimmed] = true
			result = append(result, trimmed)
		}
	}

	// Ensure non-nil for JSON marshaling
	if result == nil {
		return []string{}
	}
	return result
}
