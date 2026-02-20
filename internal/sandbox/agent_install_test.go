package sandbox

import (
	"strings"
	"testing"
)

func TestInstallClaude(t *testing.T) {
	t.Run("skips when already installed (native)", func(t *testing.T) {
		mock := NewMockRemoteSandbox("test")
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
		mock := NewMockRemoteSandbox("test")
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
	mock := NewMockRemoteSandbox("test")
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
	mock := NewMockRemoteSandbox("test")
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
	mock := NewMockRemoteSandbox("test")
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
	mock := NewMockRemoteSandbox("test")
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
	mock := NewMockRemoteSandbox("test")
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
