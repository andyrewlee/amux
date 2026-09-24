package tmux

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// Parse-branch coverage for the detached run-session readers
// (detached_session.go): RunSessionStatus, RunSessionTail, FindRunSessions.
// These guard the run-script lifecycle (status/stop/output) and previously
// needed a live tmux server to reach their subtle branches — empty-output
// success, display-message prefix-matching a different session, dead-pane
// exit-code parsing, pane resolution order, and instance-namespace filtering.
// The runTmuxCmd/runTmuxCmdCombined var-seams (tmux_runner.go) drive every
// branch here without a server; skipIfNoTmux covers only the exported
// functions' EnsureAvailable PATH gate, matching the seam-test convention.

// runTailSeam installs a runTmuxCmd fake that returns canned output per
// tmux subcommand (list-panes rows and the capture-pane body) and records
// the pane target capture-pane ran against.
func runTailSeam(t *testing.T, paneRows []byte, paneErr error, captureOut []byte, captureErr error) *string {
	t.Helper()
	var captureTarget string
	orig := runTmuxCmd
	runTmuxCmd = func(cmd *exec.Cmd) ([]byte, error) {
		sub := ""
		for i, arg := range cmd.Args {
			switch arg {
			case "list-panes", "capture-pane":
				sub = arg
			}
			if arg == "-t" && i+1 < len(cmd.Args) && sub == "capture-pane" {
				captureTarget = cmd.Args[i+1]
			}
		}
		if sub == "list-panes" {
			return paneRows, paneErr
		}
		return captureOut, captureErr
	}
	t.Cleanup(func() { runTmuxCmd = orig })
	return &captureTarget
}

// ---------------------------------------------------------------------------
// RunSessionStatus
// ---------------------------------------------------------------------------

func TestRunSessionStatus_EmptyNameShortCircuits(t *testing.T) {
	exists, alive, code, err := RunSessionStatus("", testOpts())
	if exists || alive || code != -1 || err != nil {
		t.Fatalf("empty name = (%v, %v, %d, %v), want (false, false, -1, nil)", exists, alive, code, err)
	}
}

func TestRunSessionStatus_EmptyOutputMeansMissing(t *testing.T) {
	skipIfNoTmux(t)
	fakeRunTmuxCmdCombined(t, nil, nil)
	exists, _, _, err := RunSessionStatus("gone", testOpts())
	if err != nil || exists {
		t.Fatalf("empty output = (%v, %v), want exists=false", exists, err)
	}
}

func TestRunSessionStatus_Exit1MeansMissing(t *testing.T) {
	skipIfNoTmux(t)
	fakeRunTmuxCmdCombined(t, []byte("can't find session: gone"), exitCode1Err(t))
	exists, _, _, err := RunSessionStatus("gone", testOpts())
	if err != nil || exists {
		t.Fatalf("exit 1 not-found stderr = (%v, %v), want exists=false", exists, err)
	}
}

func TestRunSessionStatus_Exit1OtherStderrStillMissing(t *testing.T) {
	skipIfNoTmux(t)
	// isExitCode1 alone maps to missing — tmux uses 1 for all lookup misses.
	fakeRunTmuxCmdCombined(t, []byte("no current target"), exitCode1Err(t))
	exists, _, _, err := RunSessionStatus("gone", testOpts())
	if err != nil || exists {
		t.Fatalf("exit 1 = (%v, %v), want exists=false", exists, err)
	}
}

func TestRunSessionStatus_OtherErrorPropagates(t *testing.T) {
	skipIfNoTmux(t)
	want := errors.New("connection refused")
	fakeRunTmuxCmdCombined(t, nil, want)
	_, _, _, err := RunSessionStatus("s", testOpts())
	if !errors.Is(err, want) {
		t.Fatalf("non-exit-1 error must propagate, got %v", err)
	}
}

