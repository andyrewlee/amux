package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// readAssistantsSection decodes the "assistants" object of a config file on
// disk so tests can assert on exactly what saveAssistants wrote, independent
// of the tolerant read path.
func readAssistantsSection(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("Unmarshal config %q error = %v", path, err)
	}
	assistants, ok := payload["assistants"].(map[string]any)
	if !ok {
		t.Fatalf("config %q missing object assistants section, got %#v", path, payload["assistants"])
	}
	return assistants
}

func TestSaveAssistantsWritesCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	assistants := map[string]AssistantConfig{
		"claude": {Command: "claude --resume", InterruptCount: 2, InterruptDelayMs: 200},
		"mytool": {Command: "mytool --serve"},
	}

	if err := saveAssistants(path, assistants); err != nil {
		t.Fatalf("saveAssistants() error = %v", err)
	}

	section := readAssistantsSection(t, path)
	claude, ok := section["claude"].(map[string]any)
	if !ok {
		t.Fatalf("assistants.claude missing or wrong type, got %#v", section["claude"])
	}
	if claude["command"] != "claude --resume" {
		t.Errorf("assistants.claude.command = %#v, want %q", claude["command"], "claude --resume")
	}
	if got, ok := claude["interrupt_count"].(float64); !ok || got != 2 {
		t.Errorf("assistants.claude.interrupt_count = %#v, want 2", claude["interrupt_count"])
	}
	if got, ok := claude["interrupt_delay_ms"].(float64); !ok || got != 200 {
		t.Errorf("assistants.claude.interrupt_delay_ms = %#v, want 200", claude["interrupt_delay_ms"])
	}

	mytool, ok := section["mytool"].(map[string]any)
	if !ok {
		t.Fatalf("assistants.mytool missing or wrong type, got %#v", section["mytool"])
	}
	if mytool["command"] != "mytool --serve" {
		t.Errorf("assistants.mytool.command = %#v, want %q", mytool["command"], "mytool --serve")
	}
	// Zero-value interrupt fields are omitted rather than written as 0, since
	// applyAssistantOverrides treats a present-but-zero InterruptCount as
	// invalid and would otherwise coerce it back to 1 on next read anyway.
	if _, present := mytool["interrupt_count"]; present {
		t.Errorf("assistants.mytool.interrupt_count = %#v, want omitted", mytool["interrupt_count"])
	}
	if _, present := mytool["interrupt_delay_ms"]; present {
		t.Errorf("assistants.mytool.interrupt_delay_ms = %#v, want omitted", mytool["interrupt_delay_ms"])
	}

	// What we wrote must round-trip back through the read path.
	file, err := readConfigFile(path)
	if err != nil {
		t.Fatalf("readConfigFile() error = %v", err)
	}
	got := defaultAssistants()
	applyAssistantOverrides(got, file.Assistants)
	if got["claude"].Command != "claude --resume" {
		t.Errorf("round-trip claude command = %q, want %q", got["claude"].Command, "claude --resume")
	}
	if got["mytool"].Command != "mytool --serve" {
		t.Errorf("round-trip mytool command = %q, want %q", got["mytool"].Command, "mytool --serve")
	}
}

func TestSaveAssistantsCreatesParentDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "c", "config.json")

	if err := saveAssistants(path, map[string]AssistantConfig{"claude": {Command: "claude"}}); err != nil {
		t.Fatalf("saveAssistants() error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected config file at %q, stat error = %v", path, err)
	}
}

func TestSaveAssistantsPreservesUnrelatedSections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	existing := `{
  "ui": {
    "theme": "nord"
  },
  "custom_top_level": 42
}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := saveAssistants(path, map[string]AssistantConfig{"claude": {Command: "claude --new"}}); err != nil {
		t.Fatalf("saveAssistants() error = %v", err)
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
	ui, ok := payload["ui"].(map[string]any)
	if !ok || ui["theme"] != "nord" {
		t.Errorf("ui section dropped or changed, got %#v", payload["ui"])
	}
}

func TestSaveAssistantsOverwritesExistingAssistantsSection(t *testing.T) {
	// Unlike the "ui" section (merged key-by-key), the caller's map is always
	// the complete current roster, so the whole "assistants" object is
	// replaced rather than merged with what's on disk.
	path := filepath.Join(t.TempDir(), "config.json")
	existing := `{
  "assistants": {
    "stale": {"command": "stale-cmd"}
  }
}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := saveAssistants(path, map[string]AssistantConfig{"claude": {Command: "claude"}}); err != nil {
		t.Fatalf("saveAssistants() error = %v", err)
	}

	section := readAssistantsSection(t, path)
	if _, present := section["stale"]; present {
		t.Errorf("expected stale assistant entry to be replaced, got %#v", section["stale"])
	}
	if _, present := section["claude"]; !present {
		t.Errorf("expected claude assistant entry to be written, got %#v", section)
	}
}

func TestSaveAssistantsRefusesMalformedExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := []byte("}{ not json at all")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := saveAssistants(path, map[string]AssistantConfig{"claude": {Command: "claude"}}); err == nil {
		t.Fatal("saveAssistants() error = nil, want non-nil for malformed existing config")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != string(original) {
		t.Fatalf("config file was modified despite malformed JSON: got %q want %q", got, original)
	}
}

func TestSaveAssistantsReturnsErrorWhenPathParentIsFile(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	path := filepath.Join(blocker, "config.json")

	if err := saveAssistants(path, map[string]AssistantConfig{"claude": {Command: "claude"}}); err == nil {
		t.Fatal("saveAssistants() error = nil, want non-nil when parent is a file")
	}
}

func TestConfigSaveAssistants(t *testing.T) {
	t.Run("nil receiver is a no-op", func(t *testing.T) {
		var c *Config
		if err := c.SaveAssistants(); err != nil {
			t.Fatalf("(*Config)(nil).SaveAssistants() error = %v, want nil", err)
		}
	})

	t.Run("nil Paths is a no-op", func(t *testing.T) {
		c := &Config{Assistants: map[string]AssistantConfig{"claude": {Command: "claude"}}}
		if err := c.SaveAssistants(); err != nil {
			t.Fatalf("SaveAssistants() with nil Paths error = %v, want nil", err)
		}
	})

	t.Run("persists in-memory Assistants to ConfigPath", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		c := &Config{
			Paths: &Paths{ConfigPath: path},
			Assistants: map[string]AssistantConfig{
				"claude": {Command: "claude --resume", InterruptCount: 2, InterruptDelayMs: 200},
			},
		}
		if err := c.SaveAssistants(); err != nil {
			t.Fatalf("SaveAssistants() error = %v", err)
		}

		file, err := readConfigFile(path)
		if err != nil {
			t.Fatalf("readConfigFile() error = %v", err)
		}
		got := defaultAssistants()
		applyAssistantOverrides(got, file.Assistants)
		if got["claude"].Command != "claude --resume" {
			t.Errorf("persisted claude command = %q, want %q", got["claude"].Command, "claude --resume")
		}
	})

	t.Run("propagates underlying write error", func(t *testing.T) {
		dir := t.TempDir()
		blocker := filepath.Join(dir, "blocker")
		if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		c := &Config{
			Paths:      &Paths{ConfigPath: filepath.Join(blocker, "config.json")},
			Assistants: map[string]AssistantConfig{"claude": {Command: "claude"}},
		}
		if err := c.SaveAssistants(); err == nil {
			t.Fatal("SaveAssistants() error = nil, want non-nil")
		}
	})
}
