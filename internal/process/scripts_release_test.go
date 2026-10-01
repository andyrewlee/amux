package process

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"

	"github.com/andyrewlee/amux/internal/testutil"
)

// TestScriptRunnerReleaseWorkspace proves a workspace's port allocation is
// released (dropped from the allocator) once no script is running for it.
func TestScriptRunnerReleaseWorkspace(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()
	ws := &data.Workspace{Repo: repo, Root: wsRoot}

	runner := NewScriptRunner(6200, 10)

	// Allocate a port range for the workspace, as BuildEnv would during a run.
	if _, err := runner.portAllocator.AllocatePort(ws.Root); err != nil {
		t.Fatalf("AllocatePort: %v", err)
	}
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
	if _, err := runner.portAllocator.AllocatePort(ws.Root); err != nil {
		t.Fatalf("AllocatePort: %v", err)
	}

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

	testutil.Eventually(t, 3*time.Second, 10*time.Millisecond, func() bool {
		return runner.IsRunning(ws)
	}, "timed out waiting for setup script to be tracked as running")

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
	if _, err := runner.portAllocator.AllocatePort(ws.Root); err != nil {
		t.Fatalf("AllocatePort: %v", err)
	}
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

// TestScriptRunnerDurableReleaseRetainsInterval proves the durable contract at
// the runner surface: ReleaseWorkspace — including a release parked behind a
// live consumer — never frees the reservation for another workspace, and a
// freshly constructed runner on the same state home still sees it.
func TestScriptRunnerDurableReleaseRetainsInterval(t *testing.T) {
	home, meta := t.TempDir(), t.TempDir()

	newRunner := func() *ScriptRunner {
		r := NewScriptRunner(6200, 10)
		store := data.NewPortReservationStore(home)
		if err := store.Initialize(nil); err != nil {
			t.Fatalf("Initialize: %v", err)
		}
		r.SetPortReservationStore(store)
		return r
	}
	runnerA := newRunner()

	wsA := savedWorkspace(t, meta, "a")
	wsB := savedWorkspace(t, meta, "b")

	envA, err := runnerA.BuildSessionEnv(wsA)
	if err != nil {
		t.Fatalf("BuildSessionEnv(a): %v", err)
	}
	portA := envSliceToMap(envA)["AMUX_PORT"]

	// Release parked behind a live run — then the run finishes, firing the
	// real drain path (finishRunningEntry → ReleasePort). Durable mode must
	// retain regardless.
	rs := &runningScript{}
	key := scriptWorkspaceKey(wsA)
	runnerA.setRunningEntry(key, rs)
	runnerA.ReleaseWorkspace(wsA)       // parks the release against rs
	runnerA.finishRunningEntry(key, rs) // drains the park → durable no-op

	// A second runner on the same home still sees A's reservation and gives B
	// a disjoint range — the parked release freed nothing.
	runnerB := newRunner()
	base, end, found, err := runnerB.PortInterval(wsA)
	if err != nil || !found {
		t.Fatalf("runner B PortInterval(a) = found %v err %v, want true/nil", found, err)
	}
	if strconv.Itoa(base) != portA || end != base+9 {
		t.Fatalf("registry interval = %d-%d, want surviving base %s with width 10", base, end, portA)
	}
	envB, err := runnerB.BuildSessionEnv(wsB)
	if err != nil {
		t.Fatalf("BuildSessionEnv(b): %v", err)
	}
	if envSliceToMap(envB)["AMUX_PORT"] == portA {
		t.Fatalf("b was handed a's retained range %s — release leaked across instances", portA)
	}
	// A's own env stays its original interval.
	envA2, err := runnerB.BuildSessionEnv(wsA)
	if err != nil {
		t.Fatalf("BuildSessionEnv(a via B): %v", err)
	}
	if envSliceToMap(envA2)["AMUX_PORT"] != portA {
		t.Fatalf("a's interval moved after restart-style reserve")
	}
}
