package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/agentstatus"
	"github.com/andyrewlee/amux/internal/tmux"
)

// The wait watcher's pure decision paths — maps in, outcome out. The
// baseline rule (sessions dead when observation started never fire), the
// determinism of the pick, the process-exit lifetime of reports, and the
// dead-set fold pin here; the live-scan halves are covered by the e2e
// suites.
func TestWaitWatcherExitEventBaselineAndPick(t *testing.T) {
	w := &waitWatcher{
		want:     map[string]bool{"any": true},
		baseline: map[string]bool{"stale-run": true},
	}
	u := map[string]bool{"stale-run": true, "a-fresh-run": true, "b-fresh-run": true, "live-run": true}
	dead := map[string]bool{"stale-run": true, "a-fresh-run": true, "b-fresh-run": true}
	reports := map[string]agentstatus.Report{
		"a-fresh-run": {State: agentstatus.StateDone},
		"b-fresh-run": {State: agentstatus.StateDone},
		"stale-run":   {State: agentstatus.StateDone},
	}
	m, ok := w.findExitEvent(u, reports, dead)
	if !ok || m.Session != "a-fresh-run" {
		t.Fatalf("findExitEvent = (%#v, %v), want a-fresh-run — the deterministic first non-baseline exit", m, ok)
	}
	if m.Report.State != agentstatus.StateDone {
		t.Fatalf("a dead pane's surviving report must ride the event: %#v", m)
	}

	// Sessions outside the universe (an unreported foreign pane) never
	// fire, dead or not.
	u2 := map[string]bool{"a-fresh-run": true}
	if m, ok := w.findExitEvent(u2, map[string]agentstatus.Report{}, dead); !ok || m.Session != "a-fresh-run" {
		t.Fatalf("exit events must stay inside the universe: (%#v, %v)", m, ok)
	}

	// A transient report fetched before the death (its snapshot still
	// says the pane was alive) must not ride the exit line: the dead set
	// is authoritative for the pane's lifetime.
	transient := map[string]agentstatus.Report{"a-fresh-run": {State: agentstatus.StateWorking}}
	if m, _ := w.findExitEvent(u, transient, dead); m.Session == "a-fresh-run" && m.Report.State != "" {
		t.Fatalf("a transient report must not survive an observed death: %#v", m)
	}
}

// deadSessions folds AllSessionStates into the dead set: a session is
// dead when it exists and no pane in it is live.
func TestDeadSessionsFold(t *testing.T) {
	states := map[string]tmux.SessionState{
		"dead-run":   {Exists: true, HasLivePane: false},
		"live-run":   {Exists: true, HasLivePane: true},
		"half-multi": {Exists: true, HasLivePane: true}, // any live pane → not dead
	}
	u := map[string]bool{"dead-run": true, "live-run": true, "half-multi": true, "gone": true}
	dead := deadSessions(u, states)
	if !dead["dead-run"] || dead["live-run"] || dead["half-multi"] || dead["gone"] {
		t.Fatalf("deadSessions = %#v", dead)
	}
}

func TestWaitWatcherSessionUniverseIsExact(t *testing.T) {
	w := &waitWatcher{session: "only-this"}
	u := w.universe(map[string]agentstatus.Report{})
	if len(u) != 1 || !u["only-this"] {
		t.Fatalf("--session universe = %#v, want exactly the named session", u)
	}
}

// The state-matching half keeps the protocol's process-exit lifetime: a
// transient report on a dead pane is stale, never a match; a surviving
// one matches.
func TestWaitWatcherFindMatchLifetime(t *testing.T) {
	w := &waitWatcher{want: map[string]bool{agentstatus.StateDone: true}, session: "the-tab"}
	reports := map[string]agentstatus.Report{
		"the-tab": {State: agentstatus.StateWorking, PaneDead: true},
		"other":   {State: agentstatus.StateDone},
	}
	if _, err := w.findMatch(reports); err == nil {
		t.Fatal("a transient report on a dead pane must never match")
	}
	reports["the-tab"] = agentstatus.Report{State: agentstatus.StateDone, PaneDead: true}
	m, err := w.findMatch(reports)
	if err != nil || m.Session != "the-tab" {
		t.Fatalf("findMatch = (%#v, %v), want the-tab's surviving done report", m, err)
	}
}

