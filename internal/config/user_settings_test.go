package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readUISection decodes the "ui" object of a config file on disk so tests can
// assert on exactly what saveUISettings wrote, independent of the read path.
func readUISection(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("Unmarshal config %q error = %v", path, err)
	}
	ui, ok := payload["ui"].(map[string]any)
	if !ok {
		t.Fatalf("config %q missing object ui section, got %#v", path, payload["ui"])
	}
	return ui
}

func TestSaveUISettingsWritesAllFields(t *testing.T) {
	tests := []struct {
		name     string
		settings UISettings
	}{
		{
			name:     "defaults",
			settings: defaultUISettings(),
		},
		{
			name: "fully populated",
			settings: UISettings{
				ShowKeymapHints:  true,
				Theme:            "dracula",
				TmuxServer:       "amux-test",
				TmuxConfigPath:   "/tmp/tmux.conf",
				TmuxSyncInterval: "5s",
				NotifyOnDone:     true,
			},
		},
		{
			name: "empty strings preserved",
			settings: UISettings{
				ShowKeymapHints:  false,
				Theme:            "",
				TmuxServer:       "",
				TmuxConfigPath:   "",
				TmuxSyncInterval: "",
			},
		},
		{
			name: "show hints with empty theme",
			settings: UISettings{
				ShowKeymapHints: true,
				Theme:           "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")

			if err := saveUISettings(path, tt.settings); err != nil {
				t.Fatalf("saveUISettings() error = %v", err)
			}

			ui := readUISection(t, path)
			if got := ui["show_keymap_hints"]; got != tt.settings.ShowKeymapHints {
				t.Errorf("show_keymap_hints = %#v, want %#v", got, tt.settings.ShowKeymapHints)
			}
			if got := ui["theme"]; got != tt.settings.Theme {
				t.Errorf("theme = %#v, want %#v", got, tt.settings.Theme)
			}
			if got := ui["tmux_server"]; got != tt.settings.TmuxServer {
				t.Errorf("tmux_server = %#v, want %#v", got, tt.settings.TmuxServer)
			}
			if got := ui["tmux_config"]; got != tt.settings.TmuxConfigPath {
				t.Errorf("tmux_config = %#v, want %#v", got, tt.settings.TmuxConfigPath)
			}
			if got := ui["tmux_sync_interval"]; got != tt.settings.TmuxSyncInterval {
				t.Errorf("tmux_sync_interval = %#v, want %#v", got, tt.settings.TmuxSyncInterval)
			}
			if got := ui["notify_on_done"]; got != tt.settings.NotifyOnDone {
				t.Errorf("notify_on_done = %#v, want %#v", got, tt.settings.NotifyOnDone)
			}

			// What we wrote must round-trip back through the read path.
			file, err := readConfigFile(path)
			if err != nil {
				t.Fatalf("readConfigFile() error = %v", err)
			}
			got := applyUISettings(defaultUISettings(), file.UI)
			if got != tt.settings {
				t.Errorf("round-trip settings = %+v, want %+v", got, tt.settings)
			}
		})
	}
}

func TestSaveUISettingsCreatesParentDirectories(t *testing.T) {
	// Nested, not-yet-existing directories must be created (MkdirAll path).
	path := filepath.Join(t.TempDir(), "a", "b", "c", "config.json")

	if err := saveUISettings(path, defaultUISettings()); err != nil {
		t.Fatalf("saveUISettings() error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected config file at %q, stat error = %v", path, err)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("expected config parent directory: %v", err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Fatalf("expected config parent to be private, got mode %03o", mode)
	}
}

func TestSaveUISettingsPreservesUnrelatedSections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	existing := `{
  "assistants": {
    "myagent": {"command": "myagent"}
  },
  "custom_top_level": 42
}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := saveUISettings(path, UISettings{Theme: "nord", ShowKeymapHints: true}); err != nil {
		t.Fatalf("saveUISettings() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if got, ok := payload["custom_top_level"].(float64); !ok || got != 42 {
		t.Errorf("custom_top_level = %#v, want 42 preserved", payload["custom_top_level"])
	}
	assistants, ok := payload["assistants"].(map[string]any)
	if !ok {
		t.Fatalf("assistants section dropped, got %#v", payload["assistants"])
	}
	agent, ok := assistants["myagent"].(map[string]any)
	if !ok || agent["command"] != "myagent" {
		t.Errorf("assistants.myagent.command = %#v, want preserved", assistants["myagent"])
	}
	ui, ok := payload["ui"].(map[string]any)
	if !ok {
		t.Fatalf("ui section missing or wrong type, got %#v", payload["ui"])
	}
	if ui["theme"] != "nord" {
		t.Errorf("ui.theme = %#v, want nord", ui["theme"])
	}
}

func TestSaveUISettingsOverwritesExistingUISection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	existing := `{
  "ui": {
    "theme": "stale",
    "tmux_server": "old-server",
    "extra_ui_key": "keepme"
  }
}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := saveUISettings(path, UISettings{Theme: "fresh", TmuxServer: "new-server"}); err != nil {
		t.Fatalf("saveUISettings() error = %v", err)
	}

	ui := readUISection(t, path)
	if ui["theme"] != "fresh" {
		t.Errorf("theme = %#v, want fresh (overwritten)", ui["theme"])
	}
	if ui["tmux_server"] != "new-server" {
		t.Errorf("tmux_server = %#v, want new-server (overwritten)", ui["tmux_server"])
	}
	// Keys the writer does not manage in the existing ui object are retained
	// because the map is merged rather than replaced.
	if ui["extra_ui_key"] != "keepme" {
		t.Errorf("extra_ui_key = %#v, want keepme (merged)", ui["extra_ui_key"])
	}
}

