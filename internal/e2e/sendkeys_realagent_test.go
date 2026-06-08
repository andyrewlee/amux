package e2e

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// TestSendKeysDeliversHexEnterToRawAgent covers the hardened tmux.SendKeys path
// directly against a real raw-mode agent. SendKeys is the path the `amux agent
// send` CLI (used to orchestrate agents) relies on, and it carries the bug-2
// fixes: literal text via -l and a hex 0D Enter rather than the named key. Its
// only existing integration test drives `cat` in cooked mode, which accepts a
// named Enter and plain text that does not prove literal mode. This test sends
// through SendKeys into the fixture and asserts the agent received literal
// "C-a" bytes plus a literal carriage return (0x0D). Without -l, tmux interprets
// C-a as a key name instead of text.
func TestSendKeysDeliversHexEnterToRawAgent(t *testing.T) {
	requireRealTmux(t)

	bin := buildFakeAgent(t)
	logPath := filepath.Join(t.TempDir(), "received.log")
	server := fmt.Sprintf("amux-sendkeys-%d", time.Now().UnixNano())
	opts := tmux.Options{ServerName: server, ConfigPath: "/dev/null", CommandTimeout: 5 * time.Second}
	defer killTmuxServer(t, server)

	const session = "agent"
	agentCmd := fmt.Sprintf("FAKEAGENT_LOG=%s exec %s", shQuote(logPath), shQuote(bin))
	start := exec.Command("tmux", "-L", server, "new-session", "-d", "-s", session, "sh", "-c", agentCmd)
	if out, err := start.CombinedOutput(); err != nil {
		t.Fatalf("start agent session: %v\n%s", err, out)
	}

	// Wait until the agent is in raw mode and ready (it prints the banner only
	// then), so input is never delivered before raw mode is active.
	if !waitForPaneContains(server, session, "FAKEAGENT READY", 10*time.Second) {
		t.Fatal("fake agent never signaled readiness in its pane")
	}

	if err := tmux.SendKeys(session, "C-a", true, opts); err != nil {
		t.Fatalf("SendKeys: %v", err)
	}

	want := []byte{'C', '-', 'a', 0x0d}
	got, ok := waitForFileBytes(logPath, want, 10*time.Second)
	if !ok {
		t.Fatalf("agent did not receive text + hex 0D via SendKeys\n got: % x\nwant: % x", got, want)
	}
}

// waitForPaneContains polls a tmux pane's captured contents for substr.
func waitForPaneContains(server, session, substr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, _ := exec.Command("tmux", "-L", server, "capture-pane", "-p", "-t", session).CombinedOutput()
		if strings.Contains(string(out), substr) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func shQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
