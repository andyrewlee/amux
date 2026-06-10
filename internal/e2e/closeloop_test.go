package e2e

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// requireRealTmux skips the test only when tmux is genuinely unusable. It avoids
// the shared ensureTmuxServer probe, whose bare `start-server` races against an
// empty server self-exiting and so skips even where tmux works fine. Here a
// detached throwaway session keeps the server alive long enough to confirm
// reachability, so this test runs (rather than silently skips) wherever tmux is
// actually present — which is the entire point of a close-the-loop test.
func requireRealTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	server := fmt.Sprintf("amux-closeloop-check-%d", time.Now().UnixNano())
	probe := exec.Command("tmux", "-L", server, "new-session", "-d", "-s", "probe", "sh", "-c", "sleep 5")
	if out, err := probe.CombinedOutput(); err != nil {
		t.Skipf("tmux unusable: %v\n%s", err, out)
	}
	_ = exec.Command("tmux", "-L", server, "kill-server").Run()
}

// writeFakeAgent installs the fake raw-mode agent as the assistant binary named
// `name` on amux's PATH. It uses a tiny launcher script that bakes in
// FAKEAGENT_LOG so the recording path survives regardless of how the environment
// propagates through amux -> tmux -> pane. Returns the bin dir to put on PATH.
func writeFakeAgent(t *testing.T, home, name, logPath string) string {
	t.Helper()
	bin := buildFakeAgent(t)
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	launcher := fmt.Sprintf("#!/bin/sh\nFAKEAGENT_LOG=%q exec %q \"$@\"\n", logPath, bin)
	scriptPath := filepath.Join(binDir, name)
	if err := os.WriteFile(scriptPath, []byte(launcher), 0o755); err != nil {
		t.Fatalf("write fake agent launcher: %v", err)
	}
	return binDir
}

// TestCloseLoopKeystrokeDeliveryToRawAgent is the close-the-loop guarantee: it
// drives a real keystroke through amux's actual input path into a real raw-mode
// agent and asserts the agent received the bytes intact, including a literal
// carriage return (0x0D). This is the test the four historically-escaped bugs
// could not survive: a regression to the named Enter key, a dropped/zeroed
// enter delay, or input sent before the agent is ready all change what the
// agent records here.
func TestCloseLoopKeystrokeDeliveryToRawAgent(t *testing.T) {
	skipIfNoGit(t)
	requireRealTmux(t)

	home := t.TempDir()
	repo := initRepo(t)
	writeRegistry(t, home, repo)

	logPath := filepath.Join(t.TempDir(), "agent_input.log")
	binDir := writeFakeAgent(t, home, "claude", logPath)

	server := fmt.Sprintf("amux-e2e-%d", time.Now().UnixNano())
	defer killTmuxServer(t, server)

	session, cleanup, err := StartPTYSession(PTYOptions{
		Home: home,
		Env:  sessionEnv(binDir, server),
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer cleanup()

	waitForUIContains(t, session, filepath.Base(repo), closeLoopTimeout)
	activatePrimaryWorkspace(t, session)
	waitForUIContains(t, session, "[New agent]", closeLoopTimeout)
	createAgentTab(t, session)

	// The fake agent prints this only after it is in raw mode and ready for
	// input, so waiting for it gates the test against premature sends (bug #4).
	waitForUIContains(t, session, "FAKEAGENT READY", closeLoopTimeout)

	// Type into the focused agent. amux must forward a literal CR (0x0D), Ctrl+U
	// (0x15), and Ctrl+D (0x04) through the real TUI/tmux/raw-agent path.
	if err := session.SendBytes([]byte{'h', 'e', 'l', 'l', 'o', 0x0d, 0x15, 0x04}); err != nil {
		t.Fatalf("send keystrokes: %v", err)
	}

	want := []byte{'h', 'e', 'l', 'l', 'o', 0x0d, 0x15, 0x04}
	got, ok := waitForFileBytes(logPath, want, closeLoopTimeout)
	if !ok {
		t.Fatalf("agent did not receive intact keystrokes via amux\n got: % x\nwant: % x\n\nscreen:\n%s",
			got, want, session.ScreenASCII())
	}
	if !bytes.Contains(got, []byte{0x0d}) {
		t.Fatalf("Enter was not delivered as a literal carriage return (0x0D); got % x", got)
	}
	if !bytes.Contains(got, []byte{0x15, 0x04}) {
		t.Fatalf("Ctrl+U/Ctrl+D were not delivered as literal control bytes; got % x", got)
	}

	// Some terminals send Ctrl+U/Ctrl+D using the CSI-u keyboard protocol once
	// the app requests keyboard enhancements. Verify those enhanced key reports
	// still become the control bytes a raw agent expects.
	if err := session.SendString("\x1b[117;5u\x1b[100;5u"); err != nil {
		t.Fatalf("send enhanced ctrl keys: %v", err)
	}
	wantEnhanced := append(want, 0x15, 0x04)
	got, ok = waitForFileBytes(logPath, wantEnhanced, closeLoopTimeout)
	if !ok {
		t.Fatalf("agent did not receive enhanced Ctrl+U/Ctrl+D via amux\n got: % x\nwant: % x\n\nscreen:\n%s",
			got, wantEnhanced, session.ScreenASCII())
	}

	wheelInput := centerPaneWheelUpInput(120, 30, 2, 3)
	if err := session.SendString(wheelInput.outer); err != nil {
		t.Fatalf("send mouse wheel: %v", err)
	}
	got, ok = waitForFileBytes(logPath, []byte(wheelInput.forwarded), closeLoopTimeout)
	if !ok {
		t.Fatalf("agent did not receive forwarded wheel input via amux\n got: % x\nwant: % x\nouter: % x\n\nscreen:\n%s",
			got, []byte(wheelInput.forwarded), []byte(wheelInput.outer), session.ScreenASCII())
	}
}
