package e2e

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

var (
	fakeAgentOnce sync.Once
	fakeAgentPath string
	fakeAgentErr  error
)

// fakeAgentBuildDirPrefix marks the directories this test process creates and
// owns for the shared fakeagent binary; the cleanup guard keys on it.
const fakeAgentBuildDirPrefix = "amux-fakeagent-"

// buildFakeAgentBinary owns the fakeagent build directory lifecycle: it creates
// an amux-fakeagent-* directory under destRoot, runs the injected build step,
// and removes the directory immediately if the build fails. On success the
// directory is kept — the binary is process-shared, so process-level cleanup
// (cleanupBuiltFakeAgent from TestMain) removes it after every test finishes.
// It never creates or deletes caller-owned directories.
func buildFakeAgentBinary(destRoot string, runBuild func(outPath string) error) (string, error) {
	dir, err := os.MkdirTemp(destRoot, fakeAgentBuildDirPrefix+"*")
	if err != nil {
		return "", err
	}
	out := filepath.Join(dir, "fakeagent")
	if err := runBuild(out); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return out, nil
}

// cleanupBuiltFakeAgent removes the shared fakeagent build directory after all
// tests in the process finish. It is a no-op when no binary was built and
// refuses paths outside the owned naming convention.
func cleanupBuiltFakeAgent() error {
	return cleanupOwnedFakeAgentDir(fakeAgentPath)
}

// cleanupOwnedFakeAgentDir removes the directory containing binPath after
// checking it carries this process's owned naming convention. An empty path is
// a no-op; an unrelated directory is refused, never removed.
func cleanupOwnedFakeAgentDir(binPath string) error {
	if binPath == "" {
		return nil
	}
	dir := filepath.Dir(binPath)
	if !strings.HasPrefix(filepath.Base(dir), fakeAgentBuildDirPrefix) {
		return fmt.Errorf("refusing to remove unexpected fakeagent directory %q", dir)
	}
	return os.RemoveAll(dir)
}

// buildFakeAgent compiles internal/e2e/fakeagent once per test binary and returns
// the resulting executable path. Reused by the full close-the-loop E2E test.
func buildFakeAgent(t *testing.T) string {
	t.Helper()
	fakeAgentOnce.Do(func() {
		root, err := repoRoot()
		if err != nil {
			fakeAgentErr = err
			return
		}
		out, err := buildFakeAgentBinary(os.TempDir(), func(outPath string) error {
			cmd := exec.Command("go", "build", "-o", outPath, "./internal/e2e/fakeagent")
			cmd.Dir = root
			if combined, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("build fakeagent: %w\n%s", err, combined)
			}
			return nil
		})
		if err != nil {
			fakeAgentErr = err
			return
		}
		fakeAgentPath = out
	})
	if fakeAgentErr != nil {
		t.Fatalf("build fake agent: %v", fakeAgentErr)
	}
	return fakeAgentPath
}

// TestFakeAgentRecordsRawCarriageReturn exercises the fixture in isolation over a
// bare PTY (no tmux, no amux), so it runs on every platform. It proves the
// property every close-the-loop input test depends on: keystrokes — including a
// literal carriage return (0x0D) — are recorded exactly, not translated to NL.
func TestFakeAgentRecordsRawCarriageReturn(t *testing.T) {
	t.Parallel()
	bin := buildFakeAgent(t)
	logPath := filepath.Join(t.TempDir(), "received.log")

	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "FAKEAGENT_LOG="+logPath)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("pty start: %v", err)
	}
	t.Cleanup(func() {
		// Kill first: closing the PTY master alone does not reliably unblock the
		// slave's read on macOS, so the agent (and cmd.Wait) would hang. Killing
		// the process closes the slave, which EOFs the master and drains cleanly.
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		_ = ptmx.Close()
	})

	// Drain PTY output so we can observe the readiness banner.
	var mu sync.Mutex
	var out bytes.Buffer
	go func() {
		b := make([]byte, 512)
		for {
			n, err := ptmx.Read(b)
			if n > 0 {
				mu.Lock()
				out.Write(b[:n])
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()

	bannerSeen := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return bytes.Contains(out.Bytes(), []byte("FAKEAGENT READY"))
	}
	waitForCond(t, bannerSeen, 5*time.Second, "fake agent never signaled readiness")

	// Deliver text + a literal CR the way amux delivers agent input. In raw mode
	// the agent must record 0x0D, not a line-discipline-translated NL.
	if _, err := ptmx.Write([]byte("hello\r")); err != nil {
		t.Fatalf("write to pty: %v", err)
	}

	want := []byte{0x68, 0x65, 0x6c, 0x6c, 0x6f, 0x0d} // "hello" + CR
	got, ok := waitForFileBytes(logPath, want, 5*time.Second)
	if !ok {
		t.Fatalf("fake agent did not record raw bytes\n got: % x\nwant: % x", got, want)
	}
}
