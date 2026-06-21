package tmux

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// This file covers the error-classification *glue* — the wiring that decides,
// for a given (output, error) from the exec choke point, whether to return an
// empty result, swallow the error, or wrap/propagate it. Before the runTmuxCmd /
// runTmuxCmdCombined var-seams (tmux_runner.go), these branches were reachable
// only when a real tmux server produced the exact failure. Here we swap the
// seam for a fake and drive every branch without a live tmux server.
//
// The pure classifiers (isExitCode1 / isSessionNotFoundStderr / isNoClientStderr
// / isOptionMissingStderr) are tested in errors_test.go; this file pins how each
// read/mutate site *reacts* to them.

// exitCode1Err returns a genuine *exec.ExitError whose ExitCode() == 1 by
// running a real subprocess that exits 1. tmux signals "not found" this way, and
// isExitCode1 unwraps via errors.As(*exec.ExitError), so a fabricated error type
// would not exercise the real classifier. Built once per test.
func exitCode1Err(t *testing.T) error {
	t.Helper()
	err := exec.Command("sh", "-c", "exit 1").Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("setup: expected exec.ExitError with code 1, got %v", err)
	}
	return err
}

// fakeRunTmuxCmd installs a runTmuxCmd seam returning the given output/err and
// restores the original on cleanup.
func fakeRunTmuxCmd(t *testing.T, output []byte, err error) {
	t.Helper()
	orig := runTmuxCmd
	runTmuxCmd = func(*exec.Cmd) ([]byte, error) { return output, err }
	t.Cleanup(func() { runTmuxCmd = orig })
}

// fakeRunTmuxCmdCombined installs a runTmuxCmdCombined seam.
func fakeRunTmuxCmdCombined(t *testing.T, output []byte, err error) {
	t.Helper()
	orig := runTmuxCmdCombined
	runTmuxCmdCombined = func(*exec.Cmd) ([]byte, error) { return output, err }
	t.Cleanup(func() { runTmuxCmdCombined = orig })
}

// testOpts disables EnsureAvailable's dependence on a real tmux by pointing at a
// throwaway server name; the seam short-circuits the actual exec, but
// EnsureAvailable still needs tmux on PATH for the exported entry points.
func testOpts() Options {
	opts := DefaultOptions()
	opts.ServerName = "amux-runner-seam-test"
	return opts
}

// ---------------------------------------------------------------------------
// listTmux: exit 1 -> empty, other error -> propagated, success -> parsed.
// ---------------------------------------------------------------------------

func TestListTmux_ExitCode1MeansEmpty(t *testing.T) {
	fakeRunTmuxCmd(t, nil, exitCode1Err(t))
	lines, err := listTmux(testOpts(), "list-panes")
	if err != nil {
		t.Fatalf("exit 1 should be swallowed as empty, got err: %v", err)
	}
	if lines != nil {
		t.Fatalf("exit 1 should yield nil lines, got %#v", lines)
	}
}

func TestListTmux_OtherErrorPropagates(t *testing.T) {
	want := errors.New("no server running on /tmp/x")
	fakeRunTmuxCmd(t, nil, want)
	lines, err := listTmux(testOpts(), "list-panes")
	if !errors.Is(err, want) {
		t.Fatalf("non-exit-1 error must propagate, got lines=%#v err=%v", lines, err)
	}
}

func TestListTmux_SuccessParsesLines(t *testing.T) {
	fakeRunTmuxCmd(t, []byte("sess-a\t0\nsess-b\t1\n"), nil)
	lines, err := listTmux(testOpts(), "list-panes")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := []string{"sess-a\t0", "sess-b\t1"}
	if len(lines) != len(want) {
		t.Fatalf("got %#v, want %#v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d: got %q want %q", i, lines[i], want[i])
		}
	}
}

// ---------------------------------------------------------------------------
// runTmux: exit 1 -> nil error (mutation against a missing target is fine),
// other error -> propagated.
// ---------------------------------------------------------------------------

func TestRunTmux_ExitCode1IsNotAnError(t *testing.T) {
	fakeRunTmuxCmd(t, nil, exitCode1Err(t))
	if err := runTmux(testOpts(), "kill-session", "-t", "=gone"); err != nil {
		t.Fatalf("exit 1 must be treated as success, got %v", err)
	}
}

