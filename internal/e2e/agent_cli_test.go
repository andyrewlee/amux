package e2e

import (
	"errors"
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
// a hosted agent or supervising script would. Failures carry the stable
// exit-code contract: 3 = target not found or ambiguous, 4 = unreachable
// (no server, dead pane), 2 = usage.

// exitCodeOf extracts a failed command's exit status for contract pins.
func exitCodeOf(t *testing.T, err error) int {
	t.Helper()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("expected an ExitError, got %v", err)
	}
	return ee.ExitCode()
}

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

	// Before any session exists (no tmux server): ls says "empty fleet"
	// with exit 0 — nothing hosted is a valid answer — while send has a
	// destination it cannot reach: exit 4.
	if out, err := amux("agent", "ls"); err != nil || !strings.Contains(out, "WORKSPACE") {
		t.Fatalf("agent ls with no server must be a valid empty answer (header, no rows), got (%v):\n%s", err, out)
	}
	if out, err := amux("send", "--session", "anything", "--text", "hi"); err == nil {
		t.Fatalf("send with no server must fail, got success:\n%s", out)
	} else if code := exitCodeOf(t, err); code != 4 {
		t.Fatalf("send with no server must exit 4 (unreachable), got %d:\n%s", code, out)
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
	if code := exitCodeOf(t, deadErr); code != 4 {
		t.Fatalf("send to a dead pane must exit 4 (unreachable), got %d", code)
	}

	// The refusal must not depend on the pane having reported: a dead
	// pane that never spoke the protocol has no report options, and
	// send-keys against its corpse would still "succeed". Liveness is
	// observed directly (the batched pane scan), so this session — dead,
	// no report, own workspace so the ambiguity test stays two-way —
	// must be refused with 4 too.
	tmuxCmd("new-session", "-d", "-s", "amux-ws2-run-silent-1", "sh", "-c", "sleep 1; exit 0",
		";", "set-option", "-w", "-t", "amux-ws2-run-silent-1", "remain-on-exit", "on")
	tmuxCmd("set-option", "-t", "amux-ws2-run-silent-1", "@amux_workspace", "ws2")
	tmuxCmd("set-option", "-t", "amux-ws2-run-silent-1", "@amux_workspace_name", "silent-e2e")
	tmuxCmd("set-option", "-t", "amux-ws2-run-silent-1", "@amux_type", "run")
	waitForCond(t, func() bool {
		out, err := exec.Command("tmux", "-L", server, "-f", "/dev/null",
			"list-panes", "-t", "amux-ws2-run-silent-1", "-F", "#{pane_dead}").CombinedOutput()
		return err == nil && strings.TrimSpace(string(out)) == "1"
	}, 5*time.Second, "silent pane in amux-ws2-run-silent-1 never died")
	if out, err := amux("send", "--workspace", "silent-e2e", "--text", "anyone?"); err == nil {
		t.Fatalf("send to a dead pane that never reported must fail, got success:\n%s", out)
	} else if code := exitCodeOf(t, err); code != 4 {
		t.Fatalf("send to an unreported dead pane must exit 4 (unreachable), got %d:\n%s", code, out)
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
	if code := exitCodeOf(t, err); code != 3 {
		t.Fatalf("ambiguous target must exit 3, got %d:\n%s", code, out)
	}
	for _, session := range []string{"amux-ws1-tab-aa1-1", "amux-ws1-run-bb2-1"} {
		if !strings.Contains(out, session) {
			t.Fatalf("ambiguity error must list %q:\n%s", session, out)
		}
	}

	// Untagged tmux sessions are not amux's to type into, and an unknown
	// workspace resolves to nothing — both are selector failures: 3.
	tmuxCmd("new-session", "-d", "-s", "foreign-session", "cat")
	if out, err := amux("send", "--session", "foreign-session", "--text", "hi"); err == nil {
		t.Fatalf("send to an untagged session must fail, got success:\n%s", out)
	} else if code := exitCodeOf(t, err); code != 3 {
		t.Fatalf("send to an untagged session must exit 3 (not found), got %d:\n%s", code, out)
	}
	if out, err := amux("send", "--workspace", "no-such-workspace", "--text", "hi"); err == nil {
		t.Fatalf("send to an unknown workspace must fail, got success:\n%s", out)
	} else if code := exitCodeOf(t, err); code != 3 {
		t.Fatalf("send to an unknown workspace must exit 3 (not found), got %d:\n%s", code, out)
	}
}

// `amux wait` exit passthrough against a real server: a program's real
// exit status becomes wait's exit status when its pane dies while wait
// watches, and a session already dead when wait started never fires —
// the baseline that keeps a lingering dead run session from ending every
// later wait with stale news.
func TestWaitExitPassthroughRealBinary(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	bin, cleanupBin, err := buildAmuxBinary()
	if err != nil {
		t.Fatalf("build amux binary: %v", err)
	}
	defer cleanupBin()

	server := fmt.Sprintf("amux-e2e-waitexit-%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", server, "kill-server").Run() })
	env := append(os.Environ(), "AMUX_TMUX_SERVER="+server)
	tmuxCmd := func(args ...string) string {
		full := append([]string{"tmux", "-L", server, "-f", "/dev/null"}, args...)
		out, _ := exec.Command(full[0], full[1:]...).CombinedOutput()
		return string(out)
	}
	tagRunSession := func(session string) {
		tmuxCmd("set-option", "-t", session, "@amux_workspace", "ws1")
		tmuxCmd("set-option", "-t", session, "@amux_workspace_name", "wait-e2e")
		tmuxCmd("set-option", "-t", session, "@amux_type", "run")
	}

	// The stale session: dead (exit 5) before wait ever starts.
	// remain-on-exit is chained into the new-session command (the `;`
	// argv separator, exactly how amux's own launcher does it): a pane
	// that dies before a separate set-option lands is destroyed with its
	// session, so the sleep alone would be a timing budget, not a
	// guarantee.
	tmuxCmd("new-session", "-d", "-s", "amux-ws1-run-stale", "sh", "-c", "sleep 1; exit 5",
		";", "set-option", "-w", "-t", "amux-ws1-run-stale", "remain-on-exit", "on")
	tagRunSession("amux-ws1-run-stale")
	waitForCond(t, func() bool {
		out, err := exec.Command("tmux", "-L", server, "-f", "/dev/null",
			"list-panes", "-t", "amux-ws1-run-stale", "-F", "#{pane_dead}").CombinedOutput()
		return err == nil && strings.TrimSpace(string(out)) == "1"
	}, 5*time.Second, "stale pane never died")

	// The fresh session: exits 7 a few seconds in — after wait is
	// watching. This is the orchestrator's shape: start wait, then spawn
	// the thing whose outcome you need. remain-on-exit rides the create
	// for the same reason as the stale session's.
	tmuxCmd("new-session", "-d", "-s", "amux-ws1-run-fresh", "sh", "-c", "sleep 4; exit 7",
		";", "set-option", "-w", "-t", "amux-ws1-run-fresh", "remain-on-exit", "on")
	tagRunSession("amux-ws1-run-fresh")

	wait := exec.Command(bin, "wait", "--state", "any", "--workspace", "wait-e2e", "--timeout", "20s")
	wait.Env = env
	out, err := wait.CombinedOutput()
	got := string(out)
	if err == nil {
		t.Fatalf("wait must exit with the program's status 7, got success:\n%s", got)
	}
	if code := exitCodeOf(t, err); code != 7 {
		t.Fatalf("wait exit = %d, want 7 (the program's real status):\n%s", code, got)
	}
	if !strings.Contains(got, "session=amux-ws1-run-fresh") || !strings.Contains(got, "exit=7") {
		t.Fatalf("wait output must name the session and its status:\n%s", got)
	}
	if strings.Contains(got, "exit=5") || strings.Contains(got, "stale") {
		t.Fatalf("a session dead before wait started must never fire (baseline):\n%s", got)
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
