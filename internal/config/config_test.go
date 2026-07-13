package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig() error = %v", err)
	}
	if cfg.Paths == nil {
		t.Fatal("DefaultConfig() returned nil Paths")
	}
	if cfg.PortStart == 0 || cfg.PortRangeSize == 0 {
		t.Fatalf("DefaultConfig() returned invalid ports: start=%d range=%d", cfg.PortStart, cfg.PortRangeSize)
	}

	// Verify assistant configs referenced in README exist.
	for _, name := range []string{"claude", "codex", "gemini", "amp", "opencode", "cline"} {
		if _, ok := cfg.Assistants[name]; !ok {
			t.Fatalf("DefaultConfig() missing assistant config for %s", name)
		}
	}
	if cfg.ResolvedDefaultAssistant() == "" {
		t.Fatal("resolved default assistant should not be empty")
	}
}

func TestDefaultConfigLoadsAssistantOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".amux", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	content := `{
  "assistants": {
    "my-fast-agent": {
      "command": "my-fast-agent --fast"
    },
    "myagent": {
      "command": "myagent",
      "interrupt_count": 3,
      "interrupt_delay_ms": 150
    }
  }
}`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig() error = %v", err)
	}

	if got := cfg.ResolvedDefaultAssistant(); got != "claude" {
		t.Fatalf("ResolvedDefaultAssistant() = %q, want %q", got, "claude")
	}
	customFast, ok := cfg.Assistants["my-fast-agent"]
	if !ok {
		t.Fatalf("expected my-fast-agent assistant to exist")
	}
	if customFast.Command != "my-fast-agent --fast" {
		t.Fatalf("my-fast-agent command = %q, want %q", customFast.Command, "my-fast-agent --fast")
	}

	custom, ok := cfg.Assistants["myagent"]
	if !ok {
		t.Fatalf("expected custom assistant to be loaded")
	}
	if custom.Command != "myagent" {
		t.Fatalf("custom command = %q, want %q", custom.Command, "myagent")
	}
	if custom.InterruptCount != 3 {
		t.Fatalf("custom interrupt_count = %d, want %d", custom.InterruptCount, 3)
	}
	if custom.InterruptDelayMs != 150 {
		t.Fatalf("custom interrupt_delay_ms = %d, want %d", custom.InterruptDelayMs, 150)
	}
}

func TestDefaultConfigKeepsAssistantOverridesWhenUISectionIsInvalid(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".amux", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	content := `{
	  "assistants": {
	    "myagent": {
	      "command": "myagent",
	      "interrupt_count": 3
	    }
	  },
	  "ui": {
	    "show_keymap_hints": "true"
	  }
	}`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig() error = %v", err)
	}

	custom, ok := cfg.Assistants["myagent"]
	if !ok {
		t.Fatalf("expected valid assistant override to survive invalid ui section")
	}
	if custom.Command != "myagent" || custom.InterruptCount != 3 {
		t.Fatalf("custom assistant = %+v, want command myagent and interrupt_count 3", custom)
	}
	if cfg.UI.ShowKeymapHints {
		t.Fatalf("ShowKeymapHints = true, want default false after invalid ui section")
	}
}

func TestDefaultConfigKeepsUISettingsWhenAssistantSectionIsInvalid(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".amux", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	content := `{
	  "assistants": {
	    "myagent": {
	      "command": "myagent",
	      "interrupt_count": "3"
	    }
	  },
	  "ui": {
	    "show_keymap_hints": true,
	    "tmux_server": "amux-test"
	  }
	}`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig() error = %v", err)
	}
	if _, ok := cfg.Assistants["myagent"]; ok {
		t.Fatalf("expected invalid assistant section to be ignored")
	}
	if !cfg.UI.ShowKeymapHints {
		t.Fatalf("ShowKeymapHints = false, want true from valid ui section")
	}
	if cfg.UI.TmuxServer != "amux-test" {
		t.Fatalf("TmuxServer = %q, want %q", cfg.UI.TmuxServer, "amux-test")
	}

	persisted := cfg.PersistedUISettings()
	if !persisted.ShowKeymapHints || persisted.TmuxServer != "amux-test" {
		t.Fatalf("PersistedUISettings() = %+v, want valid ui section despite assistant error", persisted)
	}
}

func TestDefaultConfigIgnoresDefaultAssistantSetting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".amux", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	content := `{"default_assistant":"does-not-exist"}`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig() error = %v", err)
	}

	if got := cfg.ResolvedDefaultAssistant(); got != "claude" {
		t.Fatalf("ResolvedDefaultAssistant() = %q, want %q", got, "claude")
	}
}

func TestDefaultConfigSkipsInvalidAssistantOverrideIDs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".amux", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	content := `{
  "assistants": {
    "my agent": {
      "command": "bad-assistant"
    },
    "ok_agent": {
      "command": "ok-agent"
    }
  }
}`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig() error = %v", err)
	}

	if _, ok := cfg.Assistants["my agent"]; ok {
		t.Fatalf("expected invalid assistant id to be ignored")
	}
	if _, ok := cfg.Assistants["ok_agent"]; !ok {
		t.Fatalf("expected valid assistant id to be loaded")
	}
	if got := cfg.ResolvedDefaultAssistant(); got != "claude" {
		t.Fatalf("ResolvedDefaultAssistant() = %q, want %q", got, "claude")
	}
}

