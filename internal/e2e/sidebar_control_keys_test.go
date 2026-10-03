package e2e

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/ui/layout"
)

// TestCloseLoopSidebarControlKeysReachRawProcess is the sidebar half of the
// control-byte contract: a focused sidebar terminal must receive Ctrl+Q and
// Ctrl+] as their C0 bytes (0x11, 0x1d) — the center pane's tab-cycling
// reservation must not leak across panes. It drives a real keystroke
// through amux's actual input path into a real raw-mode process, the same
// guarantee as TestCloseLoopKeystrokeDeliveryToRawAgent.
func TestCloseLoopSidebarControlKeysReachRawProcess(t *testing.T) {
	skipIfNoGit(t)
	requireRealTmux(t)

	home := t.TempDir()
	repo := initRepo(t)
	writeRegistry(t, home, repo)

	logPath := filepath.Join(t.TempDir(), "sidebar_input.log")
	bin := buildFakeAgent(t)
	// The sidebar launches `exec $SHELL -l`, so this launcher deliberately
	// ignores argv (a bare -l would land in fakeagent's flag parser) and just
	// execs the recorder with its log path baked in.
	launcher := filepath.Join(home, "fake-shell")
	script := fmt.Sprintf("#!/bin/sh\nexec env FAKEAGENT_LOG=%q %q\n", logPath, bin)
	if err := os.WriteFile(launcher, []byte(script), 0o755); err != nil {
		t.Fatalf("write sidebar shell launcher: %v", err)
	}

	server := fmt.Sprintf("amux-e2e-%d", time.Now().UnixNano())
	defer killTmuxServer(t, server)

	session, cleanup, err := StartPTYSession(PTYOptions{
		Home:   home,
		Env:    append(sessionEnv(home, server), "SHELL="+launcher),
		Width:  180,
		Height: 40,
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer cleanup()

	waitForUIContains(t, session, filepath.Base(repo), closeLoopTimeout)
	activatePrimaryWorkspace(t, session)
	waitForUIContains(t, session, "Terminal 1", closeLoopTimeout)
	// The sidebar terminal's fake shell prints this only once it is in raw
	// mode and ready for input.
	bannerY := waitForScreenLine(t, session, "FAKEAGENT READY", closeLoopTimeout)

	// Focus the sidebar terminal through the actual UI: click its pane at the
	// ready banner's row. The sidebar column comes from the layout manager —
	// left gutter + dashboard + gap + center + gap.
	l := layout.NewManager()
	l.Resize(180, 40)
	if l.SidebarWidth() <= 0 {
		t.Fatalf("180x40 must render the sidebar pane (mode %v)", l.Mode())
	}
	sidebarX := l.LeftGutter() + l.DashboardWidth() + l.GapX() + l.CenterWidth() + l.GapX()
	if err := session.SendString(leftClickInput(sidebarX+4, bannerY)); err != nil {
		t.Fatalf("focus sidebar terminal: %v", err)
	}

	// Prove focus and readiness with a unique sentinel in the real recorder
	// before sending any control bytes — no sleeps, ordered delivery.
	sentinel := []byte("SBFOCUSOK")
	if err := session.SendBytes(sentinel); err != nil {
		t.Fatalf("send focus sentinel: %v", err)
	}
	got, ok := waitForFileBytes(logPath, sentinel, closeLoopTimeout)
	if !ok {
		t.Fatalf("sidebar terminal did not receive focus sentinel\n got: % x\nwant: % x\n\nscreen:\n%s",
			got, sentinel, session.ScreenASCII())
	}

	// Legacy C0 delivery: Ctrl+Q, Ctrl+], and CR inside a unique frame.
	legacyWant := append(append(append(sentinel, []byte("<LEGACY>")...), 0x11, 0x1d, 0x0d), []byte("</LEGACY>")...)
	legacySend := append(append(append([]byte{}, []byte("<LEGACY>")...), 0x11, 0x1d, 0x0d), []byte("</LEGACY>")...)
	if err := session.SendBytes(legacySend); err != nil {
		t.Fatalf("send legacy control frame: %v", err)
	}
	got, ok = waitForFileBytes(logPath, legacyWant, closeLoopTimeout)
	if !ok {
		t.Fatalf("sidebar terminal did not receive legacy control bytes\n got: % x\nwant: % x\n\nscreen:\n%s",
			got, legacyWant, session.ScreenASCII())
	}

	// Enhanced CSI-u delivery: ctrl+q (113;5u) and ctrl+] (93;5u) plus CR
	// inside a second unique frame.
	enhancedSend := []byte("<ENHANCED>\x1b[113;5u\x1b[93;5u\x0d</ENHANCED>")
	if err := session.SendString(string(enhancedSend)); err != nil {
		t.Fatalf("send enhanced control frame: %v", err)
	}
	enhancedWant := append(append(legacyWant, []byte("<ENHANCED>")...), 0x11, 0x1d, 0x0d)
	enhancedWant = append(enhancedWant, []byte("</ENHANCED>")...)
	got, ok = waitForFileBytes(logPath, enhancedWant, closeLoopTimeout)
	if !ok {
		t.Fatalf("sidebar terminal did not receive enhanced control bytes\n got: % x\nwant: % x\n\nscreen:\n%s",
			got, enhancedWant, session.ScreenASCII())
	}
	if !bytes.Contains(got, []byte{0x11, 0x1d, 0x0d}) {
		t.Fatalf("Ctrl+Q/Ctrl+]/CR were not delivered as literal control bytes; got % x", got)
	}
}

// waitForScreenLine polls the screen until needle appears and returns the
// row index of the first line containing it.
func waitForScreenLine(t *testing.T, session *PTYSession, needle string, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		last = session.ScreenASCII()
		for y, line := range strings.Split(last, "\n") {
			if strings.Contains(line, needle) {
				return y
			}
		}
		time.Sleep(screenPollInterval)
	}
	t.Fatalf("timeout waiting for %q on screen\n\nScreen:\n%s", needle, last)
	return 0
}
