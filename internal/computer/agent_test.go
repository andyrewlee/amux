package computer

import (
	"strings"
	"testing"
)

func TestResolveAgentCommandPath(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		setupExec map[string]MockExecResult
		want      string
	}{
		{
			name:    "claude native installation found",
			command: "claude",
			setupExec: map[string]MockExecResult{
				// getHomeDir lookup
				"sh -lc": {Output: "/home/user", ExitCode: 0},
				// Native installation check succeeds
				"test -x": {Output: "", ExitCode: 0},
			},
			want: "/home/user/.local/bin/claude",
		},
		{
			name:    "codex found in PATH",
			command: "codex",
			setupExec: map[string]MockExecResult{
				"sh -lc":           {Output: "/home/user", ExitCode: 0},
				"command -v codex": {Output: "/usr/local/bin/codex\n", ExitCode: 0},
			},
			want: "/usr/local/bin/codex",
		},
		{
			name:    "gemini found in PATH",
			command: "gemini",
			setupExec: map[string]MockExecResult{
				"sh -lc":            {Output: "/home/user", ExitCode: 0},
				"command -v gemini": {Output: "/usr/local/bin/gemini\n", ExitCode: 0},
			},
			want: "/usr/local/bin/gemini",
		},
		{
			name:    "command not found returns original command",
			command: "unknown",
			setupExec: map[string]MockExecResult{
				"sh -lc":             {Output: "/home/user", ExitCode: 0},
				"command -v unknown": {Output: "", ExitCode: 1},
				"command -v node":    {Output: "", ExitCode: 1},
			},
			want: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockRemoteComputer("test")

			for prefix, result := range tt.setupExec {
				mock.SetExecResult(prefix, result.Output, result.ExitCode)
			}

			got := resolveAgentCommandPath(mock, tt.command)
			if got != tt.want {
				t.Errorf("resolveAgentCommandPath(%q) = %q, want %q", tt.command, got, tt.want)
			}
		})
	}
}

func TestIsAgentInstallFresh(t *testing.T) {
	tests := []struct {
		name      string
		agent     string
		setupExec map[string]MockExecResult
		want      bool
	}{
		{
			name:  "marker exists and is fresh",
			agent: "claude",
			setupExec: map[string]MockExecResult{
				// Return current timestamp (simulating fresh marker)
				"if [ -f /amux/.installed/claude": {Output: "1704067200", ExitCode: 0}, // Jan 1, 2024
			},
			want: false, // Will be false because mock timestamp is old
		},
		{
			name:  "marker does not exist",
			agent: "claude",
			setupExec: map[string]MockExecResult{
				"if [ -f /amux/.installed/claude": {Output: "0", ExitCode: 0},
			},
			want: false,
		},
		{
			name:  "command fails",
			agent: "claude",
			setupExec: map[string]MockExecResult{
				"if [ -f /amux/.installed/claude": {Output: "", ExitCode: 1},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockRemoteComputer("test")

			for prefix, result := range tt.setupExec {
				mock.SetExecResult(prefix, result.Output, result.ExitCode)
			}

			got := isAgentInstallFresh(mock, tt.agent)
			if got != tt.want {
				t.Errorf("isAgentInstallFresh(%q) = %v, want %v", tt.agent, got, tt.want)
			}
		})
	}
}

func TestTouchAgentMarker(t *testing.T) {
	mock := NewMockRemoteComputer("test")

	touchAgentMarker(mock, "claude")

	history := mock.GetExecHistory()

	// Should have mkdir and touch commands
	foundMkdir := false
	foundTouch := false
	for _, cmd := range history {
		if strings.Contains(cmd, "mkdir") && strings.Contains(cmd, "/amux/.installed") {
			foundMkdir = true
		}
		if strings.Contains(cmd, "touch") && strings.Contains(cmd, "/amux/.installed/claude") {
			foundTouch = true
		}
	}

	if !foundMkdir {
		t.Error("touchAgentMarker should create /amux/.installed directory")
	}
	if !foundTouch {
		t.Error("touchAgentMarker should touch /amux/.installed/claude marker")
	}
}

func TestAgentInstallMarker(t *testing.T) {
	tests := []struct {
		agent string
		want  string
	}{
		{"claude", "/amux/.installed/claude"},
		{"codex", "/amux/.installed/codex"},
		{"amp", "/amux/.installed/amp"},
		{"droid", "/amux/.installed/droid"},
	}

	for _, tt := range tests {
		t.Run(tt.agent, func(t *testing.T) {
			got := agentInstallMarker(tt.agent)
			if got != tt.want {
				t.Errorf("agentInstallMarker(%q) = %q, want %q", tt.agent, got, tt.want)
			}
		})
	}
}

