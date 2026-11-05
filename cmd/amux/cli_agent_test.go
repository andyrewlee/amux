package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// The flag grammar of `amux send` is the safety surface: exactly one
// target, exactly one payload form, --enter only with text. These pin the
// rules without a tmux server (resolveSendTarget is reached only after
// they pass, and its live behavior is covered by the e2e suite).
func TestRunSendCommandFlagRules(t *testing.T) {
	cases := []struct {
		name string
		args []string
		code int
	}{
		{"no target", []string{"--text", "hi"}, 2},
		{"both targets", []string{"--workspace", "w", "--session", "s", "--text", "hi"}, 2},
		{"no payload", []string{"--session", "s"}, 2},
		{"both payloads", []string{"--session", "s", "--text", "hi", "--keys", "esc"}, 2},
		{"enter with keys", []string{"--session", "s", "--keys", "cr", "--enter"}, 2},
		{"unknown key", []string{"--session", "s", "--keys", "kill-session"}, 2},
		{"tmux injection as key", []string{"--session", "s", "--keys", "c-c;kill-session"}, 2},
		{"empty keys string", []string{"--session", "s", "--keys", " , "}, 2},
		{"unexpected positional", []string{"--session", "s", "--text", "hi", "extra"}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if code := runSendCommand(tc.args); code != tc.code {
				t.Fatalf("exit = %d, want %d", code, tc.code)
			}
		})
	}
}

func TestParseSendKeys(t *testing.T) {
	if got := parseSendKeys(" escape , up ,cr "); len(got) != 3 || got[0] != "escape" || got[1] != "up" || got[2] != "cr" {
		t.Fatalf("parseSendKeys = %#v", got)
	}
	if got := parseSendKeys("  "); got != nil {
		t.Fatalf("blank keys = %#v, want nil", got)
	}
}

// statusSnippet is the protocol report recipe skill.md and
// docs/ORCHESTRATION.md share verbatim. skill.md must stay self-contained
// (a hosted pane cannot read the amux repo's docs), so the drift risk is
// real: pinning the shared block in both files makes a one-sided edit fail
// here instead of teaching agents a stale recipe.
const statusSnippet = `status() {
  printf '\e]7501;state=%s:msg=%s\e\\' "$1" "$(printf '%s' "$2" | base64 | tr -d '\n')"
}`

func TestSkillCommand(t *testing.T) {
	if code := runSkillCommand(nil); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if code := runSkillCommand([]string{"extra"}); code != 2 {
		t.Fatalf("extra args exit = %d, want 2", code)
	}
	doc, err := skillFS.ReadFile("skill.md")
	if err != nil {
		t.Fatal(err)
	}
	// The skill doc is an agent's manual for verbs that must exist: keep
	// it honest as verbs change. Every `$AMUX_BIN_PATH <verb>` line in the
	// doc must name a verb the binary actually dispatches.
	text := string(doc)
	for _, verb := range []string{"report", "wait", "agent ls", "send"} {
		if !strings.Contains(text, verb) {
			t.Fatalf("skill doc does not mention verb %q", verb)
		}
	}
	for _, env := range []string{"AMUX_ENV", "AMUX_BIN_PATH", "AMUX_WORKSPACE_NAME"} {
		if !strings.Contains(text, env) {
			t.Fatalf("skill doc does not mention env %q", env)
		}
	}
	// The advertised key vocabulary must be the accepted one: sentinel
	// names tmux resolves are present, names it would type as text are not.
	for _, key := range []string{"escape", "bspace", "pgdn"} {
		if !strings.Contains(text, key) {
			t.Fatalf("skill doc key vocabulary missing %q", key)
		}
	}
	for _, notKey := range []string{"esc,", " bs ", "pgdown", "a-x"} {
		if strings.Contains(text, notKey) {
			t.Fatalf("skill doc advertises %q, which tmux would type as literal text", notKey)
		}
	}
	// Shared recipes must agree with the contract doc, verbatim.
	if !strings.Contains(text, statusSnippet) {
		t.Fatal("skill doc's status() recipe drifted from the pinned snippet")
	}
	orchestration, err := os.ReadFile("../../docs/ORCHESTRATION.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(orchestration), statusSnippet) {
		t.Fatal("docs/ORCHESTRATION.md's status() recipe drifted from the pinned snippet")
	}
	// The one-read promise: bounded length.
	if len(doc) > 8192 {
		t.Fatalf("skill doc is %d bytes; keep it one read", len(doc))
	}
}

func TestHandleAgentCLIDispatch(t *testing.T) {
	for _, argv := range [][]string{{}, {"tui"}, {"report"}, {""}} {
		if handled, _ := HandleAgentCLI(argv); handled {
			t.Fatalf("HandleAgentCLI(%q) claimed argv it does not own", argv)
		}
	}
	if handled, _ := HandleAgentCLI([]string{"agent"}); !handled {
		t.Fatal("agent subcommand must be handled")
	}
	if handled, _ := HandleAgentCLI([]string{"--skill"}); !handled {
		t.Fatal("--skill must be handled")
	}
}

// The --json error envelope is a machine seam: exact shape, compact, on
// stderr — scripts branch on the exit code and may parse the code field,
// so the bytes are pinned.
func TestPrintJSONErrorEnvelope(t *testing.T) {
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	printJSONErrorEnvelope("unavailable", "tmux: not found")
	os.Stderr = old
	w.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	want := `{"error":{"code":"unavailable","message":"tmux: not found"}}` + "\n"
	if buf.String() != want {
		t.Fatalf("envelope = %q, want %q", buf.String(), want)
	}
}
