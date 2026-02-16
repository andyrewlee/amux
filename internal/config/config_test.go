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
  "default_assistant": "openclaw",
  "assistants": {
    "openclaw": {
      "command": "openclaw --fast"
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

	if got := cfg.ResolvedDefaultAssistant(); got != "openclaw" {
		t.Fatalf("ResolvedDefaultAssistant() = %q, want %q", got, "openclaw")
	}
	oc, ok := cfg.Assistants["openclaw"]
	if !ok {
		t.Fatalf("expected openclaw assistant to exist")
	}
	if oc.Command != "openclaw --fast" {
		t.Fatalf("openclaw command = %q, want %q", oc.Command, "openclaw --fast")
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

func TestDefaultConfigInvalidDefaultAssistantFallsBack(t *testing.T) {
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
  "default_assistant": "my agent",
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
