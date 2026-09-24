package process

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

func TestRunOnDone_NoHookConfiguredIsNil(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Scripts.OnDone = ""
	if err := runner.RunOnDone(ws, "amux-x-tab-1"); err != nil {
		t.Fatalf("RunOnDone() with no hook = %v, want nil", err)
	}
}

func TestRunOnDone_UserHookRunsWithSessionEnv(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	ws := newHostedWorkspace(t, "nonconcurrent")
	out := filepath.Join(ws.Root, "hook-out")
	ws.Scripts.OnDone = `printf %s "$AMUX_SESSION" > hook-out`

	if err := runner.RunOnDone(ws, "amux-abc123-tab-4"); err != nil {
		t.Fatalf("RunOnDone() error = %v", err)
	}
	if err := waitForFile(out, 3*time.Second); err != nil {
		t.Fatalf("hook output never appeared: %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read hook output: %v", err)
	}
	if string(got) != "amux-abc123-tab-4" {
		t.Fatalf("AMUX_SESSION = %q, want the session name", got)
	}
}

func TestRunOnDone_DoesNotOccupyRunningSlot(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Scripts.OnDone = "sleep 2"

	if err := runner.RunOnDone(ws, "amux-x-tab-1"); err != nil {
		t.Fatalf("RunOnDone() error = %v", err)
	}
	// The hook is a one-shot notification, not the workspace's run process:
	// it must never register in the running map that Stop/IsRunning manage.
	if runner.IsRunning(ws) {
		t.Fatal("IsRunning() = true while only an on-done hook is executing")
	}
}

func TestRunOnDone_RepoHookTrustGated(t *testing.T) {
	repo := t.TempDir()
	writeWorkspaceConfig(t, repo, `{"on-done": "touch repo-hook-ran"}`)
	runner := NewScriptRunner(6200, 10)
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Repo = repo

	err := runner.RunOnDone(ws, "amux-x-tab-1")
	if err == nil {
		t.Fatal("RunOnDone() = nil for an untrusted repo hook, want trust gate")
	}
	if !errors.Is(err, ErrScriptsNotTrusted) {
		t.Fatalf("RunOnDone() error = %v, want ErrScriptsNotTrusted", err)
	}
	if _, statErr := os.Stat(filepath.Join(repo, "repo-hook-ran")); statErr == nil {
		t.Fatal("untrusted repo hook executed")
	}
}

func TestRunOnDone_RepoHookRunsAfterTrust(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()
	writeWorkspaceConfig(t, repo, `{"on-done": "printf %s repo > repo-hook-ran"}`)
	runner := NewScriptRunner(6200, 10)
	trustRepo(t, runner, repo)
	ws := &data.Workspace{
		Name: "ws", Repo: repo, Root: wsRoot,
		ScriptMode: "nonconcurrent",
		Scripts:    data.ScriptsConfig{OnDone: "printf %s user > repo-hook-ran"},
	}

	if err := runner.RunOnDone(ws, "amux-x-tab-1"); err != nil {
		t.Fatalf("RunOnDone() error = %v after trust", err)
	}
	out := filepath.Join(wsRoot, "repo-hook-ran")
	if err := waitForFile(out, 3*time.Second); err != nil {
		t.Fatalf("hook output never appeared: %v", err)
	}
	got, _ := os.ReadFile(out)
	if string(got) != "repo" {
		t.Fatalf("hook ran %q, want the repo script to win over ws.Scripts", got)
	}
}
