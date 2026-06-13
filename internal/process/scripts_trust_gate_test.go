package process

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

// TestScriptRunnerRunSetupUntrustedDoesNotExecute proves that an untrusted
// repo's setup-workspace command does NOT run: no side effect occurs and the
// error is ErrScriptsNotTrusted.
func TestScriptRunnerRunSetupUntrustedDoesNotExecute(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()
	marker := filepath.Join(wsRoot, "marker")

	writeWorkspaceConfig(t, repo, `{"setup-workspace": ["touch `+marker+`"]}`)

	runner := NewScriptRunner(6200, 10)
	useTempTrust(t, runner) // isolated, empty registry => repo is untrusted
	ws := &data.Workspace{Repo: repo, Root: wsRoot}

	err := runner.RunSetup(ws)
	if !errors.Is(err, ErrScriptsNotTrusted) {
		t.Fatalf("expected ErrScriptsNotTrusted, got %v", err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("untrusted setup command executed: marker file was created")
	}
}

// TestScriptRunnerRunSetupTrustedExecutes proves that once the repo is trusted,
// the same setup-workspace command runs and produces its side effect.
func TestScriptRunnerRunSetupTrustedExecutes(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()
	marker := filepath.Join(wsRoot, "marker")

	writeWorkspaceConfig(t, repo, `{"setup-workspace": ["touch `+marker+`"]}`)

	runner := NewScriptRunner(6200, 10)
	trustRepo(t, runner, repo)
	ws := &data.Workspace{Repo: repo, Root: wsRoot}

	if err := runner.RunSetup(ws); err != nil {
		t.Fatalf("RunSetup() error = %v", err)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("trusted setup command did not run: %v", statErr)
	}
}

// TestScriptRunnerRunScriptUntrustedRepoScriptDoesNotExecute proves a repo
// config's run script is gated when the repo is untrusted.
func TestScriptRunnerRunScriptUntrustedRepoScriptDoesNotExecute(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()
	marker := filepath.Join(wsRoot, "marker")

	writeWorkspaceConfig(t, repo, `{"run": "touch `+marker+`"}`)

	runner := NewScriptRunner(6200, 10)
	useTempTrust(t, runner)
	ws := &data.Workspace{Repo: repo, Root: wsRoot}

	_, err := runner.RunScript(ws, ScriptRun)
	if !errors.Is(err, ErrScriptsNotTrusted) {
		t.Fatalf("expected ErrScriptsNotTrusted, got %v", err)
	}
	// Give any (erroneously) launched process a moment; it must never appear.
	if waitForFile(marker, 200*time.Millisecond) == nil {
		t.Fatal("untrusted repo run script executed: marker file was created")
	}
}

// TestScriptRunnerWsScriptsRunWithoutTrust proves the user-entered ws.Scripts.Run
// fallback is NOT gated: it executes even though the repo is untrusted (empty
// registry). This is the core "don't gate user-entered scripts" guarantee.
func TestScriptRunnerWsScriptsRunWithoutTrust(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()
	marker := filepath.Join(wsRoot, "marker")

	// Repo config has no run script, so RunScript falls back to ws.Scripts.Run.
	writeWorkspaceConfig(t, repo, `{}`)

	runner := NewScriptRunner(6200, 10)
	useTempTrust(t, runner) // empty registry: repo is NOT trusted
	ws := &data.Workspace{
		Repo:    repo,
		Root:    wsRoot,
		Scripts: data.ScriptsConfig{Run: "touch " + marker},
	}

	if _, err := runner.RunScript(ws, ScriptRun); err != nil {
		t.Fatalf("RunScript() error = %v (ws.Scripts.* must not be gated)", err)
	}
	if err := waitForFile(marker, 2*time.Second); err != nil {
		t.Fatalf("user-entered ws.Scripts.Run did not execute: %v", err)
	}
}
