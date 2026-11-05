package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeFakeAgentAltScroll is writeFakeAgent with FAKEAGENT_ALTSCROLL=1 baked
// in — the agent enters the alternate screen and enables DECSET 1007
// (alternate scroll) without mouse reporting, the Codex shape. A host that
// honors the terminal contract delivers wheel events to it as cursor keys.
func writeFakeAgentAltScroll(t *testing.T, home, name, logPath string) string {
	t.Helper()
	bin := buildFakeAgent(t)
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	launcher := fmt.Sprintf("#!/bin/sh\nFAKEAGENT_LOG=%q FAKEAGENT_ALTSCROLL=1 exec %q \"$@\"\n", logPath, bin)
	scriptPath := filepath.Join(binDir, name)
	if err := os.WriteFile(scriptPath, []byte(launcher), 0o755); err != nil {
		t.Fatalf("write fake agent launcher: %v", err)
	}
	return binDir
}

// TestAlternateScrollWheelBecomesArrowKeysAtRawAgent pins the
// alternate-screen wheel contract end-to-end: an alt-screen agent that
// enabled DECSET 1007 instead of mouse reporting receives wheel events as
// cursor-key presses (three per notch), and unmodified PgUp/PgDn are
// forwarded as keypresses — the application owns its viewport, amux keeps no
// synthesized history for it. The raw-mode agent's log proves the exact bytes
// arrived through the real PTY/tmux path.
func TestAlternateScrollWheelBecomesArrowKeysAtRawAgent(t *testing.T) {
	skipIfNoGit(t)
	requireRealTmux(t)

	home := t.TempDir()
	repo := initRepo(t)
	writeRegistry(t, home, repo)

	logPath := filepath.Join(t.TempDir(), "agent_input.log")
	binDir := writeFakeAgentAltScroll(t, home, "claude", logPath)

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
	waitForUIContains(t, session, "FAKEAGENT READY", closeLoopTimeout)

	// Wheel up, wheel down: three cursor-key presses each, CSI form (the
	// agent never set DECCKM). The pane is an agent pane, so the wheel
	// forwards instead of scrolling anything locally.
	wheelUp := centerPaneWheelUpInput(120, 30, 2, 3)
	if err := session.SendString(wheelUp.outer); err != nil {
		t.Fatalf("send wheel up: %v", err)
	}
	got, ok := waitForFileBytes(logPath, []byte("\x1b[A\x1b[A\x1b[A"), closeLoopTimeout)
	if !ok {
		t.Fatalf("alt-scroll agent did not receive wheel-up as arrow keys\n got: % x\nouter: % x\n\nscreen:\n%s",
			got, []byte(wheelUp.outer), session.ScreenASCII())
	}

	time.Sleep(wheelThrottleDelay)
	if err := session.SendString(centerPaneWheelDownInput(120, 30, 5, 7)); err != nil {
		t.Fatalf("send wheel down: %v", err)
	}
	got, ok = waitForFileBytes(logPath, []byte("\x1b[B\x1b[B\x1b[B"), closeLoopTimeout)
	if !ok {
		t.Fatalf("alt-scroll agent did not receive wheel-down as arrow keys\n got: % x\n\nscreen:\n%s",
			got, session.ScreenASCII())
	}

	// Unmodified PgUp forwards to the application; it owns its viewport.
	if err := session.SendString("\x1b[5~"); err != nil {
		t.Fatalf("send PgUp: %v", err)
	}
	got, ok = waitForFileBytes(logPath, []byte("\x1b[5~"), closeLoopTimeout)
	if !ok {
		t.Fatalf("alt-screen agent did not receive forwarded PgUp\n got: % x\n\nscreen:\n%s",
			got, session.ScreenASCII())
	}
}
