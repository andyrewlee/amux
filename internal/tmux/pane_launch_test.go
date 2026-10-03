package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/panelaunch"
	"github.com/andyrewlee/amux/internal/testutil"
)

// TestMain lets this test binary stand in for the amux launcher: prepared
// pane commands invoke os.Executable, which under `go test` is this binary.
// Dispatch the private helper argv before testing's flag parse or any test
// runs — a pane spawn must never recurse into the test suite.
func TestMain(m *testing.M) {
	if handled, code := panelaunch.HandleInvocation(os.Args[1:]); handled {
		os.Exit(code)
	}
	os.Exit(m.Run())
}

// paneLaunchMarker is a synthetic env-only value: unique per test and never
// part of the configured command, so containment assertions attribute any
// leak to the env transport specifically.
func paneLaunchMarker(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("amux-pl-marker-%d", time.Now().UnixNano())
}

// TestPaneLaunchEnvConfidentiality_CommandText proves the rendered outer
// command never carries env values: the marker travels only inside the
// private payload. (Step 1 red test, now green on the new transport.)
func TestPaneLaunchEnvConfidentiality_CommandText(t *testing.T) {
	marker := paneLaunchMarker(t)
	prepared, err := NewClientCommand("secret-session", ClientCommandParams{
		WorkDir:     t.TempDir(),
		Command:     "echo public",
		Environment: []string{"AMUX_PL_SECRET=" + marker},
		Options:     Options{ServerName: "s", ConfigPath: "/dev/null"},
	})
	if err != nil {
		t.Fatalf("NewClientCommand() error = %v", err)
	}
	defer prepared.AbortBeforeStart()
	if strings.Contains(prepared.Command, marker) {
		t.Fatal("rendered command contains the env marker")
	}
	if strings.Contains(prepared.Command, "AMUX_PL_SECRET") {
		t.Fatal("rendered command contains the env name")
	}
	// Helper argv inside the pane invocation: launcher + op + payload only.
	if !strings.Contains(prepared.Command, "'--internal-pane-launch' 'run' '") {
		t.Fatal("rendered command lacks the private consumer invocation")
	}
	if !strings.Contains(prepared.Command, prepared.PayloadPath()) {
		t.Fatal("rendered command does not name the attempt's payload path")
	}
}

// TestPaneLaunchPreparedCommandOwnership covers the parent-side lifecycle:
// AbortBeforeStart removes the attempt and is safe to call twice, and the
// ensure fragment embeds a best-effort in-band discard for the
// already-present branch.
func TestPaneLaunchPreparedCommandOwnership(t *testing.T) {
	prepared, err := NewClientCommand("own-session", ClientCommandParams{
		WorkDir: t.TempDir(),
		Command: "echo hi",
		Options: Options{ServerName: "s", ConfigPath: "/dev/null"},
	})
	if err != nil {
		t.Fatalf("NewClientCommand() error = %v", err)
	}
	payloadPath := prepared.PayloadPath()
	if _, err := os.Lstat(payloadPath); err != nil {
		t.Fatalf("payload missing before start: %v", err)
	}
	if !strings.Contains(prepared.Command, "'--internal-pane-launch' 'discard' '") {
		t.Fatal("ensure fragment lacks the already-present discard branch")
	}
	if !strings.Contains(prepared.Command, "pane_start_command") {
		t.Fatal("ensure fragment lacks the lost-race start-command proof")
	}
	prepared.AbortBeforeStart()
	if _, err := os.Lstat(payloadPath); !os.IsNotExist(err) {
		t.Fatalf("payload survived AbortBeforeStart: %v", err)
	}
	attemptDir := filepath.Dir(payloadPath)
	if _, err := os.Lstat(attemptDir); !os.IsNotExist(err) {
		t.Fatalf("attempt dir survived AbortBeforeStart: %v", err)
	}
	prepared.AbortBeforeStart() // double discard must be a no-op
}

