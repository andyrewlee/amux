package e2e

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
		dir, err := os.MkdirTemp("", "amux-fakeagent-*")
		if err != nil {
			fakeAgentErr = err
			return
		}
		out := filepath.Join(dir, "fakeagent")
		cmd := exec.Command("go", "build", "-o", out, "./internal/e2e/fakeagent")
		cmd.Dir = root
		if combined, err := cmd.CombinedOutput(); err != nil {
			fakeAgentErr = fmt.Errorf("build fakeagent: %w\n%s", err, combined)
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
	if !waitForCond(bannerSeen, 5*time.Second) {
		t.Fatal("fake agent never signaled readiness")
	}

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

// waitForCond polls cond until it is true or the timeout elapses.
func waitForCond(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

// waitForFileBytes polls path until it contains want, returning the last-read
// contents and whether want was found.
func waitForFileBytes(path string, want []byte, timeout time.Duration) ([]byte, bool) {
	var last []byte
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil {
			last = b
			if bytes.Contains(b, want) {
				return b, true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return last, false
}
