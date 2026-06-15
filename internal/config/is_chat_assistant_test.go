package config

import "testing"

// TestIsChatAssistant pins the agreed chat-classification fallback so activity
// detection (app.tabSessionInfoByName) and center rendering (center.assistantIsChat)
// stay in lockstep. The empty-config and registered-but-not-in-config cases are
// exactly where the previous three divergent implementations disagreed.
func TestIsChatAssistant(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *Config
		assistant string
		want      bool
	}{
		{
			name:      "nil config falls back to registry: registered agent is chat",
			cfg:       nil,
			assistant: "claude",
			want:      true,
		},
		{
			name:      "nil config falls back to registry: unknown agent is not chat",
			cfg:       nil,
			assistant: "totally-unknown",
			want:      false,
		},
		{
			name:      "empty config falls back to registry: registered agent is chat",
			cfg:       &Config{Assistants: map[string]AssistantConfig{}},
			assistant: "codex",
			want:      true,
		},
		{
			name:      "empty config falls back to registry: unknown agent is not chat",
			cfg:       &Config{Assistants: map[string]AssistantConfig{}},
			assistant: "totally-unknown",
			want:      false,
		},
		{
			name: "non-empty config: present assistant is chat even if unregistered",
			cfg: &Config{Assistants: map[string]AssistantConfig{
				"custom-bot": {Command: "custom"},
			}},
			assistant: "custom-bot",
			want:      true,
		},
		{
			name: "non-empty config: registered agent absent from config is not chat",
			cfg: &Config{Assistants: map[string]AssistantConfig{
				"custom-bot": {Command: "custom"},
			}},
			assistant: "claude",
			want:      false,
		},
		{
			name: "non-empty config: assistant present alongside others is chat",
			cfg: &Config{Assistants: map[string]AssistantConfig{
				"claude": {Command: "claude"},
				"codex":  {Command: "codex"},
			}},
			assistant: "codex",
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.IsChatAssistant(tt.assistant); got != tt.want {
				t.Fatalf("IsChatAssistant(%q) = %v, want %v", tt.assistant, got, tt.want)
			}
		})
	}
}
