package tmux

import (
	"errors"
	"os/exec"
	"testing"
)

// This file pins the two tmux queries that gate session garbage collection
// (internal/app/app_tmux_gc.go): the pane-liveness read
// (hasLivePane / SessionStateFor, `list-panes -F '#{pane_dead}'`) and the
// attached-client read (SessionClientCount / SessionHasClients,
// `list-clients -F '#{client_name}'`). A false "dead" reaps a live agent; a
// false "attached" (or a spurious error) leaks or protects the wrong session.
// All four route through the runTmuxCmd var-seam (tmux_runner.go), so every
// live/dead/empty/exit-1/error branch is driven here with canned output and no
// tmux server. Fake/idiom helpers (fakeRunTmuxCmd, exitCode1Err, testOpts)
// live in tmux_runner_seam_test.go.

// seamResult is one canned (stdout, err) reply for a tmux subcommand.
type seamResult struct {
	out []byte
	err error
}

// livenessSeam installs a runTmuxCmd fake that dispatches on the tmux
// subcommand (has-session / list-panes / list-clients) and records each call,
// so multi-step reads — a has-session pre-check followed by the list — can be
// driven per step without a server. It also pins the command shape: the
// list-panes and list-clients invocations must carry the exact -F format the
// production code relies on.
func livenessSeam(t *testing.T, responses map[string]seamResult) *[]string {
	t.Helper()
	var calls []string
	orig := runTmuxCmd
	runTmuxCmd = func(cmd *exec.Cmd) ([]byte, error) {
		sub := ""
		for _, arg := range cmd.Args {
			switch arg {
			case "has-session", "list-panes", "list-clients":
				sub = arg
			}
		}
		calls = append(calls, sub)
		switch sub {
		case "list-panes":
			if !argsContain(cmd.Args, "#{pane_dead}") {
				t.Errorf("list-panes must request #{pane_dead}, got args %v", cmd.Args)
			}
		case "list-clients":
			if !argsContain(cmd.Args, "#{client_name}") {
				t.Errorf("list-clients must request #{client_name}, got args %v", cmd.Args)
			}
		}
		res, ok := responses[sub]
		if !ok {
			t.Errorf("unexpected tmux subcommand %q (args %v)", sub, cmd.Args)
			return nil, errors.New("unexpected tmux subcommand in test seam")
		}
		return res.out, res.err
	}
	t.Cleanup(func() { runTmuxCmd = orig })
	return &calls
}

