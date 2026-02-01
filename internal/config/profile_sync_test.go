package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func setupTestProfiles(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	// Create shared dirs
	os.MkdirAll(filepath.Join(root, "shared", "skills"), 0755)
	os.MkdirAll(filepath.Join(root, "shared", "plugins"), 0755)
	return root
}

func TestSyncFreshProfile(t *testing.T) {
	root := setupTestProfiles(t)
	profileDir := filepath.Join(root, "myprofile")
	os.MkdirAll(profileDir, 0755)

	if err := SyncProfileSharedDirs(root, "myprofile"); err != nil {
		t.Fatalf("SyncProfileSharedDirs: %v", err)
	}

	for _, name := range []string{"skills", "plugins"} {
		link := filepath.Join(profileDir, name)
		fi, err := os.Lstat(link)
		if err != nil {
			t.Fatalf("Lstat %s: %v", name, err)
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s should be a symlink", name)
		}
		dest, err := os.Readlink(link)
		if err != nil {
			t.Fatalf("Readlink %s: %v", name, err)
		}
		expected := filepath.Join("..", "shared", name)
		if dest != expected {
			t.Errorf("symlink %s points to %q, want %q", name, dest, expected)
		}
	}
}

func TestSyncExistingDirsGetBackup(t *testing.T) {
	root := setupTestProfiles(t)
	profileDir := filepath.Join(root, "myprofile")

	// Create existing skills and plugins dirs with a file inside
	for _, name := range []string{"skills", "plugins"} {
		dir := filepath.Join(profileDir, name)
		os.MkdirAll(dir, 0755)
		os.WriteFile(filepath.Join(dir, "test.txt"), []byte("data"), 0644)
	}

	if err := SyncProfileSharedDirs(root, "myprofile"); err != nil {
		t.Fatalf("SyncProfileSharedDirs: %v", err)
	}

	for _, name := range []string{"skills", "plugins"} {
		// Original should now be a symlink
		link := filepath.Join(profileDir, name)
		fi, err := os.Lstat(link)
		if err != nil {
			t.Fatalf("Lstat %s: %v", name, err)
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s should be a symlink", name)
		}

		// Backup should exist with the file inside
		backup := filepath.Join(profileDir, name+"_backup")
		data, err := os.ReadFile(filepath.Join(backup, "test.txt"))
		if err != nil {
			t.Fatalf("ReadFile %s_backup/test.txt: %v", name, err)
		}
		if string(data) != "data" {
			t.Errorf("backup file content = %q, want %q", string(data), "data")
		}
	}
}

func TestSyncIdempotent(t *testing.T) {
	root := setupTestProfiles(t)
	profileDir := filepath.Join(root, "myprofile")
	os.MkdirAll(profileDir, 0755)

	// Sync twice
	if err := SyncProfileSharedDirs(root, "myprofile"); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if err := SyncProfileSharedDirs(root, "myprofile"); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	for _, name := range []string{"skills", "plugins"} {
		link := filepath.Join(profileDir, name)
		fi, err := os.Lstat(link)
		if err != nil {
			t.Fatalf("Lstat %s: %v", name, err)
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s should be a symlink after double sync", name)
		}
	}
}

func TestSyncAllProfilesSkipsShared(t *testing.T) {
	root := setupTestProfiles(t)

	// Create multiple profile dirs
	for _, name := range []string{"profile1", "profile2"} {
		os.MkdirAll(filepath.Join(root, name), 0755)
	}

	if err := SyncAllProfiles(root); err != nil {
		t.Fatalf("SyncAllProfiles: %v", err)
	}

	// Verify both profiles have symlinks
	for _, profile := range []string{"profile1", "profile2"} {
		for _, name := range []string{"skills", "plugins"} {
			link := filepath.Join(root, profile, name)
			fi, err := os.Lstat(link)
			if err != nil {
				t.Fatalf("Lstat %s/%s: %v", profile, name, err)
			}
			if fi.Mode()&os.ModeSymlink == 0 {
				t.Errorf("%s/%s should be a symlink", profile, name)
			}
		}
	}

	// Verify shared dir was NOT modified with symlinks inside it
	for _, name := range []string{"skills", "plugins"} {
		sharedDir := filepath.Join(root, "shared", name)
		fi, err := os.Lstat(sharedDir)
		if err != nil {
			t.Fatalf("Lstat shared/%s: %v", name, err)
		}
		if !fi.IsDir() {
			t.Errorf("shared/%s should be a regular directory", name)
		}
	}
}