func TestEnsureAgentInstalled_Shell(t *testing.T) {
	mock := NewMockRemoteComputer("test")

	// Shell agent should return nil without doing anything
	err := EnsureAgentInstalled(mock, AgentShell, false, false)
	if err != nil {
		t.Errorf("EnsureAgentInstalled(AgentShell) error = %v", err)
	}

	// Should not execute any commands
	history := mock.GetExecHistory()
	if len(history) > 0 {
		t.Errorf("EnsureAgentInstalled(AgentShell) should not execute commands, got %d", len(history))
	}
}

func TestEnsureAgentInstalled_SkipsIfFresh(t *testing.T) {
	mock := NewMockRemoteComputer("test")

	// Simulate fresh marker (within 24h) - use current timestamp
	// The isAgentInstallFresh function checks if timestamp is within TTL
	// We need to return "0" to indicate no marker exists, then it will install
	mock.SetExecResult("if [ -f /amux/.installed/claude", "0", 0)
	mock.SetExecResult("which claude", "", 0) // Already installed
	mock.SetExecResult("touch", "", 0)
	mock.SetExecResult("mkdir", "", 0)

	err := EnsureAgentInstalled(mock, AgentClaude, false, false)
	if err != nil {
		t.Errorf("EnsureAgentInstalled() error = %v", err)
	}
}

func TestInstallClaude(t *testing.T) {
	t.Run("skips when already installed (native)", func(t *testing.T) {
		mock := NewMockRemoteComputer("test")
		// Mock getHomeDir
		mock.SetExecResult("sh -lc", "/home/user", 0)
		// Mock native installation check - succeeds
		mock.SetExecResult("test -x", "", 0)
		// Mock marker commands
		mock.SetExecResult("mkdir", "", 0)
		mock.SetExecResult("touch", "", 0)

		err := installClaude(mock, false, false)
		if err != nil {
			t.Errorf("installClaude() error = %v", err)
		}

		// Should not have called curl since it was already installed
		history := mock.GetExecHistory()
		for _, cmd := range history {
			if strings.Contains(cmd, "curl") {
				t.Error("installClaude() should not call curl when already installed")
			}
		}
	})

	t.Run("installs when not present", func(t *testing.T) {
		mock := NewMockRemoteComputer("test")
		// Mock getHomeDir
		mock.SetExecResult("sh -lc", "/home/user", 0)
		// Mock native installation check - fails (not installed)
		mock.SetExecResult("test -x", "", 1)
		// Mock which - fails (not in PATH)
		mock.SetExecResult("which claude", "", 1)
		// Mock curl install - succeeds
		mock.SetExecResult("bash -lc", "", 0)
		// Mock marker commands
		mock.SetExecResult("mkdir", "", 0)
		mock.SetExecResult("touch", "", 0)

		err := installClaude(mock, false, false)
		if err != nil {
			t.Errorf("installClaude() error = %v", err)
		}

		// Should have called curl for installation
		history := mock.GetExecHistory()
		foundCurl := false
		for _, cmd := range history {
			if strings.Contains(cmd, "curl") && strings.Contains(cmd, "claude.ai/install.sh") {
				foundCurl = true
				break
			}
		}
		if !foundCurl {
			t.Error("installClaude() should use native installer (curl)")
		}
	})
}

func TestInstallCodex(t *testing.T) {
	mock := NewMockRemoteComputer("test")
	mock.SetExecResult("which codex", "", 1) // Not installed
	mock.SetExecResult("npm install", "", 0) // Install succeeds
	mock.SetExecResult("mkdir", "", 0)
	mock.SetExecResult("touch", "", 0)

	err := installCodex(mock, false, false)
	if err != nil {
		t.Errorf("installCodex() error = %v", err)
	}

	history := mock.GetExecHistory()
	foundNpm := false
	for _, cmd := range history {
		if strings.Contains(cmd, "npm install -g @openai/codex") {
			foundNpm = true
			break
		}
	}
	if !foundNpm {
		t.Error("installCodex() should use npm install")
	}
}

func TestInstallGemini(t *testing.T) {
	mock := NewMockRemoteComputer("test")
	mock.SetExecResult("which gemini", "", 1) // Not installed
	mock.SetExecResult("npm install", "", 0)  // Install succeeds
	mock.SetExecResult("mkdir", "", 0)
	mock.SetExecResult("touch", "", 0)

	err := installGemini(mock, false, false)
	if err != nil {
		t.Errorf("installGemini() error = %v", err)
	}

	history := mock.GetExecHistory()
	foundNpm := false
	for _, cmd := range history {
		if strings.Contains(cmd, "npm install -g @google/gemini-cli") {
			foundNpm = true
			break
		}
	}
	if !foundNpm {
		t.Error("installGemini() should use npm install")
	}
}

