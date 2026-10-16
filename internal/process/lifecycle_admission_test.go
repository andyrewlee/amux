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

// TestRunScriptRefusedAfterStopAll pins the BUG-06 latch: a RunScript admitted
// after StopAll swapped the running map is refused with ErrRunnerStopped —
// never admitted into the new map where it would escape the shutdown sweep.
// A StopAll that lands inside the spawn window (between the early check and
// setRunningEntry) is refused the same way by the stopped latch, with the
// just-started child killed and reaped by the loser.
func TestRunScriptRefusedAfterStopAll(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	ws := newHostedWorkspace(t, "concurrent")
	ws.Scripts.Run = "sleep 60"

	runner.StopAll()

	// The coordinator's early admission check refuses before any spawn — the
	// stopped latch means no child ever starts.
	cmd, err := runner.RunScript(ws, ScriptRun)
	if !errors.Is(err, ErrRunnerStopped) {
		t.Fatalf("RunScript after StopAll = %v, want ErrRunnerStopped", err)
	}
	if cmd != nil {
		t.Fatal("rejected run returned a live cmd")
	}
	if runner.IsRunning(ws) {
		t.Fatal("IsRunning() = true for a post-StopAll spawn")
	}

	// The coordinator's latch rejects setup/on-done admissions the same way.
	key := scriptWorkspaceKey(ws)
	if _, err := runner.lifecycle.admitSetup(key); !errors.Is(err, ErrRunnerStopped) {
		t.Fatalf("admitSetup after StopAll = %v, want ErrRunnerStopped", err)
	}
	if _, err := runner.lifecycle.admitOnDone(key, noopCmd()); !errors.Is(err, ErrRunnerStopped) {
		t.Fatalf("admitOnDone after StopAll = %v, want ErrRunnerStopped", err)
	}
}

// TestRunScriptStopAllConcurrentAdmission exercises the BUG-06 window under
// -race: a RunScript racing StopAll either registers into the pre-swap map
// (killed by the sweep) or is refused by the stopped latch — in no
// interleaving may an entry survive into the post-shutdown map.
func TestRunScriptStopAllConcurrentAdmission(t *testing.T) {
	for i := 0; i < 12; i++ {
		runner := NewScriptRunner(6200, 10)
		ws := newHostedWorkspace(t, "concurrent")
		ws.Scripts.Run = "sleep 60"

		done := make(chan error, 1)
		go func() {
			_, err := runner.RunScript(ws, ScriptRun)
			done <- err
		}()
		runner.StopAll()

		if err := <-done; err != nil && !errors.Is(err, ErrRunnerStopped) {
			t.Fatalf("iteration %d: racing RunScript error = %v, want nil or ErrRunnerStopped", i, err)
		}
		runner.mu.Lock()
		tracked := len(runner.running)
		runner.mu.Unlock()
		if tracked != 0 {
			t.Fatalf("iteration %d: %d running entries escaped the StopAll sweep", i, tracked)
		}
	}
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