func TestRunSessionStatus_PrefixMatchIsNotExistence(t *testing.T) {
	skipIfNoTmux(t)
	// display-message -t prefix-matches: asking for "run" can resolve "runx".
	// The echoed session_name must equal the request.
	fakeRunTmuxCmdCombined(t, []byte("runx\t0\t\n"), nil)
	exists, _, _, err := RunSessionStatus("run", testOpts())
	if err != nil || exists {
		t.Fatalf("prefix-matched echo = (%v, %v), want exists=false", exists, err)
	}
}

func TestRunSessionStatus_Parses(t *testing.T) {
	skipIfNoTmux(t)
	tests := []struct {
		name     string
		output   string
		wantEx   bool
		wantLive bool
		wantCode int
	}{
		{"alive", "s\t0\t\n", true, true, -1},
		{"dead status 0", "s\t1\t0\n", true, false, 0},
		{"dead status 1", "s\t1\t1\n", true, false, 1},
		{"dead status empty", "s\t1\t\n", true, false, -1},
		{"dead status garbage", "s\t1\tabc\n", true, false, -1},
		{"two fields", "s\t0\n", false, false, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeRunTmuxCmdCombined(t, []byte(tt.output), nil)
			ex, live, code, err := RunSessionStatus("s", testOpts())
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if ex != tt.wantEx || live != tt.wantLive || code != tt.wantCode {
				t.Fatalf("output %q = (%v, %v, %d), want (%v, %v, %d)",
					tt.output, ex, live, code, tt.wantEx, tt.wantLive, tt.wantCode)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// RunSessionTail
// ---------------------------------------------------------------------------

func TestRunSessionTail_GuardClauses(t *testing.T) {
	if out, ok := RunSessionTail("", 10, testOpts()); ok || out != "" {
		t.Fatalf("empty name = (%q, %v), want (\"\", false)", out, ok)
	}
	if out, ok := RunSessionTail("s", 0, testOpts()); ok || out != "" {
		t.Fatalf("lines=0 = (%q, %v), want (\"\", false)", out, ok)
	}
}

func TestRunSessionTail_NoPanes(t *testing.T) {
	skipIfNoTmux(t)
	runTailSeam(t, nil, exitCode1Err(t), nil, nil)
	if out, ok := RunSessionTail("s", 10, testOpts()); ok || out != "" {
		t.Fatalf("list-panes exit 1 = (%q, %v), want (\"\", false)", out, ok)
	}
}

func TestRunSessionTail_SkipsRowsWithoutPaneID(t *testing.T) {
	skipIfNoTmux(t)
	rows := []byte("not-a-pane\t1\n\t0\nmalformed-only-one-field\n")
	target := runTailSeam(t, rows, nil, []byte("x"), nil)
	if out, ok := RunSessionTail("s", 10, testOpts()); ok || out != "" {
		t.Fatalf("rows lacking %%-ids = (%q, %v), want (\"\", false)", out, ok)
	}
	if *target != "" {
		t.Fatalf("capture-pane must not run when no pane resolves, ran against %q", *target)
	}
}

func TestRunSessionTail_PrefersActivePane(t *testing.T) {
	skipIfNoTmux(t)
	rows := []byte("%7\t0\n%9\t1\n%8\t0\n")
	target := runTailSeam(t, rows, nil, []byte("body\n"), nil)
	out, ok := RunSessionTail("s", 10, testOpts())
	if !ok || out != "body" {
		t.Fatalf("= (%q, %v), want (\"body\", true)", out, ok)
	}
	if *target != "%9" {
		t.Fatalf("active pane preferred, captured %q", *target)
	}
}

func TestRunSessionTail_FallsBackToFirstPane(t *testing.T) {
	skipIfNoTmux(t)
	rows := []byte("%7\t0\n%8\t0\n")
	target := runTailSeam(t, rows, nil, []byte("body"), nil)
	if _, ok := RunSessionTail("s", 10, testOpts()); !ok {
		t.Fatalf("want ok")
	}
	if *target != "%7" {
		t.Fatalf("no active pane -> first valid pane, captured %q", *target)
	}
}

func TestRunSessionTail_CaptureErrorMeansFalse(t *testing.T) {
	skipIfNoTmux(t)
	runTailSeam(t, []byte("%7\t1\n"), nil, nil, errors.New("pane vanished"))
	if out, ok := RunSessionTail("s", 10, testOpts()); ok || out != "" {
		t.Fatalf("capture error = (%q, %v), want (\"\", false)", out, ok)
	}
}

func TestRunSessionTail_TrimsTrailingWhitespace(t *testing.T) {
	skipIfNoTmux(t)
	runTailSeam(t, []byte("%7\t1\n"), nil, []byte("line1\nline2 \t\n\n"), nil)
	out, ok := RunSessionTail("s", 10, testOpts())
	if !ok || out != "line1\nline2" {
		t.Fatalf("trailing whitespace = (%q, %v), want (\"line1\\nline2\", true)", out, ok)
	}
}

// ---------------------------------------------------------------------------
// FindRunSessions — the rows arrive as
// name|@amux|@amux_instance|@amux_type|@amux_workspace (keys sorted by
// listSessionsWithTags).
// ---------------------------------------------------------------------------

func findRunSeam(t *testing.T, lines []byte, err error) {
	t.Helper()
	fakeRunTmuxCmd(t, lines, err)
}

func runRow(name, instance string) string {
	return name + "|1|" + instance + "|run|ws-1"
}

func TestFindRunSessions_NamespaceFilter(t *testing.T) {
	skipIfNoTmux(t)
	findRunSeam(t, []byte(strings.Join([]string{
		runRow("amux-ws1-run-a", "abc.launch1"),
		runRow("amux-ws1-run-b", "abc.launch2"), // same ns, different launch
		runRow("amux-ws1-run-c", "xyz.launch1"), // foreign ns
		runRow("amux-ws1-run-d", ""),            // untagged legacy -> match
	}, "\n")+"\n"), nil)

	names, err := FindRunSessions("ws-1", "abc.mine", testOpts())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	got := strings.Join(names, ",")
	want := "amux-ws1-run-a,amux-ws1-run-b,amux-ws1-run-d"
	if got != want {
		t.Fatalf("names = %q, want %q", got, want)
	}
}

func TestFindRunSessions_EmptyInstanceIDMatchesAll(t *testing.T) {
	skipIfNoTmux(t)
	findRunSeam(t, []byte(runRow("amux-ws1-run-a", "xyz.other")+"\n"), nil)
	names, err := FindRunSessions("ws-1", "", testOpts())
	if err != nil || len(names) != 1 {
		t.Fatalf("empty instanceID must not filter, got %v err=%v", names, err)
	}
}

func TestFindRunSessions_TagMatchFilter(t *testing.T) {
	skipIfNoTmux(t)
	// Row with a different workspace tag: matchesTags drops it before the
	// instance check ever runs.
	findRunSeam(t, []byte("amux-other-run|1|abc.x|run|ws-2\n"), nil)
	names, err := FindRunSessions("ws-1", "abc.y", testOpts())
	if err != nil || len(names) != 0 {
		t.Fatalf("workspace mismatch must filter, got %v err=%v", names, err)
	}
}

func TestFindRunSessions_TrimsAndSkipsEmptyNames(t *testing.T) {
	skipIfNoTmux(t)
	findRunSeam(t, []byte("  amux-ws1-run-a  |1|abc.x|run|ws-1\n|1|abc.x|run|ws-1\n"), nil)
	names, err := FindRunSessions("ws-1", "abc.y", testOpts())
	if err != nil || len(names) != 1 || names[0] != "amux-ws1-run-a" {
		t.Fatalf("names = %#v err=%v, want [amux-ws1-run-a]", names, err)
	}
}
