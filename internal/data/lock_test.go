//go:build !windows

package data

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

// The .lock files in this package are flock(LOCK_EX) rendezvous files whose
// entire reason to exist is to serialize two *separate* amux processes touching
// the same workspace/registry data. The same-process goroutine tests
// (TestLockRegistryFileRetriesWhenWaiterAcquiresUnlinkedInode) can't prove that:
// a Go process holds a single open-file-description lock, so a second
// goroutine's flock in the same process can observe different semantics than a
// genuinely separate process would. This test re-execs the test binary as a
// child, has the child hold the exclusive lock, and asserts the parent's
// concurrent acquire blocks until the child releases.

const (
	flockChildEnv     = "AMUX_FLOCK_CHILD_LOCK_PATH"
	flockChildReady   = "amux-flock-child-acquired-lock"
	flockChildRelease = "release"
)

// TestMain lets the parent re-exec this test binary in a "child" mode that
// acquires the real cross-process flock. When AMUX_FLOCK_CHILD_LOCK_PATH is set
// we never run the normal test suite; we just behave as the lock-holding child.
func TestMain(m *testing.M) {
	if lockPath := os.Getenv(flockChildEnv); lockPath != "" {
		os.Exit(runFlockChild(lockPath, os.Stdin, os.Stdout))
	}
	os.Exit(m.Run())
}

// runFlockChild acquires the exclusive lock at lockPath via the production
// lockRegistryFile path, signals readiness on out, then blocks until the parent
// writes the release line on in before unlocking. It returns the process exit
// code.
func runFlockChild(lockPath string, in io.Reader, out io.Writer) int {
	lock, err := lockRegistryFile(lockPath, false)
	if err != nil {
		fmt.Fprintf(out, "child lock error: %v\n", err)
		return 2
	}
	// Announce that the exclusive lock is held. The parent uses this token,
	// not a sleep, to know the lock is contended.
	if _, err := fmt.Fprintln(out, flockChildReady); err != nil {
		unlockRegistryFile(lock)
		return 3
	}
	// Hold the lock until the parent explicitly tells us to release. This keeps
	// the contended window open for exactly as long as the parent needs, with no
	// timing assumptions.
	scanner := bufio.NewScanner(in)
	for scanner.Scan() {
		if scanner.Text() == flockChildRelease {
			break
		}
	}
	unlockRegistryFile(lock)
	return 0
}

// TestLockRegistryFileCrossProcessMutualExclusion proves the .lock flock
// actually serializes two separate OS processes (the contract the lock exists
// for), not just same-process goroutines.
func TestLockRegistryFileCrossProcessMutualExclusion(t *testing.T) {
	lockPath := newCrossProcessLockPath(t)

	child := newFlockChild(t, lockPath)
	child.waitUntilHoldsLock(t)

	// While the child holds the exclusive lock, the parent's acquire must block.
	acquired := make(chan struct{})
	acqErr := make(chan error, 1)
	go func() {
		file, err := lockRegistryFile(lockPath, false)
		if err != nil {
			acqErr <- err
			return
		}
		// Hand the file back for cleanup on the parent goroutine.
		t.Cleanup(func() { unlockRegistryFile(file) })
		close(acquired)
	}()

	// The parent must NOT acquire while the child holds the lock. A short window
	// is enough to catch a broken lock (it would acquire essentially instantly).
	select {
	case <-acquired:
		t.Fatal("parent acquired the lock while the child still held it: cross-process mutual exclusion is broken")
	case err := <-acqErr:
		t.Fatalf("parent lockRegistryFile() error while child held lock: %v", err)
	case <-time.After(300 * time.Millisecond):
		// Expected: still blocked.
	}

	// Tell the child to release, then the parent's pending acquire must succeed.
	child.signalRelease(t)

	select {
	case <-acquired:
		// Expected: parent acquired the lock once the child released it.
	case err := <-acqErr:
		t.Fatalf("parent lockRegistryFile() error after child released: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("parent never acquired the lock after the child released it: handoff is broken")
	}

	child.waitExit(t)
}

// flockChild is a handle to the re-execed test binary running in child lock
// mode, wired up to the child's stdin/stdout.
type flockChild struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
}

func newFlockChild(t *testing.T, lockPath string) *flockChild {
	t.Helper()
	// Re-exec only this test's TestMain child branch; -test.run is a no-op in
	// the child because TestMain exits before m.Run(), but pin it anyway so the
	// child never runs the broader suite if the env var were ever lost.
	cmd := exec.Command(os.Args[0], "-test.run=TestLockRegistryFileCrossProcessMutualExclusion")
	cmd.Env = append(os.Environ(), flockChildEnv+"="+lockPath)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("child StdinPipe(): %v", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("child StdoutPipe(): %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}

	child := &flockChild{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdoutPipe)}
	// Backstop: never leak the child even if an assertion fails mid-test.
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return child
}

// waitUntilHoldsLock blocks until the child reports it has acquired the
// exclusive lock, reading its readiness token rather than sleeping.
func (c *flockChild) waitUntilHoldsLock(t *testing.T) {
	t.Helper()
	line := make(chan string, 1)
	readErr := make(chan error, 1)
	go func() {
		text, err := c.stdout.ReadString('\n')
		if err != nil {
			readErr <- err
			return
		}
		line <- text
	}()

	select {
	case text := <-line:
		if got := trimLine(text); got != flockChildReady {
			t.Fatalf("child readiness line = %q, want %q", got, flockChildReady)
		}
	case err := <-readErr:
		t.Fatalf("reading child readiness: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("child never signaled it acquired the lock")
	}
}

// signalRelease tells the child to drop the lock.
func (c *flockChild) signalRelease(t *testing.T) {
	t.Helper()
	if _, err := io.WriteString(c.stdin, flockChildRelease+"\n"); err != nil {
		t.Fatalf("signal child release: %v", err)
	}
}

// waitExit asserts the child exited cleanly after releasing the lock.
func (c *flockChild) waitExit(t *testing.T) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("child exited with error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child did not exit after releasing the lock")
	}
}

// newCrossProcessLockPath returns a workspace-store .lock path under a temp root
// so the test exercises the same path layout production uses.
func newCrossProcessLockPath(t *testing.T) string {
	t.Helper()
	store := NewWorkspaceStore(t.TempDir())
	return store.workspaceLockPath("cross-process-lock")
}

func trimLine(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
