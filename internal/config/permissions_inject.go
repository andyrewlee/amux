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
