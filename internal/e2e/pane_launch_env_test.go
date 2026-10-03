package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/panelaunch"
)

// TestPaneLaunchRealBinary exercises the private pane-launch transport
// through the built amux binary — not the test-binary TestMain seam. This
// catches mistakes the in-process dispatch could hide: wrong argv shape,
// dispatch ordered after the TTY gate, or a missing helper entry point.
func TestPaneLaunchRealBinary(t *testing.T) {
	bin, _, err := buildAmuxBinary()
	if err != nil {
		t.Fatalf("build amux binary: %v", err)
	}

	run := func(args ...string) (string, string, int) {
		cmd := exec.Command(bin, args...)
		var outBuf, errBuf strings.Builder
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf
		err := cmd.Run()
		if err == nil {
			return outBuf.String(), errBuf.String(), 0
		}
		if exit, ok := err.(*exec.ExitError); ok {
			return outBuf.String(), errBuf.String(), exit.ExitCode()
		}
		t.Fatalf("binary %v failed to run: %v", args, err)
		return "", "", -1
	}

	// Redirected streams (no TTY): the private dispatch must handle these
	// before the interactive gate, which would otherwise exit 1.
	t.Run("malformed helper argv is a handled failure", func(t *testing.T) {
		_, stderr, code := run("--internal-pane-launch")
		if code != 2 || !strings.Contains(stderr, "pane launch: invalid invocation") {
			t.Fatalf("code=%d stderr=%q, want usage failure", code, stderr)
		}
	})
	t.Run("discard consumed attempt is silent success", func(t *testing.T) {
		missing := filepath.Join(os.TempDir(), "amux-pane-launch-gone", "payload")
		_, stderr, code := run("--internal-pane-launch", "discard", missing)
		if code != 0 || stderr != "" {
			t.Fatalf("code=%d stderr=%q, want silent success", code, stderr)
		}
	})

	// Subprocess exec path: the real binary consumes a payload and replaces
	// itself, so the command's exit status and output are the binary's.
	t.Run("run consumes payload and execs", func(t *testing.T) {
		workDir := t.TempDir()
		outFile := filepath.Join(workDir, "env.out")
		prep, err := panelaunch.Prepare(workDir,
			fmt.Sprintf("printf '%%s' \"$E2E_MARKER\" > %q; exit 3", outFile),
			[]string{"E2E_MARKER=real-binary-marker"})
		if err != nil {
			t.Fatalf("prepare: %v", err)
		}
		_, stderr, code := run("--internal-pane-launch", "run", prep.Path())
		if code != 3 {
			t.Fatalf("code=%d stderr=%q, want command exit status 3", code, stderr)
		}
		raw, err := os.ReadFile(outFile)
		if err != nil || string(raw) != "real-binary-marker" {
			t.Fatalf("env delivery = %q, %v", raw, err)
		}
		if _, err := os.Lstat(prep.Path()); !os.IsNotExist(err) {
			t.Fatal("payload survived consumption")
		}
	})

	// Real isolated tmux pane: the invocation shape that lands in
	// pane_start_command must carry no env values while the payload still
	// delivers them inside the pane.
	t.Run("real tmux pane", func(t *testing.T) {
		tmuxBin, err := exec.LookPath("tmux")
		if err != nil {
			t.Skipf("tmux unavailable: %v", err)
		}
		serverName := fmt.Sprintf("amux-e2e-pl-%d", time.Now().UnixNano())
		t.Cleanup(func() {
			_ = exec.Command(tmuxBin, "-L", serverName, "kill-server").Run()
		})

		workDir := t.TempDir()
		outFile := filepath.Join(workDir, "env.out")
		const marker = "pane-env-marker-7c1"
		prep, err := panelaunch.Prepare(workDir,
			fmt.Sprintf("env > %q; sleep 30", outFile),
			[]string{"E2E_MARKER=" + marker})
		if err != nil {
			t.Fatalf("prepare: %v", err)
		}
		session := "e2e-pane-launch"
		invocation := fmt.Sprintf(
			"'%s' '--internal-pane-launch' 'run' '%s'", bin, prep.Path(),
		)
		out, err := exec.Command(tmuxBin,
			"-L", serverName, "-f", "/dev/null",
			"new-session", "-d", "-s", session, "-x", "80", "-y", "24",
			"-c", workDir, invocation).CombinedOutput()
		if err != nil {
			t.Fatalf("new-session: %v\n%s", err, out)
		}

		// The stored pane command shows the private argv, never the marker.
		startCmd, err := exec.Command(tmuxBin,
			"-L", serverName,
			"display-message", "-p", "-t", session, "#{pane_start_command}").Output()
		if err != nil {
			t.Fatalf("read pane_start_command: %v", err)
		}
		if !strings.Contains(string(startCmd), "--internal-pane-launch") {
			t.Fatalf("pane_start_command lacks private invocation: %q", startCmd)
		}
		if strings.Contains(string(startCmd), marker) {
			t.Fatalf("pane_start_command leaked env marker: %q", startCmd)
		}

		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			raw, _ := os.ReadFile(outFile)
			if s := string(raw); s != "" {
				if !strings.Contains(s, "E2E_MARKER="+marker) {
					t.Fatalf("pane env missing marker; env file:\n%s", s)
				}
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatal("pane never wrote env output")
	})
}