// TestPaneLaunchDeliversEnvOnFreshServer runs the full create pipeline
// against a real tmux server and proves the layered environment reaches the
// pane process while staying out of every transport surface.
func TestPaneLaunchDeliversEnvOnFreshServer(t *testing.T) {
	opts := realTmuxServerWithKeepalive(t)
	marker := paneLaunchMarker(t)
	outFile := filepath.Join(t.TempDir(), "env.out")
	workDir := t.TempDir()
	const session = "pl-fresh"

	prepared, err := NewClientCommand(session, ClientCommandParams{
		WorkDir:     workDir,
		Command:     fmt.Sprintf("env > %s; sleep 60", shellQuoteForTest(outFile)),
		Environment: []string{"AMUX_PL_MARKER=" + marker, "AMUX_PL_EMPTY=", "AMUX_PL_SPACE=a b"},
		Options:     opts,
	})
	if err != nil {
		t.Fatalf("NewClientCommand() error = %v", err)
	}
	t.Cleanup(prepared.AbortBeforeStart)
	if strings.Contains(prepared.Command, marker) {
		t.Fatal("outer command contains the env marker")
	}
	_ = exec.Command("sh", "-c", prepared.Command).Run()
	waitForSessionExists(t, opts, session)

	// pane_start_command stores exactly what argv carried — the helper argv,
	// not the payload contents.
	startCmd := paneStartCommand(t, opts, session)
	if strings.Contains(startCmd, marker) || strings.Contains(startCmd, "AMUX_PL_MARKER") {
		t.Fatal("pane_start_command contains the env transport")
	}
	if !strings.Contains(startCmd, prepared.PayloadPath()) {
		t.Fatal("pane_start_command does not name the payload path")
	}

	got := waitForFile(t, outFile)
	assertEnvLine(t, got, "AMUX_PL_MARKER", marker)
	assertEnvLine(t, got, "AMUX_PL_EMPTY", "")
	assertEnvLine(t, got, "AMUX_PL_SPACE", "a b")
	if envLine(got, "TMUX") != "" || envLine(got, "TMUX_PANE") != "" {
		t.Fatal("TMUX/TMUX_PANE leaked into the delivered environment")
	}
}

// TestPaneLaunchDeliversEnvOnExistingServer seeds an already-running server
// with its own environment and proves captured assignments override
// collisions while server-only defaults survive.
func TestPaneLaunchDeliversEnvOnExistingServer(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	serverName := fmt.Sprintf("amux-pl-existing-%d", time.Now().UnixNano())
	opts := Options{ServerName: serverName, ConfigPath: "/dev/null", CommandTimeout: 5 * time.Second}
	keep := exec.Command("tmux", tmuxArgs(opts, "new-session", "-d", "-s", "_keepalive", "sleep", "300")...)
	// The keepalive launches the server; its env becomes the server env that
	// panes inherit — seed a server-only default and a stale conflicting var.
	keep.Env = append(os.Environ(),
		"AMUX_PL_SERVER_ONLY=server-value",
		"AMUX_PL_CONFLICT=old-value",
	)
	if out, err := keep.CombinedOutput(); err != nil {
		t.Skipf("tmux unusable: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", serverName, "kill-server").Run()
	})

	marker := paneLaunchMarker(t)
	outFile := filepath.Join(t.TempDir(), "env.out")
	const session = "pl-existing"
	prepared, err := NewClientCommand(session, ClientCommandParams{
		WorkDir:     t.TempDir(),
		Command:     fmt.Sprintf("env > %s; sleep 60", shellQuoteForTest(outFile)),
		Environment: []string{"AMUX_PL_CONFLICT=" + marker},
		Options:     opts,
	})
	if err != nil {
		t.Fatalf("NewClientCommand() error = %v", err)
	}
	t.Cleanup(prepared.AbortBeforeStart)
	_ = exec.Command("sh", "-c", prepared.Command).Run()
	waitForSessionExists(t, opts, session)

	got := waitForFile(t, outFile)
	assertEnvLine(t, got, "AMUX_PL_CONFLICT", marker)
	assertEnvLine(t, got, "AMUX_PL_SERVER_ONLY", "server-value")
}

// TestPaneLaunchReattachDiscardsUnusedAttempt exercises the already-present
// branch: the second prepared command for a live session must discard its
// own payload in-band while the running pane keeps its original env.
func TestPaneLaunchReattachDiscardsUnusedAttempt(t *testing.T) {
	opts := realTmuxServerWithKeepalive(t)
	const session = "pl-reattach"
	firstMarker := paneLaunchMarker(t)
	outFile := filepath.Join(t.TempDir(), "env.out")

	first, err := NewClientCommand(session, ClientCommandParams{
		WorkDir:     t.TempDir(),
		Command:     fmt.Sprintf("env > %s; sleep 60", shellQuoteForTest(outFile)),
		Environment: []string{"AMUX_PL_MARKER=" + firstMarker},
		Options:     opts,
	})
	if err != nil {
		t.Fatalf("NewClientCommand() error = %v", err)
	}
	t.Cleanup(first.AbortBeforeStart)
	_ = exec.Command("sh", "-c", first.Command).Run()
	waitForSessionExists(t, opts, session)

	secondMarker := paneLaunchMarker(t)
	second, err := NewClientCommand(session, ClientCommandParams{
		WorkDir:     t.TempDir(),
		Command:     "echo never-runs",
		Environment: []string{"AMUX_PL_MARKER=" + secondMarker},
		Options:     opts,
	})
	if err != nil {
		t.Fatalf("NewClientCommand() error = %v", err)
	}
	secondPayload := second.PayloadPath()
	_ = exec.Command("sh", "-c", second.Command).Run()

	// The already-present branch runs the discard argv inline; the attempt
	// must be gone by the time the script returns (allow a small poll for
	// the in-band helper process to finish).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Lstat(secondPayload); os.IsNotExist(err) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Lstat(secondPayload); !os.IsNotExist(err) {
		t.Fatalf("unused payload survived the already-present discard: %v", err)
	}
	second.AbortBeforeStart() // idempotent after in-band discard

	got := waitForFile(t, outFile)
	assertEnvLine(t, got, "AMUX_PL_MARKER", firstMarker)
	if strings.Contains(got, secondMarker) {
		t.Fatal("reattach leaked the unused payload's env into the live pane")
	}
}