func TestPrintExitMatch(t *testing.T) {
	stdout := captureStdout(t)
	printExitMatch(waitMatch{
		Session: "amux-ws1-run-1",
		Report:  agentstatus.Report{State: agentstatus.StateDone, App: "run"},
	}, 7)
	out := stdout()
	for _, want := range []string{"session=amux-ws1-run-1", "state=done", "app=run", "exit=7"} {
		if !strings.Contains(out, want) {
			t.Fatalf("exit print missing %q: %q", want, out)
		}
	}

	stdout = captureStdout(t)
	printExitMatch(waitMatch{Session: "amux-ws1-run-1"}, -1)
	out = stdout()
	if !strings.Contains(out, "exit=unknown") || strings.Contains(out, "state=") {
		t.Fatalf("statusless exit print = %q, want exit=unknown and no report fields", out)
	}
}

func TestParseWaitStates(t *testing.T) {
	got, err := parseWaitStates("blocked, error ,done")
	if err != nil {
		t.Fatalf("valid list rejected: %v", err)
	}
	for _, s := range []string{"blocked", "error", "done"} {
		if !got[s] {
			t.Fatalf("state %q missing from %v", s, got)
		}
	}
	if got, err = parseWaitStates("any"); err != nil || !got["any"] {
		t.Fatalf("'any' wildcard rejected: %v", err)
	}
	// Empty input is a usage error, not a silent wait-forever.
	if _, err = parseWaitStates(" , "); err == nil {
		t.Fatal("empty state list must be rejected")
	}
	// Unknown labels are refused up front: a typo would otherwise produce
	// a filter nothing can ever match.
	for _, bad := range []string{"don", "Done", "clear", "any2"} {
		if _, err = parseWaitStates(bad); err == nil {
			t.Fatalf("unknown state %q must be rejected", bad)
		}
	}
}

// TestRunReportCommandValidation pins the round-trip guard: every report
// the CLI prints must re-parse to exactly what was asked for. Values a
// consumer would silently drop or treat as absent (out-of-grammar app or
// kind, out-of-range progress, control runes) are refused, and nothing can
// terminate the OSC early — control bytes in a flag fail the value grammar
// on re-parse, so an injected second sequence can never be emitted.
func TestRunReportCommandValidation(t *testing.T) {
	injection := "a\x07\x1b]7501;state=clear"
	cases := []struct {
		name string
		args []string
		code int
	}{
		{"valid blocked", []string{"--state", "blocked", "--kind", "question", "--message", "hi"}, 0},
		{"clear", []string{"--clear"}, 0},
		{"unknown state", []string{"--state", "vibrating"}, 2},
		{"unknown kind", []string{"--state", "blocked", "--kind", "musing"}, 2},
		{"progress out of range", []string{"--state", "working", "--progress", "101"}, 2},
		{"progress with done", []string{"--state", "done", "--progress", "10"}, 2},
		{"message with newline", []string{"--state", "done", "--message", "line\nbreak"}, 2},
		{"app with control bytes", []string{"--state", "done", "--app", injection, "--message", "hi"}, 2},
		{"missing state", []string{"--app", "x"}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout := captureStdout(t)
			code := runReportCommand(tc.args)
			out := stdout()
			if code != tc.code {
				t.Fatalf("exit = %d, want %d", code, tc.code)
			}
			if tc.code == 2 && out != "" {
				t.Fatalf("rejected report printed %q to stdout", out)
			}
			if tc.code == 0 && !strings.Contains(out, "\x1b]7501;") {
				t.Fatalf("accepted report printed no OSC: %q", out)
			}
			if tc.code == 0 && strings.Count(out, "\x1b]7501;") != 1 {
				t.Fatalf("accepted report printed %d OSC sequences, want exactly 1: %q",
					strings.Count(out, "\x1b]7501;"), out)
			}
		})
	}
}

// TestRunReportCommandRoundTripIsExact checks the accepted path emits a
// sequence that re-parses to the identical report.
func TestRunReportCommandRoundTripIsExact(t *testing.T) {
	stdout := captureStdout(t)
	if code := runReportCommand([]string{"--state", "blocked", "--kind", "auth", "--app", "cli-test", "--message", "needs a token"}); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	out := stdout()
	i := strings.Index(out, "7501;")
	if i < 0 {
		t.Fatalf("malformed OSC output: %q", out)
	}
	body := strings.TrimRight(out[i+len("7501;"):], "\x07\r\n")
	r, ok := agentstatus.ParsePayload(body)
	if !ok {
		t.Fatalf("emitted sequence does not parse: %q", body)
	}
	want := agentstatus.Report{State: "blocked", Kind: "auth", App: "cli-test", Message: "needs a token", Progress: -1}
	if r != want {
		t.Fatalf("round trip = %#v, want %#v", r, want)
	}
}

// captureStdout redirects os.Stdout for the duration of one call and
// returns a function to collect what was printed.
func captureStdout(t *testing.T) func() string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	return func() string {
		os.Stdout = old
		w.Close()
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, r); err != nil {
			t.Errorf("read captured stdout: %v", err)
		}
		return buf.String()
	}
}
