package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAssistantPresentWrapper_UsesNativeBinAndPassesStdin(t *testing.T) {
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-present.sh")
	fakeBinDir := t.TempDir()
	fakeNativePath := filepath.Join(fakeBinDir, "amux-native")
	logPath := filepath.Join(fakeBinDir, "native-call.log")
	writeExecutable(t, fakeNativePath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' "$*" > "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
cat
`)

	env := withEnv(os.Environ(), "AMUX_ASSISTANT_NATIVE_BIN", fakeNativePath)
	env = withEnv(env, "AMUX_CALL_LOG", logPath)

	stdout, stderr := runScriptOutputWithInput(t, scriptPath, env, "{\"ok\":true}\n")
	if stdout != "{\"ok\":true}\n" {
		t.Fatalf("stdout = %q, want %q", stdout, "{\"ok\":true}\n")
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}

	rawArgs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read AMUX_CALL_LOG: %v", err)
	}
	if !strings.Contains(string(rawArgs), "assistant present") {
		t.Fatalf("wrapper did not invoke native present command: %q", strings.TrimSpace(string(rawArgs)))
	}
}

func TestAssistantDogfoodWrapper_UsesNativeBin(t *testing.T) {
	requireBinary(t, "bash")

	scriptPath := filepath.Join("..", "..", "skills", "amux", "scripts", "assistant-dogfood.sh")
	fakeBinDir := t.TempDir()
	fakeNativePath := filepath.Join(fakeBinDir, "amux-native")
	logPath := filepath.Join(fakeBinDir, "native-call.log")
	writeExecutable(t, fakeNativePath, `#!/usr/bin/env bash
set -euo pipefail
printf '%s' "$*" > "${AMUX_CALL_LOG:?missing AMUX_CALL_LOG}"
printf 'dogfood native output\n'
`)

	env := withEnv(os.Environ(), "AMUX_ASSISTANT_NATIVE_BIN", fakeNativePath)
	env = withEnv(env, "AMUX_CALL_LOG", logPath)

	stdout, stderr := runScriptOutput(t, scriptPath, env, "--repo", "/tmp/repo")
	if stdout != "dogfood native output\n" {
		t.Fatalf("stdout = %q, want %q", stdout, "dogfood native output\n")
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}

	rawArgs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read AMUX_CALL_LOG: %v", err)
	}
	if !strings.Contains(string(rawArgs), "assistant dogfood --repo /tmp/repo") {
		t.Fatalf("wrapper did not invoke native dogfood command: %q", strings.TrimSpace(string(rawArgs)))
	}
}

func TestAssistantStepAMUXBin_PrefersAssistantNativeBin(t *testing.T) {
	t.Setenv("AMUX_ASSISTANT_NATIVE_BIN", "/tmp/amux-native")
	t.Setenv("AMUX_BIN", "/tmp/amux-installed")

	if got := assistantStepAMUXBin(); got != "/tmp/amux-native" {
		t.Fatalf("assistantStepAMUXBin() = %q, want %q", got, "/tmp/amux-native")
	}
}

func TestAssistantStepAMUXBin_ReusesCurrentExecutableWhenRequested(t *testing.T) {
	currentExe, err := os.Executable()
	if err != nil {
		t.Fatalf("Executable() error = %v", err)
	}

	t.Setenv("AMUX_ASSISTANT_REUSE_SELF_EXEC", "true")
	t.Setenv("AMUX_ASSISTANT_NATIVE_BIN", "")
	t.Setenv("AMUX_BIN", "")

	if got := assistantStepAMUXBin(); got != currentExe {
		t.Fatalf("assistantStepAMUXBin() = %q, want current executable %q", got, currentExe)
	}
}

func TestAssistantStepAMUXBin_FallsBackToOptHomebrew(t *testing.T) {
	oldLookPath := assistantStepLookPath
	oldStat := assistantStepStat
	t.Cleanup(func() {
		assistantStepLookPath = oldLookPath
		assistantStepStat = oldStat
	})

	assistantStepLookPath = func(string) (string, error) {
		return "", errors.New("missing")
	}
	assistantStepStat = func(path string) (os.FileInfo, error) {
		if path == "/opt/homebrew/bin/amux" {
			return fakeFileInfo{name: "amux", mode: 0o755}, nil
		}
		return nil, os.ErrNotExist
	}

	t.Setenv("AMUX_ASSISTANT_REUSE_SELF_EXEC", "")
	t.Setenv("AMUX_ASSISTANT_NATIVE_BIN", "")
	t.Setenv("AMUX_BIN", "")

	if got := assistantStepAMUXBin(); got != "/opt/homebrew/bin/amux" {
		t.Fatalf("assistantStepAMUXBin() = %q, want %q", got, "/opt/homebrew/bin/amux")
	}
}

type fakeFileInfo struct {
	name string
	mode os.FileMode
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeFileInfo) Sys() any           { return nil }
