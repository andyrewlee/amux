package config

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	// This handles both existing duplicates and prevents new ones
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

// NormalizeProjectPermissions normalizes permission formats in a project's .claude directory.
// This updates settings.local.json and settings.json to use the new format.
func NormalizeProjectPermissions(workspaceRoot string) error {
	claudeDir := filepath.Join(workspaceRoot, ".claude")

	// Normalize settings.local.json (where Claude Code writes per-project permissions)
	localPath := filepath.Join(claudeDir, "settings.local.json")
	if err := normalizeSettingsFile(localPath); err != nil {
		return err
	}

	// Also normalize settings.json if it exists
	sharedPath := filepath.Join(claudeDir, "settings.json")
	if err := normalizeSettingsFile(sharedPath); err != nil {
		return err
	}

	return nil
}

// normalizeSettingsFile normalizes permission formats in a Claude settings file.
func normalizeSettingsFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist, nothing to normalize
		}
		return err
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil // Invalid JSON, skip
	}

	perms, ok := settings["permissions"].(map[string]any)
	if !ok || perms == nil {
		return nil // No permissions section
	}

	modified := false

	// Normalize allow list
	if allow := toStringSlice(perms["allow"]); allow != nil {
		normalized := normalizeAndDedupeSlice(allow)
		if !slicesEqual(allow, normalized) {
			perms["allow"] = normalized
			modified = true
		}
	}

	// Normalize deny list
	if deny := toStringSlice(perms["deny"]); deny != nil {
		normalized := normalizeAndDedupeSlice(deny)
		if !slicesEqual(deny, normalized) {
			perms["deny"] = normalized
			modified = true
		}
	}

	if !modified {
		return nil
	}

	settings["permissions"] = perms
	newData, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, newData, 0644)
}

// normalizeAndDedupeSlice normalizes and deduplicates a slice of permissions.
func normalizeAndDedupeSlice(perms []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(perms))
	for _, p := range perms {
		normalized := NormalizePermission(p)
		if !seen[normalized] {
			seen[normalized] = true
			result = append(result, normalized)
		}
	}
	if len(result) == 0 {
		return []string{}
	}
	return result
}

// slicesEqual checks if two string slices are equal.
func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mergeUnique merges two string slices, returning a deduplicated result.
// Preserves order: existing entries first, then new entries not in existing.
// Permissions are normalized to convert legacy formats (e.g., "Bash(ls:*)" -> "Bash(ls *)").
func mergeUnique(existing, additions []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(existing)+len(additions))

	// Add existing entries (deduplicated and normalized)
	for _, s := range existing {
		normalized := NormalizePermission(s)
		if !seen[normalized] {
			seen[normalized] = true
			result = append(result, normalized)
		}
	}

	// Add new entries not already present (normalized)
	for _, s := range additions {
		normalized := NormalizePermission(s)
		if !seen[normalized] {
			seen[normalized] = true
			result = append(result, normalized)
		}
	}

	// Ensure non-nil for JSON marshaling
	if result == nil {
		return []string{}
	}
	return result
}
