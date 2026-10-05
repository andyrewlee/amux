package process

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

// admitOnDoneCmd starts a real command exactly the way RunOnDone does —
// process-grouped, tail-bounded, admitted after Start — so the waiter under
// test runs against the same tracked shape production gives it.
func admitOnDoneCmd(t *testing.T, runner *ScriptRunner, ws *data.Workspace, script string) (*lifecycleProc, *tailWriter) {
	t.Helper()
	cmd := exec.Command("sh", "-c", script)
	cmd.Dir = ws.Root
	SetProcessGroup(cmd)
	tail := &tailWriter{max: scriptOutputTailBytes}
	cmd.Stdout = tail
	cmd.Stderr = tail
	if err := cmd.Start(); err != nil {
		t.Fatalf("start on-done fixture: %v", err)
	}
	proc, admErr := runner.lifecycle.admitOnDone(scriptWorkspaceKey(ws), cmd)
	if admErr != nil {
		_ = KillProcessGroup(cmd.Process.Pid, KillOptions{})
		_ = cmd.Wait()
		t.Fatalf("admitOnDone rejected a workspace not under teardown: %v", admErr)
	}
	// Backstop: if a test aborts before its waiter reaps the child, kill it
	// rather than leaking a live process out of the fixture.
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = ForceKillProcess(cmd.Process.Pid)
		}
	})
	return proc, tail
}

// runWaitOnDone executes the exact production waiter inside a recovery
// wrapper and reports whatever it panicked with — only after every defer in
// the helper has unwound, so a second close firing after the drain channel's
// first close is still captured as the outcome.
func runWaitOnDone(runner *ScriptRunner, ws *data.Workspace, key string, proc *lifecycleProc, cmdStr string, tail *tailWriter) <-chan any {
	outcome := make(chan any, 1)
	go func() {
		var panicValue any
		func() {
			defer func() { panicValue = recover() }()
			runner.waitOnDone(ws, key, proc, cmdStr, tail)
		}()
		outcome <- panicValue
	}()
	return outcome
}

// joinWaiter requires the waiter to return without panicking, bounded so a
// hung helper fails instead of stalling the suite.
func joinWaiter(t *testing.T, outcome <-chan any) {
	t.Helper()
	select {
	case panicValue := <-outcome:
		if panicValue != nil {
			t.Fatalf("on-done waiter panicked after completing: %v", panicValue)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("on-done waiter never returned")
	}
}

// assertProcCompleted requires the coordinator to have published completion
// and dropped the process from its tracked set — the contract teardown
// drains on.
func assertProcCompleted(t *testing.T, runner *ScriptRunner, key string, proc *lifecycleProc) {
	t.Helper()
	select {
	case <-proc.done:
	default:
		t.Fatal("drain channel not closed after the waiter returned")
	}
	runner.lifecycle.mu.Lock()
	defer runner.lifecycle.mu.Unlock()
	if st := runner.lifecycle.states[key]; st != nil {
		if _, tracked := st.onDone[proc]; tracked {
			t.Fatal("reaped on-done process still tracked by the coordinator")
		}
	}
}

// TestOnDoneCompletion pins single-owner completion: every admitted hook is
// reaped, recorded, and completed exactly once — finishing must never close
// the drain channel a second time.
func TestOnDoneCompletion(t *testing.T) {
	t.Run("zero exit completes once", func(t *testing.T) {
		runner := NewScriptRunner(6200, 10)
		ws := newHostedWorkspace(t, "nonconcurrent")
		key := scriptWorkspaceKey(ws)
		proc, tail := admitOnDoneCmd(t, runner, ws, "echo done-out")

		joinWaiter(t, runWaitOnDone(runner, ws, key, proc, "echo done-out", tail))
		assertProcCompleted(t, runner, key, proc)

		entry, ok := runner.LastScriptOutputs(ws)[ScriptOnDone]
		if !ok || !strings.Contains(entry.Text, "done-out") || entry.Err != "" {
			t.Fatalf("success transcript = %+v (ok=%v), want output and no Err", entry, ok)
		}
	})

	t.Run("nonzero exit notifies once", func(t *testing.T) {
		runner := NewScriptRunner(6200, 10)
		ws := newHostedWorkspace(t, "nonconcurrent")
		key := scriptWorkspaceKey(ws)
		proc, tail := admitOnDoneCmd(t, runner, ws, "echo hook-out; echo hook-err 1>&2; exit 7")

		calls := make(chan error, 4)
		runner.SetScriptExitListener(func(_ *data.Workspace, st ScriptType, runErr error) {
			if st != ScriptOnDone {
				t.Errorf("listener saw script type %v, want on-done", st)
			}
			calls <- runErr
		})

		joinWaiter(t, runWaitOnDone(runner, ws, key, proc, "exit 7", tail))
		assertProcCompleted(t, runner, key, proc)

		if len(calls) != 1 {
			t.Fatalf("exit listener fired %d times, want exactly one", len(calls))
		}
		if runErr := <-calls; runErr == nil {
			t.Fatal("listener runErr = nil on a non-zero exit")
		}
		entry, ok := runner.LastScriptOutputs(ws)[ScriptOnDone]
		if !ok || !strings.Contains(entry.Text, "hook-out") || !strings.Contains(entry.Text, "hook-err") {
			t.Fatalf("failure transcript = %+v (ok=%v), want both streams", entry, ok)
		}
	})

	t.Run("teardown drains a running hook once", func(t *testing.T) {
		runner := NewScriptRunner(6200, 10)
		ws := newHostedWorkspace(t, "nonconcurrent")
		key := scriptWorkspaceKey(ws)
		proc, tail := admitOnDoneCmd(t, runner, ws, "sleep 30")

		outcome := runWaitOnDone(runner, ws, key, proc, "sleep 30", tail)

		guard, err := runner.BeginTeardown(ws)
		if err != nil {
			t.Fatalf("BeginTeardown() error = %v", err)
		}
		defer guard.Finish(true)

		// Teardown returned only after the drain channel closed; the waiter
		// must then have unwound completely — including its own defers —
		// without a second close.
		joinWaiter(t, outcome)
		assertProcCompleted(t, runner, key, proc)
		if runner.IsRunning(ws) {
			t.Fatal("IsRunning() = true after teardown drained the on-done hook")
		}
	})
}
