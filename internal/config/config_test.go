package config

import "testing"

func TestDefaultConfig(t *testing.T) {
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
	for _, name := range []string{"claude", "codex", "gemini", "amp", "opencode"} {
		if _, ok := cfg.Assistants[name]; !ok {
			t.Fatalf("DefaultConfig() missing assistant config for %s", name)
		}
	}

	if cfg.Layout.MinChatWidth == 0 || cfg.Layout.MinDashboardWidth == 0 {
		t.Fatalf("DefaultConfig() returned invalid layout: %+v", cfg.Layout)
	}
}
