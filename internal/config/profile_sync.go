package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// sharedDirs are the subdirectory names synced between profiles.
var sharedDirs = []string{"skills", "plugins"}

// SyncProfileSharedDirs enables shared plugins/skills for a single profile.
// For each of skills and plugins:
//   - If already a correct symlink to the shared dir, skip.
//   - If a regular directory exists, rename it to {name}_backup.
//   - Create a relative symlink profileDir/{name} -> ../shared/{name}.
func SyncProfileSharedDirs(profilesRoot, profileName string) error {
	sharedRoot := filepath.Join(profilesRoot, "shared")
	profileDir := filepath.Join(profilesRoot, profileName)

	for _, name := range sharedDirs {
		sharedDir := filepath.Join(sharedRoot, name)
		if err := os.MkdirAll(sharedDir, 0755); err != nil {
			return err
		}

		target := filepath.Join(profileDir, name)
		relTarget := filepath.Join("..", "shared", name)

		fi, err := os.Lstat(target)
		if err == nil {
			if fi.Mode()&os.ModeSymlink != 0 {
				// Already a symlink — check if it points to the right place.
				dest, readErr := os.Readlink(target)
				if readErr == nil && dest == relTarget {
					continue // Already correct.
				}
				// Wrong symlink — remove it and recreate.
				_ = os.Remove(target)
			} else if fi.IsDir() {
				// Regular directory — rename to backup.
				backup := filepath.Join(profileDir, name+"_backup")
				if err := os.Rename(target, backup); err != nil {
					return err
				}
			}
		}

		if err := os.Symlink(relTarget, target); err != nil {
			return err
		}
	}

	// Propagate enabledPlugins from the shared installed_plugins.json
	// into this profile's settings.json so the agent activates them.
	syncEnabledPlugins(sharedRoot, profileDir)

	return nil
}

// UnsyncProfileSharedDirs removes shared symlinks for a single profile
// and restores any _backup directories. The shared directory is left intact.
func UnsyncProfileSharedDirs(profilesRoot, profileName string) error {
	profileDir := filepath.Join(profilesRoot, profileName)

	for _, name := range sharedDirs {
		target := filepath.Join(profileDir, name)
		backup := filepath.Join(profileDir, name+"_backup")

		fi, err := os.Lstat(target)
		if err == nil && fi.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(target); err != nil {
				return err
			}
		}

		// Restore backup if it exists.
		if _, err := os.Stat(backup); err == nil {
			if err := os.Rename(backup, target); err != nil {
				return err
			}
		}
	}
	return nil
}

// SyncAllProfiles enables shared plugins/skills for every existing profile,
// skipping the "shared" directory itself.
func SyncAllProfiles(profilesRoot string) error {
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
		if err := SyncProfileSharedDirs(profilesRoot, entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

// UnsyncAllProfiles removes shared symlinks for every existing profile,
// skipping the "shared" directory itself.
func UnsyncAllProfiles(profilesRoot string) error {
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
		if err := UnsyncProfileSharedDirs(profilesRoot, entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

// syncEnabledPlugins reads the shared installed_plugins.json to discover
// which plugins are installed, then ensures the profile's settings.json
// has them listed under enabledPlugins. This is necessary because Claude
// tracks plugin enablement in settings.json (outside the plugins/ dir).
func syncEnabledPlugins(sharedRoot, profileDir string) {
	installedPath := filepath.Join(sharedRoot, "plugins", "installed_plugins.json")
	data, err := os.ReadFile(installedPath)
	if err != nil {
		return // No installed plugins to propagate.
	}

	var registry struct {
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	if err := json.Unmarshal(data, &registry); err != nil || len(registry.Plugins) == 0 {
		return
	}

	// Build the enabledPlugins map from installed plugin keys.
	enabled := make(map[string]bool, len(registry.Plugins))
	for key := range registry.Plugins {
		enabled[key] = true
	}

	// Read existing settings.json (or start fresh).
	settingsPath := filepath.Join(profileDir, "settings.json")
	var settings map[string]any
	if existing, err := os.ReadFile(settingsPath); err == nil {
		_ = json.Unmarshal(existing, &settings)
	}
	if settings == nil {
		settings = make(map[string]any)
	}

	// Merge: preserve any existing enabledPlugins entries (e.g. explicitly
	// disabled plugins) and add newly installed ones.
	existingEnabled, _ := settings["enabledPlugins"].(map[string]any)
	if existingEnabled == nil {
		existingEnabled = make(map[string]any)
	}
	for key := range enabled {
		if _, exists := existingEnabled[key]; !exists {
			existingEnabled[key] = true
		}
	}
	settings["enabledPlugins"] = existingEnabled

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(settingsPath, out, 0644)
}
