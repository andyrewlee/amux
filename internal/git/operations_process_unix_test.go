//go:build !windows

package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/process"
)

// TestGitHelperProcess is not a test — it is the child payload re-executed by
// process-tree tests via os.Args[0]. AMUX_GIT_TEST_HELPER selects the mode;
// AMUX_GIT_HELPER_PIDFILE receives the spawned grandchild's PID so the test
// can verify (and clean up) the descendant.
func TestGitHelperProcess(t *testing.T) {
	mode := os.Getenv("AMUX_GIT_TEST_HELPER")
	if mode == "" {
		return
	}
	pidFile := os.Getenv("AMUX_GIT_HELPER_PIDFILE")
	spawn := func(detach bool) {
		child := exec.Command("sleep", "30")
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if detach {
			child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		}
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		if pidFile != "" {
			if err := os.WriteFile(pidFile, []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
				os.Exit(3)
			}
		}
	}
	switch mode {
	case "child-sleeper":
		// Leader stays alive; the descendant inherits the output pipe and
		// belongs to the helper's process group.
		spawn(false)
		time.Sleep(30 * time.Second)
	case "exit-group-child":
		// Leader exits while a same-group descendant holds the pipe.
		spawn(false)
	case "exit-detached-child":
		// Leader exits while a deliberately detached descendant (own
		// process group, unreachable by the group kill) holds the pipe —
		// WaitDelay alone must bound the drain.
		spawn(true)
	}
	os.Exit(0)
}

// helperCmd builds the re-executed test binary with the same teardown setup
// newGitCmd applies to real git invocations.
func helperCmd(t *testing.T, mode, pidFile string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestGitHelperProcess")
	cmd.Env = append(os.Environ(),
		"AMUX_GIT_TEST_HELPER="+mode,
		"AMUX_GIT_HELPER_PIDFILE="+pidFile,
	)
	process.SetProcessGroup(cmd)
	cmd.WaitDelay = gitOutputDrainDelay
	var sink strings.Builder
	cmd.Stdout = &sink
	cmd.Stderr = &sink
	return cmd
}

func readHelperPID(t *testing.T, pidFile string) int {
	t.Helper()
	var raw []byte
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(pidFile); err == nil && len(b) > 0 {
			raw = b
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		t.Fatalf("helper wrote no valid pid to %q: %q err=%v", pidFile, raw, err)
	}
	return pid
}

func waitForPIDExit(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("descendant pid %d still running %v after cancellation", pid, timeout)
}

func TestRunGitCommandCancelTerminatesProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	cmd := helperCmd(t, "child-sleeper", pidFile)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		killed bool
		err    error
	}
	done := make(chan result, 1)
	go func() {
		killed, err := runGitCommand(ctx, cmd)
		done <- result{killed, err}
	}()

	// Readiness barrier: cancel only once the descendant is spawned and its
	// pid recorded, so the deadline cannot beat helper startup under load.
	childPID := readHelperPID(t, pidFile)
	start := time.Now()
	cancel()
	res := <-done
	elapsed := time.Since(start)

	// group grace (200ms) + drain bound (500ms) + generous slack.
	if elapsed > 4*time.Second {
		t.Fatalf("cancellation took %v, want bounded return", elapsed)
	}
	if !res.killed {
		t.Fatalf("expected killedByContext=true after cancellation kill")
	}
	if res.err == nil {
		t.Fatalf("expected error from killed command, got nil")
	}

	waitForPIDExit(t, childPID, 5*time.Second)
}

func TestRunGitCommandWaitDelayBoundsDetachedDescendant(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	cmd := helperCmd(t, "exit-detached-child", pidFile)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	_, err := runGitCommand(ctx, cmd)
	elapsed := time.Since(start)

	// The leader exited cleanly but the detached descendant still holds the
	// pipe: WaitDelay must abort the drain instead of waiting 30s.
	if elapsed > 3*time.Second {
		t.Fatalf("lingering detached descendant blocked Wait for %v", elapsed)
	}
	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("error = %v, want wrapped exec.ErrWaitDelay", err)
	}

	// The detached descendant survived the group kill by design; reap it.
	pid := readHelperPID(t, pidFile)
	if err := process.ForceKillProcess(pid); err != nil {
		t.Fatalf("cleanup kill of pid %d: %v", pid, err)
	}
	waitForPIDExit(t, pid, 5*time.Second)
}

func TestRunGitCommandCancelWhileDescendantHoldsPipe(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	cmd := helperCmd(t, "exit-group-child", pidFile)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		_, _ = runGitCommand(ctx, cmd)
		close(done)
	}()

	// The leader exits fast; cancel once the descendant is spawned so the
	// group kill lands while it still holds the output pipe.
	childPID := readHelperPID(t, pidFile)
	start := time.Now()
	cancel()
	<-done
	elapsed := time.Since(start)

	if elapsed > 4*time.Second {
		t.Fatalf("cancellation with lingering child took %v, want bounded return", elapsed)
	}
	waitForPIDExit(t, childPID, 5*time.Second)
}

// TestRunGitCtxDeadlineTerminatesAliasDescendants drives the real git wrapper:
// a `!` alias runs through sh, so the sleeper is a descendant of the git
// leader. Only the group kill bounds this; the leader alone would leave the
// sleeper holding the pipes until its natural exit (30s).
func TestRunGitCtxDeadlineTerminatesAliasDescendants(t *testing.T) {
	skipIfNoGit(t)
	repo := initRepo(t)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := RunGitCtx(ctx, repo, "-c", "alias.hang=!sleep 30", "hang")
	elapsed := time.Since(start)

	if elapsed > 4*time.Second {
		t.Fatalf("RunGitCtx returned after %v, want deadline plus cleanup budget", elapsed)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RunGitCtx error = %v, want wrapped context.DeadlineExceeded", err)
	}
}
