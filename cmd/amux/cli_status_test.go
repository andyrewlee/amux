package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/agentstatus"
)

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
