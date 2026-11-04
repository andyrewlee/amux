package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The real-binary end of the Phase B agent-facing surface: `amux --skill`,
// `amux agent ls` (tagged sessions, reported-state precedence), and
// `amux send` (exact target resolution, literal text, CR submit) — all
// against a real isolated tmux server, driving the actual binary the way
// a hosted agent or supervising script would.

func TestAgentCLIVerbsRealBinary(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	bin, cleanupBin, err := buildAmuxBinary()
	if err != nil {
		t.Fatalf("build amux binary: %v", err)
	}
	defer cleanupBin()

	server := fmt.Sprintf("amux-e2e-agent-%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", server, "kill-server").Run() })
	env := append(os.Environ(), "AMUX_TMUX_SERVER="+server)
	amux := func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	tmuxCmd := func(args ...string) string {
		full := append([]string{"tmux", "-L", server, "-f", "/dev/null"}, args...)
		out, _ := exec.Command(full[0], full[1:]...).CombinedOutput()
		return string(out)
	}

	// --skill: one read, no TTY, mentions the contract it documents.
	skill, err := amux("--skill")
	if err != nil {
		t.Fatalf("amux --skill: %v\n%s", err, skill)
	}
	for _, want := range []string{"AMUX_ENV", "AMUX_BIN_PATH", "agent ls", "report --state", "wait --state"} {
		if !strings.Contains(skill, want) {
			t.Fatalf("--skill output missing %q:\n%s", want, skill)
		}
	}

	// A hosted session: the tags are what makes a session amux-hosted.
	tmuxCmd("new-session", "-d", "-s", "amux-ws1-tab-aa1-1", "cat")
	tmuxCmd("set-option", "-t", "amux-ws1-tab-aa1-1", "@amux_workspace", "ws1")
	tmuxCmd("set-option", "-t", "amux-ws1-tab-aa1-1", "@amux_workspace_name", "my-feature")
	tmuxCmd("set-option", "-t", "amux-ws1-tab-aa1-1", "@amux_type", "agent")
	tmuxCmd("set-option", "-t", "amux-ws1-tab-aa1-1", "@amux_assistant", "claude")

	// agent ls: the session appears with scan-state unknown ("-" — no
	// report, no scan) and resolves by workspace name.
	lsOut, err := amux("agent", "ls")
	if err != nil {
		t.Fatalf("amux agent ls: %v\n%s", err, lsOut)
	}
	if !strings.Contains(lsOut, "my-feature") || !strings.Contains(lsOut, "agent") ||
		!strings.Contains(lsOut, "amux-ws1-tab-aa1-1") {
		t.Fatalf("agent ls missing the hosted session:\n%s", lsOut)
	}
	lsJSON, err := amux("agent", "ls", "--json")
	if err != nil || !strings.Contains(lsJSON, `"session":"amux-ws1-tab-aa1-1"`) {
		t.Fatalf("agent ls --json wrong:\n%s (%v)", lsJSON, err)
	}

	// A reported state outranks everything: set the pane option the tap
	// writes and ls shows it as reported.
	tmuxCmd("set-option", "-p", "-t", "amux-ws1-tab-aa1-1", "@amux_reported_state", "blocked")
	tmuxCmd("set-option", "-p", "-t", "amux-ws1-tab-aa1-1", "@amux_reported_kind", "question")
	lsOut, err = amux("agent", "ls")
	if err != nil || !strings.Contains(lsOut, "blocked") {
		t.Fatalf("agent ls must surface the reported state:\n%s (%v)", lsOut, err)
	}
	lsJSON, _ = amux("agent", "ls", "--json")
	if !strings.Contains(lsJSON, `"state_source":"reported"`) {
		t.Fatalf("agent ls --json must attribute reported state:\n%s", lsJSON)
	}

	// send --text --enter: literal text and a real CR into the cat pane.
	// The dash-leading case pins the argv terminator: tmux's client would
	// otherwise eat "-R" as a flag (it resets the pane terminal).
	if out, err := amux("send", "--session", "amux-ws1-tab-aa1-1", "--text", "ping from amux send", "--enter"); err != nil {
		t.Fatalf("amux send: %v\n%s", err, out)
	}
	waitForCapture(t, server, "amux-ws1-tab-aa1-1", "ping from amux send")
	if out, err := amux("send", "--session", "amux-ws1-tab-aa1-1", "--text", "-Rdash"); err != nil {
		t.Fatalf("amux send dash-leading: %v\n%s", err, out)
	}
	waitForCapture(t, server, "amux-ws1-tab-aa1-1", "-Rdash")

	// send --keys: named keys reach the pane without error. Only tmux key
	// names are valid — `esc` would be typed as literal text, so the
	// vocabulary must refuse it (exit 2) while `escape` is accepted.
	if out, err := amux("send", "--session", "amux-ws1-tab-aa1-1", "--keys", "escape,cr"); err != nil {
		t.Fatalf("amux send --keys: %v\n%s", err, out)
	}
	if out, err := amux("send", "--session", "amux-ws1-tab-aa1-1", "--keys", "esc"); err == nil {
		t.Fatalf("esc is not a tmux key name (tmux types it as text); send must refuse it:\n%s", out)
	}

	// A session whose pane process has exited must be refused when its
	// death is observable (a stored report annotated pane-dead):
	// send-keys would otherwise succeed against the dead pane with
	// nothing to read the input. remain-on-exit keeps the pane present
	// after its process exits on its own (kill-pane would destroy it).
	tmuxCmd("new-session", "-d", "-s", "amux-ws1-tab-dead9-1", "sh", "-c", "sleep 1")
	tmuxCmd("set-option", "-w", "-t", "amux-ws1-tab-dead9-1", "remain-on-exit", "on")
	tmuxCmd("set-option", "-t", "amux-ws1-tab-dead9-1", "@amux_workspace", "ws1")
	tmuxCmd("set-option", "-t", "amux-ws1-tab-dead9-1", "@amux_workspace_name", "my-feature")
	tmuxCmd("set-option", "-t", "amux-ws1-tab-dead9-1", "@amux_type", "agent")
	tmuxCmd("set-option", "-p", "-t", "amux-ws1-tab-dead9-1:", "@amux_reported_state", "blocked")
	waitForCond(t, func() bool {
		out, err := exec.Command("tmux", "-L", server, "-f", "/dev/null",
			"list-panes", "-t", "amux-ws1-tab-dead9-1", "-F", "#{pane_dead}").CombinedOutput()
		return err == nil && strings.TrimSpace(string(out)) == "1"
	}, 5*time.Second, "pane in amux-ws1-tab-dead9-1 never died")
	deadOut, deadErr := amux("send", "--session", "amux-ws1-tab-dead9-1", "--text", "hello?")
	if deadErr == nil || !strings.Contains(deadOut, "exited") {
		t.Fatalf("send to a dead pane must fail with 'exited', got: %v\n%s", deadErr, deadOut)
	}

	// Ambiguity is an error, never a guess: two hosted sessions in one
	// workspace, --workspace must refuse and list both.
	tmuxCmd("new-session", "-d", "-s", "amux-ws1-run-bb2-1", "sleep 300")
	tmuxCmd("set-option", "-t", "amux-ws1-run-bb2-1", "@amux_workspace", "ws1")
	tmuxCmd("set-option", "-t", "amux-ws1-run-bb2-1", "@amux_workspace_name", "my-feature")
	tmuxCmd("set-option", "-t", "amux-ws1-run-bb2-1", "@amux_type", "run")
	out, err := amux("send", "--workspace", "my-feature", "--text", "hi")
	if err == nil {
		t.Fatalf("ambiguous target must fail, got success:\n%s", out)
	}
	for _, session := range []string{"amux-ws1-tab-aa1-1", "amux-ws1-run-bb2-1"} {
		if !strings.Contains(out, session) {
			t.Fatalf("ambiguity error must list %q:\n%s", session, out)
		}
	}

	// Untagged tmux sessions are not amux's to type into.
	tmuxCmd("new-session", "-d", "-s", "foreign-session", "cat")
	if out, err := amux("send", "--session", "foreign-session", "--text", "hi"); err == nil {
		t.Fatalf("send to an untagged session must fail, got success:\n%s", out)
	}
}

// waitForCapture blocks until the session's captured content contains
// want, proving bytes actually arrived (send-keys is fire-and-forget).
func waitForCapture(t *testing.T, server, session, want string) {
	t.Helper()
	waitForCond(t, func() bool {
		out, err := exec.Command("tmux", "-L", server, "-f", "/dev/null",
			"capture-pane", "-p", "-t", session).CombinedOutput()
		return err == nil && strings.Contains(string(out), want)
	}, 5*time.Second, "pane in %s never showed %q", session, want)
}
