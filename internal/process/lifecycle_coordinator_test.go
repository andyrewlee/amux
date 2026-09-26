//go:build !windows

package process

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/testutil"
)

func TestLifecycleAdmitSetup_SingleAdmission(t *testing.T) {
	c := newLifecycleCoordinator()
	key := "ws-a"

	t1, err := c.admitSetup(key)
	if err != nil {
		t.Fatalf("first admitSetup = %v", err)
	}
	if _, err := c.admitSetup(key); !errors.Is(err, ErrSetupBusy) {
		t.Fatalf("second admitSetup = %v, want ErrSetupBusy", err)
	}
	c.finishSetup(key, t1)
	if _, err := c.admitSetup(key); err != nil {
		t.Fatalf("admitSetup after finish = %v, want nil", err)
	}
}

func TestLifecycleTeardown_RejectsNewAdmissions(t *testing.T) {
	c := newLifecycleCoordinator()
	key := "ws-b"

	snap, err := c.beginTeardown(key)
	if err != nil {
		t.Fatalf("beginTeardown = %v", err)
	}
	_ = snap
	if _, err := c.admitSetup(key); !errors.Is(err, ErrWorkspaceTeardown) {
		t.Fatalf("admitSetup during teardown = %v, want ErrWorkspaceTeardown", err)
	}
	if err := c.checkAdmission(key); !errors.Is(err, ErrWorkspaceTeardown) {
		t.Fatalf("checkAdmission during teardown = %v, want ErrWorkspaceTeardown", err)
	}
	if err := c.checkArchiveAdmission(key, nil); !errors.Is(err, ErrWorkspaceTeardown) {
		t.Fatalf("foreign archive during teardown = %v, want ErrWorkspaceTeardown", err)
	}
	// The owning guard admits archive.
	g := &TeardownGuard{coord: c, key: key}
	if err := c.checkArchiveAdmission(key, g); err != nil {
		t.Fatalf("guard archive during teardown = %v, want nil", err)
	}
	// A guard for a different workspace does not.
	other := &TeardownGuard{coord: c, key: "ws-other"}
	if err := c.checkArchiveAdmission(key, other); !errors.Is(err, ErrWorkspaceTeardown) {
		t.Fatalf("foreign guard archive = %v, want ErrWorkspaceTeardown", err)
	}
	// A second concurrent teardown is refused — the holder is authoritative.
	if _, err := c.beginTeardown(key); !errors.Is(err, ErrWorkspaceTeardown) {
		t.Fatalf("concurrent beginTeardown = %v, want ErrWorkspaceTeardown", err)
	}
	// Finish(false) re-opens admission for the surviving workspace.
	g.Finish(false)
	if _, err := c.admitSetup(key); err != nil {
		t.Fatalf("admitSetup after release = %v, want nil", err)
	}
	// The released guard no longer owns archive.
	if err := c.checkArchiveAdmission(key, g); err != nil {
		// Gate is open again — archive admits freely without the guard.
		t.Fatalf("archive after release = %v, want nil", err)
	}
}

func TestLifecycleTeardown_StaleTicketStaysStale(t *testing.T) {
	c := newLifecycleCoordinator()
	key := "ws-c"

	t1, err := c.admitSetup(key)
	if err != nil {
		t.Fatalf("admitSetup = %v", err)
	}
	// The ticket is current before teardown.
	if !c.ticketCurrent(key, t1) {
		t.Fatal("ticket not current before teardown")
	}
	if _, err := c.beginTeardown(key); err != nil {
		t.Fatalf("beginTeardown = %v", err)
	}
	// Bumped generation makes the pre-teardown ticket stale immediately.
	if c.ticketCurrent(key, t1) {
		t.Fatal("pre-teardown ticket still current after teardown began")
	}
	// finishSetup still closes its drain channel (a draining teardown may
	// be blocked on it) but never resurrects the slot for a stale ticket.
	c.finishSetup(key, t1)
	select {
	case <-t1.done:
	default:
		t.Fatal("stale ticket's done channel never closed")
	}
	// finishSetup of the stale ticket must not clear state needed by a NEW
	// admission after the gate releases.
	c.releaseTeardown(key)
	t2, err := c.admitSetup(key)
	if err != nil {
		t.Fatalf("admitSetup after release = %v", err)
	}
	if t2.gen == t1.gen {
		t.Fatal("new admission reused a stale generation")
	}
	if !c.ticketCurrent(key, t2) {
		t.Fatal("new ticket not current")
	}
	c.finishSetup(key, t2)
}

