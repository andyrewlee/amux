package computer

import (
	"strings"
	"testing"
)

func TestSetupCredentials_ModeNone(t *testing.T) {
	mock := NewMockRemoteComputer("test-123")

	cfg := CredentialsConfig{
		Mode:  "none",
		Agent: AgentClaude,
	}

	err := SetupCredentials(mock, cfg, false)
	if err != nil {
		t.Errorf("SetupCredentials() with mode=none error = %v", err)
	}

	// Should not have executed any commands
	history := mock.GetExecHistory()
	if len(history) > 0 {
		t.Errorf("SetupCredentials() with mode=none should not execute commands, got %d", len(history))
	}
}

func TestSetupCredentials_CreatesDirectories(t *testing.T) {
	mock := NewMockRemoteComputer("test-123")
	mock.SetHomeDir("/home/testuser")

	cfg := CredentialsConfig{
		Mode:  "computer",
		Agent: AgentClaude,
	}

	err := SetupCredentials(mock, cfg, false)
	if err != nil {
		t.Errorf("SetupCredentials() error = %v", err)
	}

	// Should have executed mkdir commands
	history := mock.GetExecHistory()
	foundMkdir := false
	for _, cmd := range history {
		if strings.Contains(cmd, "mkdir") && strings.Contains(cmd, ".claude") {
			foundMkdir = true
			break
		}
	}

	if !foundMkdir {
		t.Error("SetupCredentials() should create .claude directory")
	}
}

func TestSetupCredentials_AllAgents(t *testing.T) {
	agents := []Agent{
		AgentClaude,
		AgentCodex,
		AgentOpenCode,
		AgentAmp,
		AgentGemini,
		AgentDroid,
	}

	for _, agent := range agents {
		t.Run(string(agent), func(t *testing.T) {
			mock := NewMockRemoteComputer("test-123")
			mock.SetHomeDir("/home/testuser")

			cfg := CredentialsConfig{
				Mode:  "computer",
				Agent: agent,
			}

			err := SetupCredentials(mock, cfg, false)
			if err != nil {
				t.Errorf("SetupCredentials() for %s error = %v", agent, err)
			}

			// Should have executed some mkdir commands
			history := mock.GetExecHistory()
			if len(history) == 0 {
				t.Errorf("SetupCredentials() for %s should execute commands", agent)
			}
		})
	}
}

func TestCheckAgentCredentials(t *testing.T) {
	tests := []struct {
		name        string
		agent       Agent
		setupExec   map[string]MockExecResult
		wantHasCred bool
	}{
		{
			name:  "claude with credentials",
			agent: AgentClaude,
			setupExec: map[string]MockExecResult{
				"test -f": {Output: "", ExitCode: 0},
			},
			wantHasCred: true,
		},
		{
			name:  "claude without credentials",
			agent: AgentClaude,
			setupExec: map[string]MockExecResult{
				"test -f": {Output: "", ExitCode: 1},
			},
			wantHasCred: false,
		},
		{
			name:  "codex with credentials",
			agent: AgentCodex,
			setupExec: map[string]MockExecResult{
				"test -f": {Output: "", ExitCode: 0},
			},
			wantHasCred: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockRemoteComputer("test-123")
			mock.SetHomeDir("/home/testuser")

			for prefix, result := range tt.setupExec {
				mock.SetExecResult(prefix, result.Output, result.ExitCode)
			}

			status := CheckAgentCredentials(mock, tt.agent)

			if status.HasCredential != tt.wantHasCred {
				t.Errorf("CheckAgentCredentials() HasCredential = %v, want %v", status.HasCredential, tt.wantHasCred)
			}

			if status.Agent != tt.agent {
				t.Errorf("CheckAgentCredentials() Agent = %v, want %v", status.Agent, tt.agent)
			}
		})
	}
}

func TestHasGitHubCredentials(t *testing.T) {
	// Test with authenticated user
	t.Run("gh authenticated", func(t *testing.T) {
		mock := NewMockRemoteComputer("test-123")
		// Default mock returns exit code 0 for all commands
		got := HasGitHubCredentials(mock)
		if !got {
			t.Error("HasGitHubCredentials() should return true when gh auth succeeds")
		}
	})
}
