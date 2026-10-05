package process

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

// TestRunScript_PostStartAdmissionKillsLoser reproduces the admission window:
// teardown seizes the gate between cmd.Start and the running-map registration,
// so its Stop sweep ran while the just-started child was still untracked. The
// post-start re-check must catch the held gate, kill and reap the child, and
// leave no running entry behind.
func TestRunScript_PostStartAdmissionKillsLoser(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Scripts.Run = "sleep 60"

	var killed []int
	origKill := runner.killProcessGroup
	runner.killProcessGroup = func(pid int, opts KillOptions) error {
		killed = append(killed, pid)
		return origKill(pid, opts)
	}

	// BeginTeardown inside the spawn window: gate + drain + Stop all run
	// before setRunningEntry, so the sweep observes an empty running map —
	// exactly the slip the early checkAdmission allowed through.
	var guard *TeardownGuard
	runner.postStartHook = func(string) {
		var err error
		guard, err = runner.BeginTeardown(ws)
		if err != nil {
			t.Errorf("BeginTeardown inside spawn window = %v", err)
		}
	}

	if _, err := runner.RunScript(ws, ScriptRun); !errors.Is(err, ErrWorkspaceTeardown) {
		t.Fatalf("RunScript under seized gate = %v, want ErrWorkspaceTeardown", err)
	}
	if runner.IsRunning(ws) {
		t.Fatal("IsRunning() = true for a rejected spawn")
	}
	if len(killed) != 1 {
		t.Fatalf("rejected spawn killed %d times, want exactly 1 (kill+reap by the loser)", len(killed))
	}
	runner.mu.Lock()
	_, tracked := runner.running[scriptWorkspaceKey(ws)]
	runner.mu.Unlock()
	if tracked {
		t.Fatal("running map still holds the rejected spawn")
	}
	guard.Finish(true)
}

// TestRunScriptHosted_PostEnsureAdmissionKillsSession is the hosted form of
// the same window: Ensure ran before the gate check could see the session, so
// teardown's sweep is what discovers it — and the post-Ensure re-check must
// still refuse admission and make sure the session is gone.
func TestRunScriptHosted_PostEnsureAdmissionKillsSession(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	host := newFakeRunSessionHost()
	runner.SetRunHost(host)
	ws := newHostedWorkspace(t, "nonconcurrent")

	var guard *TeardownGuard
	runner.postStartHook = func(string) {
		var err error
		guard, err = runner.BeginTeardown(ws)
		if err != nil {
			t.Errorf("BeginTeardown inside Ensure window = %v", err)
		}
	}

	if _, err := runner.RunScript(ws, ScriptRun); !errors.Is(err, ErrWorkspaceTeardown) {
		t.Fatalf("RunScript under seized gate = %v, want ErrWorkspaceTeardown", err)
	}
	name := "amux-ws-" + string(ws.ID()) + "-run"
	if exists, _, _, _ := host.Status(name); exists {
		t.Fatal("run session survived post-Ensure admission rejection")
	}
	if len(host.killed) == 0 {
		t.Fatal("no Kill call reached the host for the rejected session")
	}
	guard.Finish(true)
}

// TestRunOnDone_FanOutCap pins maxOnDoneHooks: admission past the cap is
// refused with ErrOnDoneLimit, and the refused spawn is killed and reaped by
// its caller rather than tracked.
func TestRunOnDone_FanOutCap(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Scripts.OnDone = "sleep 60"
	key := scriptWorkspaceKey(ws)

	for i := 0; i < maxOnDoneHooks; i++ {
		if _, err := runner.lifecycle.admitOnDone(key, noopCmd()); err != nil {
			t.Fatalf("admitOnDone %d of %d rejected: %v", i+1, maxOnDoneHooks, err)
		}
	}
	if _, err := runner.lifecycle.admitOnDone(key, noopCmd()); !errors.Is(err, ErrOnDoneLimit) {
		t.Fatalf("admitOnDone past cap = %v, want ErrOnDoneLimit", err)
	}

	killed := 0
	origKill := runner.killProcessGroup
	runner.killProcessGroup = func(pid int, opts KillOptions) error {
		killed++
		return origKill(pid, opts)
	}
	if err := runner.RunOnDone(ws, "amux-sess-1"); !errors.Is(err, ErrOnDoneLimit) {
		t.Fatalf("RunOnDone past cap = %v, want ErrOnDoneLimit", err)
	}
	if killed != 1 {
		t.Fatalf("cap-rejected spawn killed %d times, want exactly 1", killed)
	}
	runner.lifecycle.mu.Lock()
	tracked := len(runner.lifecycle.states[key].onDone)
	runner.lifecycle.mu.Unlock()
	if tracked != maxOnDoneHooks {
		t.Fatalf("on-done tracker = %d procs, want %d (rejected spawn must not register)", tracked, maxOnDoneHooks)
	}
}

// TestWaitOnDone_BoundedAtHookTimeout pins the on-done deadline: a hook that
// never exits is killed at onDoneHookTimeout instead of pinning its waiter
// and coordinator slot forever, and the abandonment is recorded + notified.
func TestWaitOnDone_BoundedAtHookTimeout(t *testing.T) {
	prevTimeout := onDoneHookTimeout
	onDoneHookTimeout = 50 * time.Millisecond
	t.Cleanup(func() { onDoneHookTimeout = prevTimeout })

	runner := NewScriptRunner(6200, 10)
	ws := newHostedWorkspace(t, "nonconcurrent")
	key := scriptWorkspaceKey(ws)
	proc, tail := admitOnDoneCmd(t, runner, ws, "sleep 60")

	calls := make(chan error, 4)
	runner.SetScriptExitListener(func(_ *data.Workspace, _ ScriptType, runErr error) {
		calls <- runErr
	})

	joinWaiter(t, runWaitOnDone(runner, ws, key, proc, "sleep 60", tail))
	assertProcCompleted(t, runner, key, proc)

	if len(calls) != 1 {
		t.Fatalf("exit listener fired %d times, want exactly one", len(calls))
	}
	if runErr := <-calls; runErr == nil || !strings.Contains(runErr.Error(), "timed out") {
		t.Fatalf("listener runErr = %v, want a timeout error", runErr)
	}
	entry, ok := runner.LastScriptOutputs(ws)[ScriptOnDone]
	if !ok || !strings.Contains(entry.Err, "timed out") {
		t.Fatalf("timeout transcript = %+v (ok=%v), want the deadline recorded", entry, ok)
	}
}