func TestSaveUISettingsRefusesMalformedExistingFile(t *testing.T) {
	// A corrupt existing file must not be overwritten because it may contain
	// hand-edited sections that the tolerant loader skipped.
	path := filepath.Join(t.TempDir(), "config.json")
	original := []byte("}{ not json at all")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	settings := UISettings{Theme: "gruvbox", ShowKeymapHints: true, TmuxSyncInterval: "2s"}
	if err := saveUISettings(path, settings); err == nil {
		t.Fatal("saveUISettings() error = nil, want non-nil for malformed existing config")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != string(original) {
		t.Fatalf("config file was modified despite malformed JSON: got %q want %q", got, original)
	}
}

func TestSaveUISettingsTreatsNonObjectUISectionAsAbsent(t *testing.T) {
	// If "ui" exists but is not an object, the writer must replace it with a
	// fresh object rather than panicking on the type assertion.
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"ui": "not-an-object"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := saveUISettings(path, UISettings{Theme: "solarized"}); err != nil {
		t.Fatalf("saveUISettings() error = %v", err)
	}
	ui := readUISection(t, path)
	if ui["theme"] != "solarized" {
		t.Errorf("theme = %#v, want solarized", ui["theme"])
	}
}

func TestSaveUISettingsReturnsErrorWhenPathParentIsFile(t *testing.T) {
	// MkdirAll fails when an ancestor of the target path is a regular file.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	path := filepath.Join(blocker, "config.json")

	if err := saveUISettings(path, defaultUISettings()); err == nil {
		t.Fatal("saveUISettings() error = nil, want non-nil when parent is a file")
	}
}

func TestSaveUISettingsProducesIndentedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveUISettings(path, defaultUISettings()); err != nil {
		t.Fatalf("saveUISettings() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var indented map[string]any
	if err := json.Unmarshal(data, &indented); err != nil {
		t.Fatalf("written file is not valid JSON: %v", err)
	}
	// MarshalIndent uses two-space indentation; the nested ui object should
	// therefore appear under a four-space prefix.
	if !strings.Contains(string(data), "\n    \"theme\":") {
		t.Errorf("expected indented ui section, got:\n%s", data)
	}
}

func TestConfigSaveUISettings(t *testing.T) {
	t.Run("nil receiver is a no-op", func(t *testing.T) {
		var c *Config
		if err := c.SaveUISettings(); err != nil {
			t.Fatalf("(*Config)(nil).SaveUISettings() error = %v, want nil", err)
		}
	})

	t.Run("nil Paths is a no-op", func(t *testing.T) {
		c := &Config{UI: UISettings{Theme: "x"}}
		if err := c.SaveUISettings(); err != nil {
			t.Fatalf("SaveUISettings() with nil Paths error = %v, want nil", err)
		}
	})

	t.Run("persists in-memory UI to ConfigPath", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		c := &Config{
			Paths: &Paths{ConfigPath: path},
			UI: UISettings{
				ShowKeymapHints:  true,
				Theme:            "monokai",
				TmuxServer:       "amux",
				TmuxConfigPath:   "/etc/tmux.conf",
				TmuxSyncInterval: "10s",
			},
		}
		if err := c.SaveUISettings(); err != nil {
			t.Fatalf("SaveUISettings() error = %v", err)
		}

		// SaveUISettings must persist exactly the receiver's UI field.
		file, err := readConfigFile(path)
		if err != nil {
			t.Fatalf("readConfigFile() error = %v", err)
		}
		if got := applyUISettings(defaultUISettings(), file.UI); got != c.UI {
			t.Errorf("persisted UI = %+v, want %+v", got, c.UI)
		}
	})

	t.Run("propagates underlying write error", func(t *testing.T) {
		// ConfigPath whose parent is a regular file forces saveUISettings to
		// fail; SaveUISettings must surface that error rather than swallow it.
		dir := t.TempDir()
		blocker := filepath.Join(dir, "blocker")
		if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		c := &Config{Paths: &Paths{ConfigPath: filepath.Join(blocker, "config.json")}}
		if err := c.SaveUISettings(); err == nil {
			t.Fatal("SaveUISettings() error = nil, want non-nil")
		}
	})

	t.Run("round-trips through PersistedUISettings", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		c := &Config{
			Paths: &Paths{ConfigPath: path},
			UI:    UISettings{Theme: "nord", TmuxServer: "srv", ShowKeymapHints: true},
		}
		if err := c.SaveUISettings(); err != nil {
			t.Fatalf("SaveUISettings() error = %v", err)
		}
		if got := c.PersistedUISettings(); got != c.UI {
			t.Errorf("PersistedUISettings() = %+v, want %+v", got, c.UI)
		}
	})
}
