package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/panelaunch"
)

// TestMain lets this test binary stand in for the amux launcher in helper
// subprocesses: prepared pane commands invoke os.Executable, which under
// `go test` is this binary. Dispatch the private argv before testing's flag
// parse — a helper subprocess must never recurse into the test suite.
func TestMain(m *testing.M) {
	if handled, code := panelaunch.HandleInvocation(os.Args[1:]); handled {
		os.Exit(code)
	}
	os.Exit(m.Run())
}

// runHelperArgv executes this binary as the pane-launch helper and reports
// its exit code and captured streams. The subprocess has no TTY: reaching
// the helper proves dispatch happens before the interactive gate.
func runHelperArgv(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.Command(exe, args...)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	if err == nil {
		return outBuf.String(), errBuf.String(), 0
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return outBuf.String(), errBuf.String(), exit.ExitCode()
	}
	t.Fatalf("helper subprocess failed to run: %v", err)
	return "", "", -1
}

// TestPaneLaunchArgDispatch pins the binary entry seam: the reserved flag is
// handled with generic errors, malformed shapes exit with usage status, and
// an already-consumed discard is an idempotent success — all without a TTY,
// proving the private dispatch runs before the interactive gate.
func TestPaneLaunchArgDispatch(t *testing.T) {
	t.Run("flag alone is usage failure", func(t *testing.T) {
		_, stderr, code := runHelperArgv(t, "--internal-pane-launch")
		if code != 2 || !strings.Contains(stderr, "pane launch: invalid invocation") {
			t.Fatalf("code=%d stderr=%q, want usage failure", code, stderr)
		}
	})
	t.Run("run without payload is usage failure", func(t *testing.T) {
		_, stderr, code := runHelperArgv(t, "--internal-pane-launch", "run")
		if code != 2 || !strings.Contains(stderr, "pane launch: invalid invocation") {
			t.Fatalf("code=%d stderr=%q, want usage failure", code, stderr)
		}
	})
	t.Run("unknown operation is usage failure", func(t *testing.T) {
		_, stderr, code := runHelperArgv(t, "--internal-pane-launch", "bogus", "/tmp/x")
		if code != 2 || !strings.Contains(stderr, "pane launch: invalid invocation") {
			t.Fatalf("code=%d stderr=%q, want usage failure", code, stderr)
		}
	})
	t.Run("relative payload path is stage failure", func(t *testing.T) {
		_, stderr, code := runHelperArgv(t, "--internal-pane-launch", "run", "relative/payload")
		if code != 1 || !strings.Contains(stderr, "pane launch: invalid invocation") {
			t.Fatalf("code=%d stderr=%q, want stage failure", code, stderr)
		}
	})
	t.Run("discard of consumed attempt is idempotent", func(t *testing.T) {
		missing := filepath.Join(os.TempDir(), "amux-pane-launch-gone", "payload")
		_, stderr, code := runHelperArgv(t, "--internal-pane-launch", "discard", missing)
		if code != 0 || stderr != "" {
			t.Fatalf("code=%d stderr=%q, want silent success", code, stderr)
		}
	})
}

// TestPaneLaunchSubprocessExec runs a real prepared payload through the
// binary subprocess: the helper must consume the payload, chdir, merge the
// env, and replace itself with `sh -lc <command>` so the command's own exit
// status and output are the subprocess's.
func TestPaneLaunchSubprocessExec(t *testing.T) {
	workDir := t.TempDir()
	outFile := filepath.Join(workDir, "out.txt")
	prep, err := panelaunch.Prepare(
		workDir,
		fmt.Sprintf("printf '%%s:%%s' \"$MARKER_A\" \"$MARKER_B\" > %q; exit 7", outFile),
		[]string{"MARKER_A=alpha", "MARKER_B=beta"},
	)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	payloadPath := prep.Path()

	_, stderr, code := runHelperArgv(t, "--internal-pane-launch", "run", payloadPath)
	if code != 7 {
		t.Fatalf("helper exit = %d, want 7 (command exit status); stderr=%q", code, stderr)
	}
	raw, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read command output: %v", err)
	}
	if string(raw) != "alpha:beta" {
		t.Fatalf("command output = %q, want env-merged marker", raw)
	}
	if _, err := os.Lstat(payloadPath); !os.IsNotExist(err) {
		t.Fatal("payload survived consumption")
	}
}

// TestPaneLaunchStderrConfidentiality proves a rejected payload leaks none
// of its contents: the helper's stderr names only the failing stage, never
// env values or command text.
func TestPaneLaunchStderrConfidentiality(t *testing.T) {
	workDir := t.TempDir()
	dir, err := os.MkdirTemp(os.TempDir(), "amux-pane-launch-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	// An expired payload carrying marker content: expired exp=1 (1970).
	raw := fmt.Sprintf(
		`{"v":1,"exp":1,"workdir":%q,"command":%q,"env":[%q]}`,
		base64.StdEncoding.EncodeToString([]byte(workDir)),
		base64.StdEncoding.EncodeToString([]byte("touch SHOULD-NOT-RUN")),
		base64.StdEncoding.EncodeToString([]byte("SECRET=marker-9f2e")),
	)
	payloadPath := filepath.Join(dir, "payload")
	if err := os.WriteFile(payloadPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runHelperArgv(t, "--internal-pane-launch", "run", payloadPath)
	if code == 0 {
		t.Fatal("expired payload executed")
	}
	for _, leak := range []string{"marker-9f2e", "SHOULD-NOT-RUN", "SECRET", payloadPath} {
		if strings.Contains(stderr, leak) {
			t.Fatalf("stderr leaked %q: %q", leak, stderr)
		}
	}
}