func TestLifecycleTeardown_RemovedDropsState(t *testing.T) {
	c := newLifecycleCoordinator()
	key := "ws-d"

	if _, err := c.beginTeardown(key); err != nil {
		t.Fatalf("beginTeardown = %v", err)
	}
	g := &TeardownGuard{coord: c, key: key}
	g.Finish(true)
	// State is gone: admissions mint a fresh generation rather than
	// inheriting a stale gate.
	c.mu.Lock()
	_, exists := c.states[key]
	c.mu.Unlock()
	if exists {
		t.Fatal("lifecycle state survived Finish(true)")
	}
	if _, err := c.admitSetup(key); err != nil {
		t.Fatalf("admitSetup on fresh state = %v", err)
	}
	// A released guard's archive admission is dead even though the gate
	// reopened: re-admitting setup does not re-authorize the old guard.
	if _, err := c.beginTeardown(key); err != nil {
		t.Fatalf("second beginTeardown = %v", err)
	}
	if err := c.checkArchiveAdmission(key, g); !errors.Is(err, ErrWorkspaceTeardown) {
		t.Fatalf("released guard archive = %v, want ErrWorkspaceTeardown", err)
	}
	c.releaseTeardown(key)
}

func TestLifecycleOnDone_TrackedSet(t *testing.T) {
	c := newLifecycleCoordinator()
	key := "ws-e"

	if c.live(key) {
		t.Fatal("live() = true before any admission")
	}
	cmd := noopCmd()
	p, ok := c.admitOnDone(key, cmd)
	if !ok {
		t.Fatal("admitOnDone = false, want true")
	}
	if !c.live(key) {
		t.Fatal("live() = false with an admitted on-done proc")
	}
	c.finishOnDone(key, p)
	if c.live(key) {
		t.Fatal("live() = true after on-done finished")
	}
	select {
	case <-p.done:
	default:
		t.Fatal("on-done done channel never closed")
	}
}

func TestLifecycleLive_CoversSetup(t *testing.T) {
	c := newLifecycleCoordinator()
	key := "ws-f"
	t1, err := c.admitSetup(key)
	if err != nil {
		t.Fatal(err)
	}
	if !c.live(key) {
		t.Fatal("live() = false during admitted setup")
	}
	c.finishSetup(key, t1)
	if c.live(key) {
		t.Fatal("live() = true after setup finished")
	}
}

// noopCmd is an unstarted command — admission bookkeeping only needs the
// pointer identity, never a live process.
func noopCmd() *exec.Cmd {
	return exec.Command("true")
}

