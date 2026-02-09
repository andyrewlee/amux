package tmux

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// splitPane splits the current window in the given session and runs command.
func splitPane(t *testing.T, opts Options, session, command string) {
	t.Helper()
	args := tmuxArgs(opts, "split-window", "-t", session, "sh", "-c", command)
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("split pane in %s: %v\n%s", session, err, out)
	}
}

// selectPane selects a pane target (e.g. "session:0.1") as active.
func selectPane(t *testing.T, opts Options, target string) {
	t.Helper()
	args := tmuxArgs(opts, "select-pane", "-t", target)
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("select pane %s: %v\n%s", target, err, out)
	}
}

// ---------------------------------------------------------------------------
// CapturePane prefix-collision tests
// ---------------------------------------------------------------------------

func TestCapturePane_NonexistentSessionWithPrefixCollision(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "amux-ws-tab-10", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	output, err := CapturePane("amux-ws-tab-1", opts)
	if err == nil {
		t.Fatalf("expected error for missing exact session, got output=%q", output)
	}
	if output != nil {
		t.Fatalf("expected nil output for missing exact session, got %q", output)
	}
}

func TestCapturePaneTail_UsesActivePaneInSplitSession(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "split-active", "printf 'PANE_A_MARKER\\n'; sleep 300")
	splitPane(t, opts, "split-active", "printf 'PANE_B_MARKER\\n'; sleep 300")
	selectPane(t, opts, "split-active:0.1")
	time.Sleep(100 * time.Millisecond)

	tail, ok := CapturePaneTail("split-active", 20, opts)
	if !ok {
		t.Fatalf("expected capture to succeed")
	}
	if tail == "" {
		t.Fatalf("expected non-empty tail output")
	}
	if !strings.Contains(tail, "PANE_B_MARKER") {
		t.Fatalf("expected active pane marker in output, got %q", tail)
	}
	if strings.Contains(tail, "PANE_A_MARKER") {
		t.Fatalf("expected inactive pane marker to be absent, got %q", tail)
	}
}