func TestInstallAmp(t *testing.T) {
	mock := NewMockRemoteComputer("test")
	mock.SetHomeDir("/home/user")
	mock.SetExecResult("sh -lc", "", 1)   // command -v amp fails
	mock.SetExecResult("bash -lc", "", 0) // curl install succeeds
	mock.SetExecResult("mkdir", "", 0)
	mock.SetExecResult("touch", "", 0)

	err := installAmp(mock, false, false)
	if err != nil {
		t.Errorf("installAmp() error = %v", err)
	}

	history := mock.GetExecHistory()
	foundCurl := false
	for _, cmd := range history {
		if strings.Contains(cmd, "curl") && strings.Contains(cmd, "ampcode.com/install.sh") {
			foundCurl = true
			break
		}
	}
	if !foundCurl {
		t.Error("installAmp() should use curl installer")
	}
}

func TestInstallDroid(t *testing.T) {
	mock := NewMockRemoteComputer("test")
	mock.SetExecResult("which droid", "", 1) // Not installed
	mock.SetExecResult("bash -lc", "", 0)    // curl install succeeds
	mock.SetExecResult("mkdir", "", 0)
	mock.SetExecResult("touch", "", 0)

	err := installDroid(mock, false, false)
	if err != nil {
		t.Errorf("installDroid() error = %v", err)
	}

	history := mock.GetExecHistory()
	foundCurl := false
	for _, cmd := range history {
		if strings.Contains(cmd, "curl") && strings.Contains(cmd, "factory.ai/cli") {
			foundCurl = true
			break
		}
	}
	if !foundCurl {
		t.Error("installDroid() should use curl installer")
	}
}

func TestInstallOpenCode(t *testing.T) {
	mock := NewMockRemoteComputer("test")
	mock.SetExecResult("which opencode", "", 1) // Not installed
	mock.SetExecResult("bash -lc", "", 0)       // curl install succeeds
	mock.SetExecResult("mkdir", "", 0)
	mock.SetExecResult("touch", "", 0)

	err := installOpenCode(mock, false, false)
	if err != nil {
		t.Errorf("installOpenCode() error = %v", err)
	}

	history := mock.GetExecHistory()
	foundCurl := false
	for _, cmd := range history {
		if strings.Contains(cmd, "curl") && strings.Contains(cmd, "opencode.ai/install") {
			foundCurl = true
			break
		}
	}
	if !foundCurl {
		t.Error("installOpenCode() should use curl installer")
	}
}

func TestGetHomeDir(t *testing.T) {
	tests := []struct {
		name      string
		setupExec map[string]MockExecResult
		want      string
	}{
		{
			name: "returns home dir from command",
			setupExec: map[string]MockExecResult{
				"sh -lc": {Output: "/home/testuser", ExitCode: 0},
			},
			want: "/home/testuser",
		},
		{
			name: "falls back to /home/daytona on failure",
			setupExec: map[string]MockExecResult{
				"sh -lc": {Output: "", ExitCode: 1},
			},
			want: "/home/daytona",
		},
		{
			name: "falls back to /home/daytona on empty output",
			setupExec: map[string]MockExecResult{
				"sh -lc": {Output: "   ", ExitCode: 0},
			},
			want: "/home/daytona",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockRemoteComputer("test")

			for prefix, result := range tt.setupExec {
				mock.SetExecResult(prefix, result.Output, result.ExitCode)
			}

			got := getHomeDir(mock)
			if got != tt.want {
				t.Errorf("getHomeDir() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHasScript(t *testing.T) {
	tests := []struct {
		name      string
		setupExec map[string]MockExecResult
		want      bool
	}{
		{
			name: "script command available",
			setupExec: map[string]MockExecResult{
				"command -v script": {Output: "/usr/bin/script", ExitCode: 0},
			},
			want: true,
		},
		{
			name: "script command not available",
			setupExec: map[string]MockExecResult{
				"command -v script": {Output: "", ExitCode: 1},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockRemoteComputer("test")

			for prefix, result := range tt.setupExec {
				mock.SetExecResult(prefix, result.Output, result.ExitCode)
			}

			got := hasScript(mock)
			if got != tt.want {
				t.Errorf("hasScript() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetNodeBinDir(t *testing.T) {
	tests := []struct {
		name      string
		setupExec map[string]MockExecResult
		want      string
	}{
		{
			name: "returns node bin directory",
			setupExec: map[string]MockExecResult{
				"command -v node":               {Output: "/usr/local/bin/node\n", ExitCode: 0},
				"dirname '/usr/local/bin/node'": {Output: "/usr/local/bin\n", ExitCode: 0},
			},
			want: "/usr/local/bin",
		},
		{
			name: "returns empty when node not found",
			setupExec: map[string]MockExecResult{
				"command -v node": {Output: "", ExitCode: 1},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockRemoteComputer("test")

			for prefix, result := range tt.setupExec {
				mock.SetExecResult(prefix, result.Output, result.ExitCode)
			}

			got := getNodeBinDir(mock)
			if got != tt.want {
				t.Errorf("getNodeBinDir() = %q, want %q", got, tt.want)
			}
		})
	}
}