func argsContain(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

// failOnAnyTmuxCall installs a runTmuxCmd fake that fails the test if the
// production code shells out at all — for inputs that must short-circuit.
func failOnAnyTmuxCall(t *testing.T) {
	t.Helper()
	orig := runTmuxCmd
	runTmuxCmd = func(cmd *exec.Cmd) ([]byte, error) {
		t.Errorf("expected no tmux invocation, got args %v", cmd.Args)
		return nil, errors.New("unexpected tmux invocation")
	}
	t.Cleanup(func() { runTmuxCmd = orig })
}

// ---------------------------------------------------------------------------
// hasLivePane: any pane_dead=="0" -> live; all "1" -> dead; exit 1 (session or
// server gone) -> not live, no error; other error -> propagated.
// hasLivePane never calls EnsureAvailable, so these run without tmux on PATH.
// ---------------------------------------------------------------------------

func TestHasLivePane_AllPanesLive(t *testing.T) {
	livenessSeam(t, map[string]seamResult{
		"has-session": {},
		"list-panes":  {out: []byte("0\n0\n")},
	})
	live, err := hasLivePane("amux-live", testOpts())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !live {
		t.Fatalf("all panes pane_dead=0 must classify as live")
	}
}

func TestHasLivePane_OneLivePaneAmongDeadIsLive(t *testing.T) {
	livenessSeam(t, map[string]seamResult{
		"has-session": {},
		"list-panes":  {out: []byte("1\n0\n1\n")},
	})
	live, err := hasLivePane("amux-mixed", testOpts())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !live {
		t.Fatalf("a single pane_dead=0 among dead panes must classify as live")
	}
}

func TestHasLivePane_AllPanesDeadIsNotLive(t *testing.T) {
	livenessSeam(t, map[string]seamResult{
		"has-session": {},
		"list-panes":  {out: []byte("1\n1\n")},
	})
	live, err := hasLivePane("amux-dead", testOpts())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if live {
		t.Fatalf("pane_dead=1 for every pane must classify as dead, not live")
	}
}

func TestHasLivePane_SessionGoneExitCode1IsNotLiveNotError(t *testing.T) {
	calls := livenessSeam(t, map[string]seamResult{
		"has-session": {err: exitCode1Err(t)},
	})
	live, err := hasLivePane("amux-gone", testOpts())
	if err != nil {
		t.Fatalf("exit 1 on has-session means session gone, not an error; got %v", err)
	}
	if live {
		t.Fatalf("a missing session must not classify as live")
	}
	for _, call := range *calls {
		if call == "list-panes" {
			t.Fatalf("list-panes must not run once has-session reports the session gone; calls %v", *calls)
		}
	}
}

func TestHasLivePane_ListPanesExitCode1IsNotLiveNotError(t *testing.T) {
	livenessSeam(t, map[string]seamResult{
		"has-session": {},
		"list-panes":  {err: exitCode1Err(t)},
	})
	live, err := hasLivePane("amux-racy", testOpts())
	if err != nil {
		t.Fatalf("exit 1 on list-panes (session vanished mid-check) must be empty, not an error; got %v", err)
	}
	if live {
		t.Fatalf("exit 1 must not classify as live")
	}
}

func TestHasLivePane_HasSessionOtherErrorPropagates(t *testing.T) {
	want := errors.New("lost server")
	livenessSeam(t, map[string]seamResult{
		"has-session": {err: want},
	})
	if _, err := hasLivePane("amux-x", testOpts()); !errors.Is(err, want) {
		t.Fatalf("non-exit-1 has-session error must propagate, got %v", err)
	}
}

func TestHasLivePane_ListPanesOtherErrorPropagates(t *testing.T) {
	want := errors.New("connection refused")
	livenessSeam(t, map[string]seamResult{
		"has-session": {},
		"list-panes":  {err: want},
	})
	if _, err := hasLivePane("amux-x", testOpts()); !errors.Is(err, want) {
		t.Fatalf("non-exit-1 list-panes error must propagate, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// SessionStateFor: the exported wrapper GC consumes. It calls EnsureAvailable
// (a PATH lookup only — the seam intercepts every exec), so these tests keep
// the file's skipIfNoTmux convention; no tmux server is ever contacted.
// ---------------------------------------------------------------------------

func TestSessionStateFor_EmptyNameIsZeroStateWithoutExec(t *testing.T) {
	failOnAnyTmuxCall(t)
	state, err := SessionStateFor("", testOpts())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if state.Exists || state.HasLivePane {
		t.Fatalf("empty session name must yield the zero state, got %+v", state)
	}
}

func TestSessionStateFor_MissingSession(t *testing.T) {
	skipIfNoTmux(t)
	livenessSeam(t, map[string]seamResult{
		"has-session": {err: exitCode1Err(t)},
	})
	state, err := SessionStateFor("amux-gone", testOpts())
	if err != nil {
		t.Fatalf("a missing session is a normal answer, not an error; got %v", err)
	}
	if state.Exists || state.HasLivePane {
		t.Fatalf("missing session must report Exists=false, got %+v", state)
	}
}

func TestSessionStateFor_LivePane(t *testing.T) {
	skipIfNoTmux(t)
	livenessSeam(t, map[string]seamResult{
		"has-session": {},
		"list-panes":  {out: []byte("0\n")},
	})
	state, err := SessionStateFor("amux-live", testOpts())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !state.Exists || !state.HasLivePane {
		t.Fatalf("live pane must yield {Exists:true HasLivePane:true}, got %+v", state)
	}
}

func TestSessionStateFor_DeadPanesOnly(t *testing.T) {
	skipIfNoTmux(t)
	livenessSeam(t, map[string]seamResult{
		"has-session": {},
		"list-panes":  {out: []byte("1\n")},
	})
	state, err := SessionStateFor("amux-dead", testOpts())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !state.Exists || state.HasLivePane {
		t.Fatalf("dead-pane session must yield {Exists:true HasLivePane:false}, got %+v", state)
	}
}

func TestSessionStateFor_OtherErrorPropagates(t *testing.T) {
	skipIfNoTmux(t)
	want := errors.New("lost server")
	livenessSeam(t, map[string]seamResult{
		"has-session": {},
		"list-panes":  {err: want},
	})
	if _, err := SessionStateFor("amux-x", testOpts()); !errors.Is(err, want) {
		t.Fatalf("non-exit-1 error must propagate, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// SessionClientCount / SessionHasClients: attached-client guard. Neither calls
// EnsureAvailable (the has-session pre-check and list-clients both go through
// the runTmuxCmd seam), so these run without tmux on PATH.
// ---------------------------------------------------------------------------

func TestSessionClientCount_EmptyNameIsZeroWithoutExec(t *testing.T) {
	failOnAnyTmuxCall(t)
	count, err := SessionClientCount("", testOpts())
	if err != nil || count != 0 {
		t.Fatalf("empty session name must be (0, nil), got (%d, %v)", count, err)
	}
}

func TestSessionClientCount_SessionGoneIsZeroNotError(t *testing.T) {
	calls := livenessSeam(t, map[string]seamResult{
		"has-session": {err: exitCode1Err(t)},
	})
	count, err := SessionClientCount("amux-gone", testOpts())
	if err != nil {
		t.Fatalf("exit 1 on has-session means session gone, not an error; got %v", err)
	}
	if count != 0 {
		t.Fatalf("missing session must count 0 clients, got %d", count)
	}
	for _, call := range *calls {
		if call == "list-clients" {
			t.Fatalf("list-clients must not run for a missing session; calls %v", *calls)
		}
	}
}

func TestSessionClientCount_CountsClientLines(t *testing.T) {
	livenessSeam(t, map[string]seamResult{
		"has-session":  {},
		"list-clients": {out: []byte("client-0\nclient-1\nclient-2\n")},
	})
	count, err := SessionClientCount("amux-attached", testOpts())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if count != 3 {
		t.Fatalf("three client lines must count 3, got %d", count)
	}
}

func TestSessionClientCount_ListClientsExitCode1IsZeroNotError(t *testing.T) {
	livenessSeam(t, map[string]seamResult{
		"has-session":  {},
		"list-clients": {err: exitCode1Err(t)},
	})
	count, err := SessionClientCount("amux-racy", testOpts())
	if err != nil {
		t.Fatalf("exit 1 on list-clients must be empty, not an error; got %v", err)
	}
	if count != 0 {
		t.Fatalf("exit 1 must count 0 clients, got %d", count)
	}
}

func TestSessionClientCount_OtherErrorPropagates(t *testing.T) {
	want := errors.New("no server running on /tmp/x")
	livenessSeam(t, map[string]seamResult{
		"has-session":  {},
		"list-clients": {err: want},
	})
	if _, err := SessionClientCount("amux-x", testOpts()); !errors.Is(err, want) {
		t.Fatalf("non-exit-1 error must propagate, got %v", err)
	}
}

func TestSessionHasClients_TrueWhenAttached(t *testing.T) {
	livenessSeam(t, map[string]seamResult{
		"has-session":  {},
		"list-clients": {out: []byte("client-0\n")},
	})
	has, err := SessionHasClients("amux-attached", testOpts())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !has {
		t.Fatalf("an attached client must report HasClients=true")
	}
}

func TestSessionHasClients_FalseWhenNoClients(t *testing.T) {
	livenessSeam(t, map[string]seamResult{
		"has-session":  {},
		"list-clients": {out: []byte("")},
	})
	has, err := SessionHasClients("amux-detached", testOpts())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if has {
		t.Fatalf("no client lines must report HasClients=false")
	}
}

func TestSessionHasClients_ErrorPropagates(t *testing.T) {
	want := errors.New("lost server")
	livenessSeam(t, map[string]seamResult{
		"has-session": {err: want},
	})
	has, err := SessionHasClients("amux-x", testOpts())
	if !errors.Is(err, want) {
		t.Fatalf("non-exit-1 error must propagate, got %v", err)
	}
	if has {
		t.Fatalf("error path must not report clients attached")
	}
}
