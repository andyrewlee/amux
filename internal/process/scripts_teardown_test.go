//go:build !windows

package process

import (
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/testutil"
)

// processGone reports whether the process (group leader) for pid is gone,
// i.e. syscall.Kill(pid, 0) returns ESRCH. A live process returns nil. Used to
// prove a child actually died, not just that the running map was cleared.
func processGone(pid int) bool {
	return syscall.Kill(pid, 0) == syscall.ESRCH
}

func TestScriptRunnerStopIgnoresTermHelper(t *testing.T) {
	if os.Getenv("AMUX_SCRIPT_HELPER") != "1" {
		return
	}
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)
	if readyPath := os.Getenv("AMUX_SCRIPT_HELPER_READY"); readyPath != "" {
		if err := os.WriteFile(readyPath, []byte("ready"), 0o600); err != nil {
			os.Exit(2)
		}
	}
	for range term {
	}
}

// TestScriptRunner_StopAll proves StopAll kills every running child and clears
// the map: it starts two real long-lived scripts under two different workspace
// roots, asserts both are running, then asserts StopAll both reaps the real
// processes (verified via Kill(pid,0)==ESRCH, not just a map clear) and that a
// follow-up Stop is a no-op.
func TestScriptRunner_StopAll(t *testing.T) {
	repoA := t.TempDir()
	rootA := t.TempDir()
	repoB := t.TempDir()
	rootB := t.TempDir()

	writeWorkspaceConfig(t, repoA, `{"run": "sleep 30"}`)
	writeWorkspaceConfig(t, repoB, `{"run": "sleep 30"}`)

	runner := NewScriptRunner(6200, 10)
	useTempTrust(t, runner)
	if err := runner.TrustRepoScripts(repoA); err != nil {
		t.Fatalf("TrustRepoScripts(A): %v", err)
	}
	if err := runner.TrustRepoScripts(repoB); err != nil {
		t.Fatalf("TrustRepoScripts(B): %v", err)
	}
	wsA := &data.Workspace{Repo: repoA, Root: rootA}
	wsB := &data.Workspace{Repo: repoB, Root: rootB}

	cmdA, err := runner.RunScript(wsA, ScriptRun)
	if err != nil {
		t.Fatalf("RunScript(A) error = %v", err)
	}
	cmdB, err := runner.RunScript(wsB, ScriptRun)
	if err != nil {
		t.Fatalf("RunScript(B) error = %v", err)
	}
	pidA := cmdA.Process.Pid
	pidB := cmdB.Process.Pid

	// Backstop: if any assertion below fails the processes still get reaped so
	// the test never leaves an orphaned "sleep 30".
	t.Cleanup(func() {
		_ = ForceKillProcess(pidA)
		_ = ForceKillProcess(pidB)
	})

	if !runner.IsRunning(wsA) || !runner.IsRunning(wsB) {
		t.Fatalf("expected both scripts running: A=%v B=%v", runner.IsRunning(wsA), runner.IsRunning(wsB))
	}

	runner.StopAll()

	if runner.IsRunning(wsA) {
		t.Fatal("expected wsA cleared from running map after StopAll")
	}
	if runner.IsRunning(wsB) {
		t.Fatal("expected wsB cleared from running map after StopAll")
	}

	// Prove the real processes actually died, not just that the map was reset.
	testutil.Eventually(t, 2*time.Second, 20*time.Millisecond, func() bool {
		return processGone(pidA)
	}, "process A (pid %d) still running after StopAll", pidA)
	testutil.Eventually(t, 2*time.Second, 20*time.Millisecond, func() bool {
		return processGone(pidB)
	}, "process B (pid %d) still running after StopAll", pidB)

	// A fresh Stop after StopAll is a no-op (nothing left in the map).
	if err := runner.Stop(wsA); err != nil {
		t.Fatalf("Stop(A) after StopAll should be a no-op, got %v", err)
	}
	if err := runner.Stop(wsB); err != nil {
		t.Fatalf("Stop(B) after StopAll should be a no-op, got %v", err)
	}
}