func TestAssistantNamesOrder(t *testing.T) {
	cfg := &Config{
		Assistants: map[string]AssistantConfig{
			"zeta":     {Command: "zeta"},
			"codex":    {Command: "codex"},
			"claude":   {Command: "claude"},
			"my-agent": {Command: "my-agent"},
			"gemini":   {Command: "gemini"},
			"amp":      {Command: "amp"},
			"opencode": {Command: "opencode"},
			"droid":    {Command: "droid"},
			"cline":    {Command: "cline"},
			"cursor":   {Command: "cursor"},
			"pi":       {Command: "pi"},
		},
	}

	got := cfg.AssistantNames()
	wantPrefix := []string{"claude", "codex", "gemini", "amp", "opencode", "droid", "cline", "cursor", "pi"}
	for i, want := range wantPrefix {
		if got[i] != want {
			t.Fatalf("AssistantNames()[%d] = %q, want %q", i, got[i], want)
		}
	}
	if got[len(got)-2] != "my-agent" || got[len(got)-1] != "zeta" {
		t.Fatalf("expected custom assistants to be sorted at end, got %v", got)
	}
}

// TestCanonicalDefaultAssistantDirectMatch confirms the direct-match branch:
// when the candidate normalizes to a name present in the assistants map, it
// is returned as-is.
func TestCanonicalDefaultAssistantDirectMatch(t *testing.T) {
	assistants := map[string]AssistantConfig{
		"claude": {Command: "claude"},
		"codex":  {Command: "codex"},
	}
	if got := canonicalDefaultAssistant("claude", assistants); got != "claude" {
		t.Fatalf("canonicalDefaultAssistant() = %q, want %q", got, "claude")
	}
}

// TestCanonicalDefaultAssistantFallsBackToFallbackConstant covers the second
// branch: the candidate is missing from the map, but fallbackDefaultAssistant
// is present, so it wins over any other ordered entry. This branch is only
// reachable by calling canonicalDefaultAssistant directly with a candidate
// other than fallbackDefaultAssistant -- ResolvedDefaultAssistant always
// passes fallbackDefaultAssistant as the candidate, so it can't isolate this
// case from the direct-match branch above.
func TestCanonicalDefaultAssistantFallsBackToFallbackConstant(t *testing.T) {
	assistants := map[string]AssistantConfig{
		fallbackDefaultAssistant: {Command: fallbackDefaultAssistant},
		"codex":                  {Command: "codex"},
	}
	if got := canonicalDefaultAssistant("unknown-agent", assistants); got != fallbackDefaultAssistant {
		t.Fatalf("canonicalDefaultAssistant() = %q, want %q", got, fallbackDefaultAssistant)
	}
}

// TestResolvedDefaultAssistantFallsBackToFirstOrdered covers the third
// branch: fallbackDefaultAssistant itself is absent from the map, so
// resolution falls through to the first name in preferred display order.
func TestResolvedDefaultAssistantFallsBackToFirstOrdered(t *testing.T) {
	assistants := map[string]AssistantConfig{
		"codex":  {Command: "codex"},
		"gemini": {Command: "gemini"},
	}
	cfg := &Config{Assistants: assistants}

	want := orderedAssistantNames(assistants)
	if len(want) == 0 {
		t.Fatal("orderedAssistantNames() returned no names; test fixture invalid")
	}
	if got := cfg.ResolvedDefaultAssistant(); got != want[0] {
		t.Fatalf("ResolvedDefaultAssistant() = %q, want %q", got, want[0])
	}
	if got := cfg.ResolvedDefaultAssistant(); got == fallbackDefaultAssistant {
		t.Fatalf("ResolvedDefaultAssistant() = %q, should not equal omitted fallback %q", got, fallbackDefaultAssistant)
	}
}

// TestResolvedDefaultAssistantFallsBackToFallbackConstantWhenAssistantsEmpty
// covers the final branch: an empty/nil assistants map has no candidates at
// all, so resolution returns the fallbackDefaultAssistant constant verbatim.
func TestResolvedDefaultAssistantFallsBackToFallbackConstantWhenAssistantsEmpty(t *testing.T) {
	cfg := &Config{Assistants: nil}
	if got := cfg.ResolvedDefaultAssistant(); got != fallbackDefaultAssistant {
		t.Fatalf("ResolvedDefaultAssistant() = %q, want %q", got, fallbackDefaultAssistant)
	}

	cfg = &Config{Assistants: map[string]AssistantConfig{}}
	if got := cfg.ResolvedDefaultAssistant(); got != fallbackDefaultAssistant {
		t.Fatalf("ResolvedDefaultAssistant() with empty map = %q, want %q", got, fallbackDefaultAssistant)
	}
}