// TestPaneLaunchDetachedRunDeliversEnv covers the detached run-session shape:
// env delivery, remain-on-exit forensics, and a provably clean transport.
func TestPaneLaunchDetachedRunDeliversEnv(t *testing.T) {
	opts := realTmuxServerWithKeepalive(t)
	marker := paneLaunchMarker(t)
	outFile := filepath.Join(t.TempDir(), "run.env")

	if err := EnsureDetachedSession("pl-run", t.TempDir(),
		fmt.Sprintf("env > %s; exit 7", shellQuoteForTest(outFile)),
		[]string{"AMUX_PL_MARKER=" + marker}, opts,
		SessionTags{WorkspaceID: "ws-pl", Type: "run"}); err != nil {
		t.Fatalf("EnsureDetachedSession() error = %v", err)
	}
	_, alive, exitCode := waitForSessionStatus(t, "pl-run", opts,
		func(exists, alive bool, code int) bool { return exists && !alive })
	if alive {
		t.Fatal("run pane should have exited")
	}
	if exitCode != 7 {
		t.Fatalf("exitCode = %d, want 7", exitCode)
	}
	startCmd := paneStartCommand(t, opts, "pl-run")
	if strings.Contains(startCmd, marker) {
		t.Fatal("dead pane's start command contains the env marker")
	}
	got := waitForFile(t, outFile)
	assertEnvLine(t, got, "AMUX_PL_MARKER", marker)
}

// TestPaneLaunchTwoWorkspacesIsolated runs two simultaneous launches with
// different markers and proves neither env bleeds into the other.
func TestPaneLaunchTwoWorkspacesIsolated(t *testing.T) {
	opts := realTmuxServerWithKeepalive(t)
	markerA, markerB := paneLaunchMarker(t), paneLaunchMarker(t)+"-b"
	outA := filepath.Join(t.TempDir(), "a.env")
	outB := filepath.Join(t.TempDir(), "b.env")

	for i, tc := range []struct{ session, out, marker string }{
		{"pl-ws-a", outA, markerA},
		{"pl-ws-b", outB, markerB},
	} {
		prepared, err := NewClientCommand(tc.session, ClientCommandParams{
			WorkDir:     t.TempDir(),
			Command:     fmt.Sprintf("env > %s; sleep 60", shellQuoteForTest(tc.out)),
			Environment: []string{"AMUX_PL_MARKER=" + tc.marker},
			Options:     opts,
		})
		if err != nil {
			t.Fatalf("NewClientCommand(%d) error = %v", i, err)
		}
		t.Cleanup(prepared.AbortBeforeStart)
		_ = exec.Command("sh", "-c", prepared.Command).Run()
		waitForSessionExists(t, opts, tc.session)
	}

	envA := waitForFile(t, outA)
	envB := waitForFile(t, outB)
	assertEnvLine(t, envA, "AMUX_PL_MARKER", markerA)
	assertEnvLine(t, envB, "AMUX_PL_MARKER", markerB)
}

func shellQuoteForTest(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
}

// paneStartCommand reads the stored start command of a session's first pane.
func paneStartCommand(t *testing.T, opts Options, session string) string {
	t.Helper()
	out, err := exec.Command("tmux", tmuxArgs(opts, "list-panes", "-t", session, "-F", "#{pane_start_command}")...).CombinedOutput()
	if err != nil {
		t.Fatalf("list-panes %s: %v\n%s", session, err, out)
	}
	return strings.TrimSpace(string(out))
}

// waitForFile polls until path holds non-empty content (the pane writes it
// asynchronously) and returns it.
func waitForFile(t *testing.T, path string) string {
	t.Helper()
	content := testutil.PollUntil(10*time.Second, 25*time.Millisecond, func() (string, bool) {
		raw, err := os.ReadFile(path)
		if err != nil || len(raw) == 0 {
			return "", false
		}
		return string(raw), true
	})
	if content == "" {
		t.Fatalf("pane never wrote %s", path)
	}
	return content
}

// envLine extracts NAME's value from `env` output (first '=' wins).
func envLine(envDump, name string) string {
	for _, line := range strings.Split(envDump, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok && k == name {
			return v
		}
	}
	return ""
}

func assertEnvLine(t *testing.T, envDump, name, want string) {
	t.Helper()
	if got := envLine(envDump, name); got != want {
		t.Fatalf("env %s not delivered as captured", name)
	}
}