// TestScriptRunner_StopForceKillsStuckProcess proves Stop's own
// SIGTERM->SIGKILL timeout fallback against a process that ignores SIGTERM.
// The body traps TERM, so the initial termination is swallowed, running.done
// never closes within the (shortened) scriptStopTimeout, and Stop must escalate
// via the real ForceKillProcess (SIGKILL).
func TestScriptRunner_StopForceKillsStuckProcess(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()
	readyPath := filepath.Join(t.TempDir(), "ready")

	runner := NewScriptRunner(6200, 10)
	trustRepo(t, runner, repo)
	ws := &data.Workspace{Repo: repo, Root: wsRoot}

	runner.killProcessGroup = func(pid int, _ KillOptions) error {
		pgid, err := syscall.Getpgid(pid)
		if err == syscall.ESRCH {
			return nil
		}
		if err != nil {
			return err
		}
		err = syscall.Kill(-pgid, syscall.SIGTERM)
		if err == syscall.ESRCH {
			return nil
		}
		return err
	}

	// Shorten Stop's escalation timeout. The stubbed killProcessGroup sends only
	// SIGTERM and never performs its own SIGKILL escalation, so a missing
	// ForceKillProcess call in Stop would leave this process alive.
	prevTimeout := scriptStopTimeout
	scriptStopTimeout = 50 * time.Millisecond
	t.Cleanup(func() { scriptStopTimeout = prevTimeout })

	cmd := exec.Command(os.Args[0], "-test.run=TestScriptRunnerStopIgnoresTermHelper", "--")
	cmd.Env = append(os.Environ(), "AMUX_SCRIPT_HELPER=1", "AMUX_SCRIPT_HELPER_READY="+readyPath)
	SetProcessGroup(cmd)
	err := cmd.Start()
	if err != nil {
		t.Fatalf("Start helper: %v", err)
	}
	pid := cmd.Process.Pid
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatalf("Getpgid(%d): %v", pid, err)
	}
	if pgid != pid {
		t.Fatalf("expected script process group %d to match pid %d", pgid, pid)
	}

	// Backstop so a failure here never leaks the helper process.
	t.Cleanup(func() { _ = ForceKillProcess(pid) })

	key := scriptWorkspaceKey(ws)
	done := make(chan struct{})
	runner.setRunningEntry(key, &runningScript{
		cmd:  cmd,
		done: done, // Never closes: forces Stop's timeout branch.
	})

	testutil.Eventually(t, 2*time.Second, 20*time.Millisecond, func() bool {
		_, statErr := os.Stat(readyPath)
		return statErr == nil
	}, "script did not install TERM trap")

	started := time.Now()
	if err := runner.Stop(ws); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed < scriptStopTimeout {
		t.Fatalf("Stop() returned before scriptStopTimeout: elapsed=%s timeout=%s", elapsed, scriptStopTimeout)
	}

	// The SIGTERM-ignoring process must have been SIGKILLed by the escalation.
	waited := make(chan error, 1)
	go func() {
		waited <- cmd.Wait()
	}()
	select {
	case err := <-waited:
		if err == nil {
			t.Fatal("expected helper to exit from SIGKILL, got nil Wait error")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("SIGTERM-ignoring process (pid %d) still running; escalation to SIGKILL did not reap it", pid)
	}
	close(done)
	if !processGone(pid) {
		t.Fatalf("SIGTERM-ignoring process (pid %d) was reaped but still appears alive", pid)
	}

	if runner.IsRunning(ws) {
		t.Fatal("expected runner entry cleared after Stop force-kill")
	}
}

// TestScriptRunner_NonconcurrentRestartKeepsNewEntry pins the
// `current == running` guard in RunScript's monitor goroutine: a dying run's
// monitor must not delete the entry that a newer run for the same workspace
// installed. Run #1 exits immediately ("exit 0"); run #2 (same workspace,
// nonconcurrent) is long-lived ("sleep 30"). Even after run #1's monitor
// goroutine fires, run #2's entry must survive and Stop must reap it.
//
// This test is inherently timing-sensitive: run #1's monitor may fire before or
// after run #2 registers, and the guard must hold in both orderings. We assert
// IsRunning *stays* true across a bounded window (Consistently) so the bug
// (guard removed -> run #1's monitor deletes run #2's entry) would be caught
// whenever the monitor fires within the window, without ever relying on a bare
// fixed sleep for synchronization.
func TestScriptRunner_NonconcurrentRestartKeepsNewEntry(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()

	runner := NewScriptRunner(6200, 10)
	useTempTrust(t, runner)
	ws := &data.Workspace{Repo: repo, Root: wsRoot, ScriptMode: "nonconcurrent"}

	// Run #1: fast-exiting body. Its monitor goroutine will fire (cmd.Wait
	// returns) and attempt to delete the map entry for this workspace.
	writeWorkspaceConfig(t, repo, `{"run": "exit 0"}`)
	if err := runner.TrustRepoScripts(repo); err != nil {
		t.Fatalf("TrustRepoScripts(#1): %v", err)
	}
	if _, err := runner.RunScript(ws, ScriptRun); err != nil {
		t.Fatalf("RunScript(#1) error = %v", err)
	}

	// Run #2: long-lived body for the same workspace. In nonconcurrent mode
	// RunScript first Stops the prior run, then registers this one. The monitor
	// guard must stop run #1's (now stale) monitor from deleting run #2's entry.
	// Re-trust because the config content (and thus its approved hash) changed.
	writeWorkspaceConfig(t, repo, `{"run": "sleep 30"}`)
	if err := runner.TrustRepoScripts(repo); err != nil {
		t.Fatalf("TrustRepoScripts(#2): %v", err)
	}
	cmd2, err := runner.RunScript(ws, ScriptRun)
	if err != nil {
		t.Fatalf("RunScript(#2) error = %v", err)
	}
	pid2 := cmd2.Process.Pid

	// Backstop so a failure never leaves an orphaned "sleep 30".
	t.Cleanup(func() { _ = ForceKillProcess(pid2) })

	// Run #2's entry must remain registered across a window long enough for run
	// #1's monitor to have fired. If the guard were missing, IsRunning would flip
	// to false when that stale monitor deleted the entry.
	testutil.Consistently(t, 1*time.Second, 20*time.Millisecond, func() string {
		if !runner.IsRunning(ws) {
			return "run #2's entry was deleted (stale monitor from run #1 raced the guard)"
		}
		return ""
	})

	// And Stop must actually stop run #2 (the live process), reaping it.
	if err := runner.Stop(ws); err != nil {
		t.Fatalf("Stop(#2) error = %v", err)
	}
	testutil.Eventually(t, 2*time.Second, 20*time.Millisecond, func() bool {
		return processGone(pid2)
	}, "run #2 (pid %d) still running after Stop", pid2)
	if runner.IsRunning(ws) {
		t.Fatal("expected run #2 entry cleared after Stop")
	}
}