func TestUnsyncRemovesSymlinksRestoresBackup(t *testing.T) {
	root := setupTestProfiles(t)
	profileDir := filepath.Join(root, "myprofile")

	// Create existing dirs with files, then sync
	for _, name := range []string{"skills", "plugins"} {
		dir := filepath.Join(profileDir, name)
		os.MkdirAll(dir, 0755)
		os.WriteFile(filepath.Join(dir, "test.txt"), []byte("original"), 0644)
	}
	if err := SyncProfileSharedDirs(root, "myprofile"); err != nil {
		t.Fatalf("SyncProfileSharedDirs: %v", err)
	}

	// Unsync
	if err := UnsyncProfileSharedDirs(root, "myprofile"); err != nil {
		t.Fatalf("UnsyncProfileSharedDirs: %v", err)
	}

	for _, name := range []string{"skills", "plugins"} {
		target := filepath.Join(profileDir, name)
		fi, err := os.Lstat(target)
		if err != nil {
			t.Fatalf("Lstat %s: %v", name, err)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			t.Errorf("%s should not be a symlink after unsync", name)
		}
		if !fi.IsDir() {
			t.Errorf("%s should be a directory after unsync", name)
		}

		// Original file should be restored
		data, err := os.ReadFile(filepath.Join(target, "test.txt"))
		if err != nil {
			t.Fatalf("ReadFile %s/test.txt: %v", name, err)
		}
		if string(data) != "original" {
			t.Errorf("restored file content = %q, want %q", string(data), "original")
		}

		// Backup should no longer exist
		backup := filepath.Join(profileDir, name+"_backup")
		if _, err := os.Stat(backup); !os.IsNotExist(err) {
			t.Errorf("%s_backup should not exist after unsync", name)
		}
	}
}

func TestUnsyncLeavesSharedIntact(t *testing.T) {
	root := setupTestProfiles(t)
	profileDir := filepath.Join(root, "myprofile")
	os.MkdirAll(profileDir, 0755)

	// Add files to shared dirs
	for _, name := range []string{"skills", "plugins"} {
		os.WriteFile(filepath.Join(root, "shared", name, "shared.txt"), []byte("shared"), 0644)
	}

	if err := SyncProfileSharedDirs(root, "myprofile"); err != nil {
		t.Fatalf("SyncProfileSharedDirs: %v", err)
	}
	if err := UnsyncProfileSharedDirs(root, "myprofile"); err != nil {
		t.Fatalf("UnsyncProfileSharedDirs: %v", err)
	}

	// Shared dirs and files should still exist
	for _, name := range []string{"skills", "plugins"} {
		data, err := os.ReadFile(filepath.Join(root, "shared", name, "shared.txt"))
		if err != nil {
			t.Fatalf("ReadFile shared/%s/shared.txt: %v", name, err)
		}
		if string(data) != "shared" {
			t.Errorf("shared file content = %q, want %q", string(data), "shared")
		}
	}
}

func TestUnsyncAllProfiles(t *testing.T) {
	root := setupTestProfiles(t)

	// Create and sync multiple profiles
	for _, name := range []string{"profile1", "profile2"} {
		os.MkdirAll(filepath.Join(root, name), 0755)
	}
	if err := SyncAllProfiles(root); err != nil {
		t.Fatalf("SyncAllProfiles: %v", err)
	}

	// Unsync all
	if err := UnsyncAllProfiles(root); err != nil {
		t.Fatalf("UnsyncAllProfiles: %v", err)
	}

	// Verify symlinks are removed
	for _, profile := range []string{"profile1", "profile2"} {
		for _, name := range []string{"skills", "plugins"} {
			link := filepath.Join(root, profile, name)
			_, err := os.Lstat(link)
			if err == nil {
				t.Errorf("%s/%s should not exist after unsync (no backup to restore)", profile, name)
			}
		}
	}
}

