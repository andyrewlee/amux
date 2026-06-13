package process

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

// TestScriptRunnerReleaseWorkspace proves a workspace's port allocation is
// released (dropped from the allocator) once no script is running for it.
func TestScriptRunnerReleaseWorkspace(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()
	ws := &data.Workspace{Repo: repo, Root: wsRoot}

	runner := NewScriptRunner(6200, 10)

	// Allocate a port range for the workspace, as BuildEnv would during a run.
	runner.portAllocator.AllocatePort(ws.Root)
	if _, ok := runner.portAllocator.GetPort(ws.Root); !ok {
		t.Fatalf("expected port to be allocated for %s", ws.Root)
	}

	runner.ReleaseWorkspace(ws)

	if _, ok := runner.portAllocator.GetPort(ws.Root); ok {
		t.Fatalf("expected port allocation to be released for %s", ws.Root)
	}
}

// TestScriptRunnerReleaseWorkspaceGatedByRunning proves the release is a no-op
// while a script is still running, so it can never strand a live script's port.
func TestScriptRunnerReleaseWorkspaceGatedByRunning(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()
	ws := &data.Workspace{Repo: repo, Root: wsRoot}

	runner := NewScriptRunner(6200, 10)
	runner.portAllocator.AllocatePort(ws.Root)

	// Simulate a script still running for this workspace.
	runner.running[scriptWorkspaceKey(ws)] = &runningScript{}

	runner.ReleaseWorkspace(ws)

	if _, ok := runner.portAllocator.GetPort(ws.Root); !ok {
		t.Fatalf("release must not strand a running script's port; allocation was dropped")
	}
}

func TestScriptRunnerReleaseWorkspaceGatedByRunningSetup(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()
	configDir := filepath.Join(repo, ".amux")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	config := `{"setup-workspace":["sleep 0.2"]}`
	if err := os.WriteFile(filepath.Join(configDir, configFilename), []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	ws := &data.Workspace{Repo: repo, Root: wsRoot}

	runner := NewScriptRunner(6200, 10)
	trustRepo(t, runner, repo)
	done := make(chan error, 1)
	go func() {
		done <- runner.RunSetup(ws)
	}()

	deadline := time.Now().Add(time.Second)
	for !runner.IsRunning(ws) {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for setup script to be tracked as running")
		}
		time.Sleep(10 * time.Millisecond)
	}

	runner.ReleaseWorkspace(ws)

	if _, ok := runner.portAllocator.GetPort(ws.Root); !ok {
		t.Fatalf("release must not strand a running setup script's port; allocation was dropped")
	}
	if err := <-done; err != nil {
		t.Fatalf("RunSetup() error = %v", err)
	}
	if runner.IsRunning(ws) {
		t.Fatal("expected setup running entry to be cleared after completion")
	}
	if _, ok := runner.portAllocator.GetPort(ws.Root); ok {
		t.Fatal("expected pending release to drop setup port after completion")
	}
}

func TestScriptRunnerPendingReleaseDoesNotApplyToReplacementRun(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()
	ws := &data.Workspace{Repo: repo, Root: wsRoot}

	runner := NewScriptRunner(6200, 10)
	runner.portAllocator.AllocatePort(ws.Root)
	key := scriptWorkspaceKey(ws)

	oldRun := &runningScript{}
	runner.setRunningEntry(key, oldRun)
	runner.ReleaseWorkspace(ws)

	newRun := &runningScript{}
	runner.setRunningEntry(key, newRun)
	runner.finishRunningEntry(key, newRun)
	runner.finishRunningEntry(key, oldRun)

	if _, ok := runner.portAllocator.GetPort(ws.Root); !ok {
		t.Fatal("stale pending release from deleted workspace must not release replacement workspace port")
	}
}