// TestRunSetup_SecondRequestBusy proves repeated setup requests no longer
// overwrite the tracked slot: while one sequence is admitted the next returns
// ErrSetupBusy immediately rather than queueing behind or replacing it.
func TestRunSetup_SecondRequestBusy(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()
	readyPath := filepath.Join(t.TempDir(), "setup-started")

	writeWorkspaceConfig(t, repo, `{"setup-workspace": ["touch `+readyPath+`; sleep 30"]}`)
	runner := NewScriptRunner(6200, 10)
	trustRepo(t, runner, repo)
	ws := &data.Workspace{Repo: repo, Root: wsRoot}

	setupDone := make(chan error, 1)
	go func() { setupDone <- runner.RunSetup(ws) }()
	testutil.Eventually(t, 3*time.Second, 10*time.Millisecond, func() bool {
		_, err := os.Stat(readyPath)
		return err == nil
	}, "setup command never started")
	pid := setupProcessPID(runner, ws)
	t.Cleanup(func() {
		if pid != 0 {
			_ = ForceKillProcess(pid)
		}
		runner.StopAll()
	})

	err := runner.RunSetup(ws)
	if !errors.Is(err, ErrSetupBusy) {
		t.Fatalf("concurrent RunSetup = %v, want ErrSetupBusy", err)
	}
	// The first sequence is still the tracked one — the busy error did not
	// displace it.
	if got := setupProcessPID(runner, ws); got != pid {
		t.Fatalf("busy request overwrote the setup slot: pid %d -> %d", pid, got)
	}
}

// TestRunSetup_TeardownCancelsQueuedCommands pins generation invalidation:
// teardown mid-sequence must prevent every QUEUED command, not just the one
// in flight. Setup is ["sleep 30", "touch <marker>"]; teardown during the
// sleep kills it, and the marker must never be created.
func TestRunSetup_TeardownCancelsQueuedCommands(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()
	marker := filepath.Join(wsRoot, "second-command-ran")

	writeWorkspaceConfig(t, repo, `{"setup-workspace": ["sleep 30", "touch `+marker+`"]}`)
	runner := NewScriptRunner(6200, 10)
	trustRepo(t, runner, repo)
	ws := &data.Workspace{Repo: repo, Root: wsRoot}

	setupDone := make(chan error, 1)
	go func() { setupDone <- runner.RunSetup(ws) }()
	testutil.Eventually(t, 3*time.Second, 10*time.Millisecond, func() bool {
		return setupProcessPID(runner, ws) != 0
	}, "first setup command never started")
	pid := setupProcessPID(runner, ws)
	t.Cleanup(func() {
		if pid != 0 {
			_ = ForceKillProcess(pid)
		}
	})

	guard, err := runner.BeginTeardown(ws)
	if err != nil {
		t.Fatalf("BeginTeardown() error = %v", err)
	}
	defer guard.Finish(true)

	select {
	case <-setupDone:
	case <-time.After(2 * time.Second):
		t.Fatal("RunSetup never returned after teardown")
	}
	// Bounded window for a hypothetical queued command to have run — it must
	// not. The marker's absence proves the cancel spanned the sequence.
	testutil.Consistently(t, 300*time.Millisecond, 20*time.Millisecond, func() string {
		if _, err := os.Stat(marker); err == nil {
			return "queued setup command ran after teardown canceled the sequence"
		}
		return ""
	})
}

