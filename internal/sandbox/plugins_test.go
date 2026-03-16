package sandbox

import (
	"testing"
)

func TestAgentRegistry_GetPlugin(t *testing.T) {
	agents := []string{
		"claude",
		"codex",
		"opencode",
		"amp",
		"gemini",
		"droid",
	}

	for _, agent := range agents {
		t.Run(agent, func(t *testing.T) {
			plugin, ok := GetAgentPlugin(agent)
			if !ok {
				t.Errorf("GetAgentPlugin(%s) returned false", agent)
				return
			}
			if plugin == nil {
				t.Errorf("GetAgentPlugin(%s) returned nil", agent)
				return
			}

			if plugin.Name() != agent {
				t.Errorf("GetAgentPlugin(%s).Name() = %s, want %s", agent, plugin.Name(), agent)
			}
		})
	}
}

func TestAgentRegistry_UnknownAgent(t *testing.T) {
	plugin, ok := GetAgentPlugin("unknown")
	if ok {
		t.Error("GetAgentPlugin(unknown) should return false")
	}
	if plugin != nil {
		t.Error("GetAgentPlugin(unknown) should return nil")
	}
}

func TestAgentRegistry_ShellAgent(t *testing.T) {
	// Shell IS a valid plugin (provides bash shell access)
	plugin, ok := GetAgentPlugin("shell")
	if !ok {
		t.Error("GetAgentPlugin(shell) should return true")
	}
	if plugin == nil {
		t.Error("GetAgentPlugin(shell) should not return nil")
	}
	if plugin != nil && plugin.Name() != "shell" {
		t.Errorf("GetAgentPlugin(shell).Name() = %s, want shell", plugin.Name())
	}
}

func TestAgentPlugin_CredentialPaths(t *testing.T) {
	agents := []string{
		"claude",
		"codex",
		"opencode",
		"amp",
		"gemini",
		"droid",
	}

	for _, agent := range agents {
		t.Run(agent, func(t *testing.T) {
			plugin, ok := GetAgentPlugin(agent)
			if !ok {
				t.Fatalf("GetAgentPlugin(%s) returned false", agent)
			}

			paths := plugin.CredentialPaths()
			if len(paths) == 0 {
				t.Errorf("Plugin %s has no credential paths", agent)
			}

			for _, path := range paths {
				if path.HomePath == "" {
					t.Errorf("Plugin %s has empty HomePath", agent)
				}
			}
		})
	}
}

func TestAgentPlugin_InstallMethods(t *testing.T) {
	agents := []string{
		"claude",
		"codex",
		"opencode",
		"amp",
		"gemini",
		"droid",
	}

	for _, agent := range agents {
		t.Run(agent, func(t *testing.T) {
			plugin, ok := GetAgentPlugin(agent)
			if !ok {
				t.Fatalf("GetAgentPlugin(%s) returned false", agent)
			}

			methods := plugin.InstallMethods()
			if len(methods) == 0 {
				t.Errorf("Plugin %s has no install methods", agent)
			}

			for _, method := range methods {
				if method.Type == "" {
					t.Errorf("Plugin %s has empty install type", agent)
				}
			}
		})
	}
}

func TestAgentPlugin_Name(t *testing.T) {
	tests := []struct {
		agent    string
		wantName string
	}{
		{"claude", "claude"},
		{"codex", "codex"},
		{"opencode", "opencode"},
		{"amp", "amp"},
		{"gemini", "gemini"},
		{"droid", "droid"},
	}

	for _, tt := range tests {
		t.Run(tt.agent, func(t *testing.T) {
			plugin, ok := GetAgentPlugin(tt.agent)
			if !ok {
				t.Fatalf("GetAgentPlugin(%s) returned false", tt.agent)
			}

			if plugin.Name() != tt.wantName {
				t.Errorf("Plugin %s Name() = %s, want %s", tt.agent, plugin.Name(), tt.wantName)
			}
		})
	}
}

func TestAgentPlugin_EnvVars(t *testing.T) {
	// Test that plugins have expected env vars
	tests := []struct {
		agent       string
		expectedEnv string
	}{
		{"claude", "ANTHROPIC_API_KEY"},
		{"codex", "OPENAI_API_KEY"},
		{"gemini", "GEMINI_API_KEY"},
	}

	for _, tt := range tests {
		t.Run(tt.agent, func(t *testing.T) {
			plugin, ok := GetAgentPlugin(tt.agent)
			if !ok {
				t.Fatalf("GetAgentPlugin(%s) returned false", tt.agent)
			}

			envVars := plugin.EnvVars()
			found := false
			for _, env := range envVars {
				if env.Name == tt.expectedEnv {
					found = true
					if !env.Secret {
						t.Errorf("Plugin %s env var %s should be marked as secret", tt.agent, tt.expectedEnv)
					}
					break
				}
			}

			if !found {
				t.Errorf("Plugin %s should have env var %s", tt.agent, tt.expectedEnv)
			}
		})
	}
}

func TestHasCredentials(t *testing.T) {
	// Note: The HasCredentials function checks directories with complex commands
	// like "test -d ... && ls -A ... | head -1"
	// The mock by default returns empty output, which means "no files in dir"
	// so HasCredentials returns false for directory checks

	t.Run("claude without credentials", func(t *testing.T) {
		mock := NewMockRemoteSandbox("test-123")
		mock.SetHomeDir("/home/testuser")
		// Default mock returns empty output, so dir appears empty

		plugin, ok := GetAgentPlugin("claude")
		if !ok {
			t.Fatal("GetAgentPlugin(claude) returned false")
		}

		got := HasCredentials(plugin, mock)
		// With default mock (empty output), credentials are not found
		if got {
			t.Error("HasCredentials() should return false when dir is empty")
		}
	})
}

func TestIsValidAgent(t *testing.T) {
	validAgents := []string{
		"claude", "codex", "opencode", "amp", "gemini", "droid", "shell",
	}

	for _, agent := range validAgents {
		if !IsValidAgent(agent) {
			t.Errorf("IsValidAgent(%s) = false, want true", agent)
		}
	}

	invalidAgents := []string{
		"", "unknown", "Claude", "CLAUDE", "foo",
	}

	for _, agent := range invalidAgents {
		if IsValidAgent(agent) {
			t.Errorf("IsValidAgent(%s) = true, want false", agent)
		}
	}
}

func TestAgent_String(t *testing.T) {
	if AgentClaude.String() != "claude" {
		t.Errorf("AgentClaude.String() = %s, want claude", AgentClaude.String())
	}
}