func TestSyncPropagatesEnabledPlugins(t *testing.T) {
	root := setupTestProfiles(t)

	// Write an installed_plugins.json to shared/plugins
	registry := map[string]any{
		"version": 2,
		"plugins": map[string]any{
			"context7@official":  []any{map[string]any{"scope": "user"}},
			"github@official":    []any{map[string]any{"scope": "user"}},
			"my-skill@official":  []any{map[string]any{"scope": "user"}},
		},
	}
	data, _ := json.Marshal(registry)
	os.WriteFile(filepath.Join(root, "shared", "plugins", "installed_plugins.json"), data, 0644)

	// Create a profile with no settings.json and sync it
	os.MkdirAll(filepath.Join(root, "work"), 0755)
	if err := SyncProfileSharedDirs(root, "work"); err != nil {
		t.Fatalf("SyncProfileSharedDirs: %v", err)
	}

	// Read the profile's settings.json
	settingsData, err := os.ReadFile(filepath.Join(root, "work", "settings.json"))
	if err != nil {
		t.Fatalf("settings.json should have been created: %v", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(settingsData, &settings); err != nil {
		t.Fatalf("unmarshal settings.json: %v", err)
	}

	enabled, ok := settings["enabledPlugins"].(map[string]any)
	if !ok {
		t.Fatalf("enabledPlugins missing or wrong type")
	}

	for _, key := range []string{"context7@official", "github@official", "my-skill@official"} {
		val, exists := enabled[key]
		if !exists {
			t.Errorf("enabledPlugins missing key %q", key)
			continue
		}
		if val != true {
			t.Errorf("enabledPlugins[%q] = %v, want true", key, val)
		}
	}
}

func TestSyncPreservesExistingEnabledPlugins(t *testing.T) {
	root := setupTestProfiles(t)

	// Write installed_plugins.json with two plugins
	registry := map[string]any{
		"version": 2,
		"plugins": map[string]any{
			"pluginA@official": []any{map[string]any{"scope": "user"}},
			"pluginB@official": []any{map[string]any{"scope": "user"}},
		},
	}
	data, _ := json.Marshal(registry)
	os.WriteFile(filepath.Join(root, "shared", "plugins", "installed_plugins.json"), data, 0644)

	// Create a profile with an existing settings.json that has pluginA disabled
	profileDir := filepath.Join(root, "existing")
	os.MkdirAll(profileDir, 0755)
	existingSettings := map[string]any{
		"enabledPlugins": map[string]any{
			"pluginA@official": false,
		},
		"otherSetting": "preserved",
	}
	settingsData, _ := json.Marshal(existingSettings)
	os.WriteFile(filepath.Join(profileDir, "settings.json"), settingsData, 0644)

	if err := SyncProfileSharedDirs(root, "existing"); err != nil {
		t.Fatalf("SyncProfileSharedDirs: %v", err)
	}

	// Read back settings.json
	result, _ := os.ReadFile(filepath.Join(profileDir, "settings.json"))
	var settings map[string]any
	json.Unmarshal(result, &settings)

	enabled := settings["enabledPlugins"].(map[string]any)

	// pluginA should remain false (existing value preserved)
	if enabled["pluginA@official"] != false {
		t.Errorf("pluginA should remain false (existing value preserved), got %v", enabled["pluginA@official"])
	}

	// pluginB should be added as true
	if enabled["pluginB@official"] != true {
		t.Errorf("pluginB should be true (newly added), got %v", enabled["pluginB@official"])
	}

	// Other settings should be preserved
	if settings["otherSetting"] != "preserved" {
		t.Errorf("otherSetting should be preserved, got %v", settings["otherSetting"])
	}
}

func TestSyncRemovesUninstalledPlugins(t *testing.T) {
	root := setupTestProfiles(t)

	// Install two plugins initially
	registry := map[string]any{
		"version": 2,
		"plugins": map[string]any{
			"pluginA@official": []any{map[string]any{"scope": "user"}},
			"pluginB@official": []any{map[string]any{"scope": "user"}},
		},
	}
	data, _ := json.Marshal(registry)
	os.WriteFile(filepath.Join(root, "shared", "plugins", "installed_plugins.json"), data, 0644)

	// Sync the profile — both plugins get enabled
	profileDir := filepath.Join(root, "myprofile")
	os.MkdirAll(profileDir, 0755)
	if err := SyncProfileSharedDirs(root, "myprofile"); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	// Verify both are enabled
	settingsData, _ := os.ReadFile(filepath.Join(profileDir, "settings.json"))
	var settings map[string]any
	json.Unmarshal(settingsData, &settings)
	enabled := settings["enabledPlugins"].(map[string]any)
	if len(enabled) != 2 {
		t.Fatalf("expected 2 enabled plugins, got %d", len(enabled))
	}

	// Simulate uninstalling pluginB — remove it from installed_plugins.json
	registry["plugins"] = map[string]any{
		"pluginA@official": []any{map[string]any{"scope": "user"}},
	}
	data, _ = json.Marshal(registry)
	os.WriteFile(filepath.Join(root, "shared", "plugins", "installed_plugins.json"), data, 0644)

	// Re-sync
	if err := SyncProfileSharedDirs(root, "myprofile"); err != nil {
		t.Fatalf("re-sync: %v", err)
	}

	// pluginB should be gone from enabledPlugins
	settingsData, _ = os.ReadFile(filepath.Join(profileDir, "settings.json"))
	json.Unmarshal(settingsData, &settings)
	enabled = settings["enabledPlugins"].(map[string]any)

	if _, exists := enabled["pluginA@official"]; !exists {
		t.Errorf("pluginA should still be enabled")
	}
	if _, exists := enabled["pluginB@official"]; exists {
		t.Errorf("pluginB should have been removed after uninstall")
	}
}