// TestBeginTeardown_ArchiveRunsUnderGuard covers the archive admission split:
// a foreign RunArchive is rejected while the gate is held, while the owning
// guard's RunArchive runs to completion before removal proceeds.
func TestBeginTeardown_ArchiveRunsUnderGuard(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()
	marker := filepath.Join(wsRoot, "archive-ran")

	writeWorkspaceConfig(t, repo, `{"archive": "touch `+marker+`"}`)
	runner := NewScriptRunner(6200, 10)
	trustRepo(t, runner, repo)
	ws := &data.Workspace{Repo: repo, Root: wsRoot}

	guard, err := runner.BeginTeardown(ws)
	if err != nil {
		t.Fatalf("BeginTeardown() error = %v", err)
	}
	defer guard.Finish(true)

	if err := runner.RunArchive(ws); !errors.Is(err, ErrWorkspaceTeardown) {
		t.Fatalf("foreign RunArchive during teardown = %v, want ErrWorkspaceTeardown", err)
	}
	if err := guard.RunArchive(ws); err != nil {
		t.Fatalf("guard RunArchive = %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("archive marker missing: %v", err)
	}
}

// TestStopAll_DrainsLifecycle proves StopAll (the quit path) cancels local
// lifecycle work — an in-flight setup and an on-done hook are both reaped —
// while remaining deliberately out of the hosted-session business.
func TestStopAll_DrainsLifecycle(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()
	readyPath := filepath.Join(t.TempDir(), "setup-started")

	writeWorkspaceConfig(t, repo, `{"setup-workspace": ["touch `+readyPath+`; sleep 30"]}`)
	runner := NewScriptRunner(6200, 10)
	trustRepo(t, runner, repo)
	ws := &data.Workspace{Repo: repo, Root: wsRoot}
	ws.Scripts.OnDone = "sleep 30"

	setupDone := make(chan error, 1)
	go func() { setupDone <- runner.RunSetup(ws) }()
	testutil.Eventually(t, 3*time.Second, 10*time.Millisecond, func() bool {
		_, err := os.Stat(readyPath)
		return err == nil
	}, "setup command never started")
	pid := setupProcessPID(runner, ws)
	if pid == 0 {
		t.Fatal("setup process not tracked")
	}
	t.Cleanup(func() { _ = ForceKillProcess(pid) })

	if err := runner.RunOnDone(ws, "amux-x-tab-1"); err != nil {
		t.Fatalf("RunOnDone() error = %v", err)
	}
	if !runner.IsRunning(ws) {
		t.Fatal("IsRunning() = false with setup and on-done live")
	}

	runner.StopAll()

	select {
	case <-setupDone:
	case <-time.After(2 * time.Second):
		t.Fatal("setup did not exit after StopAll")
	}
	if !processGone(pid) {
		t.Fatalf("setup process (pid %d) survived StopAll", pid)
	}
	if runner.IsRunning(ws) {
		t.Fatal("IsRunning() = true after StopAll drained lifecycle work")
	}
}

// TestReleaseWorkspace_ParksDuringSetup proves the port release never strands
// a live local process: while setup holds the coordinator slot the release is
// parked, and it completes once the workspace goes fully idle.
func TestReleaseWorkspace_ParksDuringSetup(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()
	releaseFlag := filepath.Join(t.TempDir(), "setup-release")
	// The setup writes a flag file when it exits — but more importantly the
	// run is short so the drain window is deterministic.
	writeWorkspaceConfig(t, repo, `{"setup-workspace": ["sleep 0.2", "touch `+releaseFlag+`"]}`)
	runner := NewScriptRunner(6200, 10)
	trustRepo(t, runner, repo)
	ws := &data.Workspace{Repo: repo, Root: wsRoot}

	// Allocate the port before setup starts so there is something to release.
	if _, err := runner.envBuilder.BuildEnv(ws); err != nil {
		t.Fatalf("BuildEnv: %v", err)
	}
	if _, held := runner.PortAllocated(ws); !held {
		t.Fatal("no port allocated for workspace")
	}

	setupDone := make(chan error, 1)
	go func() { setupDone <- runner.RunSetup(ws) }()
	testutil.Eventually(t, 3*time.Second, 10*time.Millisecond, func() bool {
		return runner.lifecycle.live(scriptWorkspaceKey(ws))
	}, "setup never reached the coordinator")

	runner.ReleaseWorkspace(ws)
	if _, held := runner.PortAllocated(ws); !held {
		t.Fatal("ReleaseWorkspace released the port while setup was still in flight")
	}

	select {
	case err := <-setupDone:
		if err != nil {
			t.Fatalf("RunSetup() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("setup never finished")
	}
	// The parked release drains on the next nothing-alive observation — the
	// indicator sync's IsRunning poll is the guaranteed caller.
	testutil.Eventually(t, 3*time.Second, 10*time.Millisecond, func() bool {
		runner.IsRunning(ws)
		_, held := runner.PortAllocated(ws)
		return !held
	}, "parked port release never drained after setup finished")
}