func TestRunTmux_OtherErrorPropagates(t *testing.T) {
	want := errors.New("connection refused")
	fakeRunTmuxCmd(t, nil, want)
	if err := runTmux(testOpts(), "kill-session", "-t", "=x"); !errors.Is(err, want) {
		t.Fatalf("non-exit-1 error must propagate, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// SessionNamesWithClients (CombinedOutput): exit 1 + no-client/empty stderr ->
// empty set, exit 1 + other stderr -> error, success -> parsed.
// ---------------------------------------------------------------------------

func TestSessionNamesWithClients_NoClientStderrIsEmpty(t *testing.T) {
	skipIfNoTmux(t)
	fakeRunTmuxCmdCombined(t, []byte("no client found"), exitCode1Err(t))
	got, err := SessionNamesWithClients(testOpts())
	if err != nil {
		t.Fatalf("no-client stderr must be swallowed, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty attached set, got %#v", got)
	}
}

func TestSessionNamesWithClients_EmptyStderrIsEmpty(t *testing.T) {
	skipIfNoTmux(t)
	fakeRunTmuxCmdCombined(t, nil, exitCode1Err(t))
	got, err := SessionNamesWithClients(testOpts())
	if err != nil {
		t.Fatalf("exit 1 with empty stderr must be swallowed, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty attached set, got %#v", got)
	}
}

func TestSessionNamesWithClients_OtherStderrIsError(t *testing.T) {
	skipIfNoTmux(t)
	exitErr := exitCode1Err(t)
	fakeRunTmuxCmdCombined(t, []byte("lost server"), exitErr)
	got, err := SessionNamesWithClients(testOpts())
	if err == nil {
		t.Fatalf("exit 1 with unrelated stderr must surface as error, got set=%#v", got)
	}
	if len(got) != 0 {
		t.Fatalf("error path should not populate the set, got %#v", got)
	}
}

func TestSessionNamesWithClients_SuccessParsesNames(t *testing.T) {
	skipIfNoTmux(t)
	fakeRunTmuxCmdCombined(t, []byte("amux-a\namux-b\n"), nil)
	got, err := SessionNamesWithClients(testOpts())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !got["amux-a"] || !got["amux-b"] || len(got) != 2 {
		t.Fatalf("expected {amux-a, amux-b}, got %#v", got)
	}
}

// ---------------------------------------------------------------------------
// SetSessionTagValues (CombinedOutput): the has-session pre-check runs through
// runTmuxCmd, then the set-option runs through runTmuxCmdCombined. We drive both
// seams to reach the session-not-found-stderr -> nil and other-stderr -> wrapped
// branches without a live server.
// ---------------------------------------------------------------------------

// withExistingSession makes hasSession (which goes through runTmuxCmd) report
// the session exists, so SetSessionTagValues proceeds to the set-option call.
func withExistingSession(t *testing.T) {
	t.Helper()
	orig := runTmuxCmd
	// has-session success: zero exit, no output needed.
	runTmuxCmd = func(*exec.Cmd) ([]byte, error) { return nil, nil }
	t.Cleanup(func() { runTmuxCmd = orig })
}

func TestSetSessionTagValues_SessionNotFoundStderrReturnsNil(t *testing.T) {
	skipIfNoTmux(t)
	withExistingSession(t)
	fakeRunTmuxCmdCombined(t, []byte("no such session: amux-x"), exitCode1Err(t))
	err := SetSessionTagValues("amux-x", []OptionValue{{Key: "@amux_k", Value: "v"}}, testOpts())
	if err != nil {
		t.Fatalf("session-gone stderr must be swallowed, got %v", err)
	}
}

func TestSetSessionTagValues_OtherStderrIsWrappedError(t *testing.T) {
	skipIfNoTmux(t)
	withExistingSession(t)
	fakeRunTmuxCmdCombined(t, []byte("server exited unexpectedly"), exitCode1Err(t))
	err := SetSessionTagValues("amux-x", []OptionValue{{Key: "@amux_k", Value: "v"}}, testOpts())
	if err == nil {
		t.Fatalf("unrelated stderr on exit 1 must surface as error")
	}
	if !strings.Contains(err.Error(), "server exited unexpectedly") {
		t.Fatalf("wrapped error should include the stderr, got %v", err)
	}
}

func TestSetSessionTagValues_SuccessReturnsNil(t *testing.T) {
	skipIfNoTmux(t)
	withExistingSession(t)
	fakeRunTmuxCmdCombined(t, nil, nil)
	err := SetSessionTagValues("amux-x", []OptionValue{{Key: "@amux_k", Value: "v"}}, testOpts())
	if err != nil {
		t.Fatalf("successful set-option must return nil, got %v", err)
	}
}

// TestRunTmuxCmdSeamsDefaultToRealImpl guards that the default seam wiring runs
// the real cmd output methods (so production behavior is unchanged) by checking
// the defaults actually execute a subprocess and surface its output.
func TestRunTmuxCmdSeamsDefaultToRealImpl(t *testing.T) {
	out, err := runTmuxCmd(exec.Command("sh", "-c", "printf hi"))
	if err != nil || string(out) != "hi" {
		t.Fatalf("default runTmuxCmd should run the real command, got out=%q err=%v", out, err)
	}
	combined, err := runTmuxCmdCombined(exec.Command("sh", "-c", "printf out; printf err 1>&2"))
	if err != nil || !strings.Contains(string(combined), "out") || !strings.Contains(string(combined), "err") {
		t.Fatalf("default runTmuxCmdCombined should capture stdout+stderr, got %q err=%v", combined, err)
	}
}
