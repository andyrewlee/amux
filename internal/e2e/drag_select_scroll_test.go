package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/ui/layout"
)

// These tests cover drag-selection edge auto-scroll in the center pane:
// press inside the terminal, drag the pointer above the viewport, and the
// view must scroll up into history — and keep scrolling while the button is
// held — whether the agent is idle, streaming lines, or repainting whole
// frames with 2J clears (the chat-agent pattern that churns scrollback
// capture/dedup under the viewport anchor).

// dragMotionInput returns an SGR mouse sequence for left-button drag motion at
// the given absolute screen cell (0-based).
func dragMotionInput(screenX, screenY int) string {
	return fmt.Sprintf("\x1b[<32;%d;%dM", screenX+1, screenY+1)
}

func leftPressInput(screenX, screenY int) string {
	return fmt.Sprintf("\x1b[<0;%d;%dM", screenX+1, screenY+1)
}

func leftReleaseInput(screenX, screenY int) string {
	return fmt.Sprintf("\x1b[<0;%d;%dm", screenX+1, screenY+1)
}

// deepHistoryScript emits 80 numbered history lines (more than a screenful of
// scrollback) once "go" is entered, then runs the given tail script.
func deepHistoryScript(tail string) string {
	return `#!/bin/sh
printf 'REDRAW READY\r\n'
IFS= read -r _
i=0
while [ $i -lt 80 ]; do
  printf 'old-frame-%02d\r\n' "$i"
  i=$((i+1))
done
` + tail
}

func writeAssistantScript(t *testing.T, home, name, script string) string {
	t.Helper()
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, name), []byte(script), 0o755); err != nil {
		t.Fatalf("write assistant script: %v", err)
	}
	return binDir
}

// dragScrollSession boots amux with the given fake assistant, opens an agent
// tab, and triggers the assistant's output with "go". It returns the session,
// the screen coordinates of a point inside the terminal content area, and the
// session's HOME directory (for locating logs/traces).
func dragScrollSession(t *testing.T, serverPrefix, script, readyNeedle string, env []string) (session *PTYSession, screenX, screenY int, home string) {
	t.Helper()
	server := fmt.Sprintf("%s-%d", serverPrefix, time.Now().UnixNano())
	return dragScrollSessionOnServer(t, server, script, readyNeedle, env)
}

func dragScrollSessionOnServer(t *testing.T, server, script, readyNeedle string, env []string) (session *PTYSession, screenX, screenY int, home string) {
	t.Helper()
	skipIfNoGit(t)
	requireRealTmux(t)

	home = t.TempDir()
	repo := initRepo(t)
	writeRegistry(t, home, repo)
	binDir := writeAssistantScript(t, home, "claude", script)

	t.Cleanup(func() { killTmuxServer(t, server) })

	session, cleanup, err := StartPTYSession(PTYOptions{
		Home: home,
		Env:  append(sessionEnv(binDir, server), env...),
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	t.Cleanup(cleanup)

	waitForUIContains(t, session, filepath.Base(repo), closeLoopTimeout)
	activatePrimaryWorkspace(t, session)
	waitForUIContains(t, session, "[New agent]", closeLoopTimeout)
	createAgentTab(t, session)
	waitForUIContains(t, session, "REDRAW READY", closeLoopTimeout)

	if err := session.SendString(centerPaneLeftClickInput(120, 30, 2, 3)); err != nil {
		t.Fatalf("focus center pane by mouse: %v", err)
	}
	if err := session.SendString("go\r"); err != nil {
		t.Fatalf("trigger assistant output: %v", err)
	}
	waitForUIContains(t, session, readyNeedle, closeLoopTimeout)

	l := layout.NewManager()
	l.Resize(120, 30)
	centerStartX := l.LeftGutter() + l.DashboardWidth() + l.GapX()
	screenX = centerStartX + 2 + 4
	screenY = l.TopGutter() + 2 + 6
	return session, screenX, screenY, home
}

// dragAboveViewportAndHold starts a selection inside the terminal and drags
// the pointer above the top edge without releasing.
func dragAboveViewportAndHold(t *testing.T, session *PTYSession, screenX, screenY int) {
	t.Helper()
	if err := session.SendString(leftPressInput(screenX, screenY)); err != nil {
		t.Fatalf("press: %v", err)
	}
	if err := session.SendString(dragMotionInput(screenX, screenY-2)); err != nil {
		t.Fatalf("drag inside viewport: %v", err)
	}
	if err := session.SendString(dragMotionInput(screenX, 0)); err != nil {
		t.Fatalf("drag above viewport: %v", err)
	}
}

var scrollIndicatorRE = regexp.MustCompile(`SCROLL:\s+([0-9]+)/`)

// waitForScrolledTo waits until the SCROLL indicator is visible and either
// needle appears on screen or the viewport reaches minOffset lines into
// history. CI can overshoot a specific sampled history line while the pointer
// is held at the edge; the scroll offset still proves the tick loop kept
// scrolling after the initial edge crossing.
func waitForScrolledTo(t *testing.T, session *PTYSession, needle string, minOffset int, timeout time.Duration) {
	t.Helper()
	sawScroll := false
	maxOffset := 0
	deadline := time.Now().Add(timeout)
	var lastScreen string
	for time.Now().Before(deadline) {
		lastScreen = session.ScreenASCII()
		if strings.Contains(lastScreen, "SCROLL:") {
			sawScroll = true
			if m := scrollIndicatorRE.FindStringSubmatch(lastScreen); len(m) == 2 {
				if offset, err := strconv.Atoi(m[1]); err == nil && offset > maxOffset {
					maxOffset = offset
				}
			}
		}
		if sawScroll && strings.Contains(lastScreen, needle) {
			return
		}
		if maxOffset >= minOffset {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !sawScroll {
		t.Fatalf("drag above viewport never entered scrollback\n\nscreen:\n%s", lastScreen)
	}
	t.Fatalf("auto-scroll never reached %q or %d lines into history (max offset %d)\n\nscreen:\n%s",
		needle, minOffset, maxOffset, lastScreen)
}

func TestDragSelectUpAutoScrollsIntoHistory(t *testing.T) {
	script := deepHistoryScript(`sleep 0.1
printf '\033[2J'
for i in 00 01 02 03 04 05 06 07 08 09 10 11; do
  printf 'new-frame-%s\r\n' "$i"
done
sleep 1000
`)
	session, screenX, screenY, _ := dragScrollSession(t, "amux-e2e-dragscroll", script, "new-frame-00", nil)

	dragAboveViewportAndHold(t, session, screenX, screenY)
	// The live view starts around old-frame-57 at the top; old-frame-30
	// requires ~27 lines of continued tick-driven scrolling.
	waitForScrolledTo(t, session, "old-frame-30", 20, 6*time.Second)
	_ = session.SendString(leftReleaseInput(screenX, 0))
}

func TestDragSelectUpAutoScrollsWhileStreaming(t *testing.T) {
	script := deepHistoryScript(`printf 'stream-begin\r\n'
i=0
while :; do
  printf 'stream-%04d\r\n' "$i"
  i=$((i+1))
  sleep 0.05
done
`)
	session, screenX, screenY, _ := dragScrollSession(t, "amux-e2e-dragstream", script, "stream-", nil)

	dragAboveViewportAndHold(t, session, screenX, screenY)
	waitForScrolledTo(t, session, "old-frame-30", 20, 6*time.Second)
	_ = session.SendString(leftReleaseInput(screenX, 0))
}

func TestDragSelectUpAutoScrollsWhileRepainting(t *testing.T) {
	script := deepHistoryScript(repaintLoop)
	session, screenX, screenY, _ := dragScrollSession(t, "amux-e2e-dragrepaint", script, "spinner-", nil)

	dragAboveViewportAndHold(t, session, screenX, screenY)
	waitForScrolledTo(t, session, "old-frame-30", 20, 6*time.Second)
	_ = session.SendString(leftReleaseInput(screenX, 0))
}

// repaintLoop mimics a coding agent's live status area: it repaints the whole
// screen (cursor home + 2J + frame) every 100ms with a changing spinner. Each
// 2J triggers CaptureNormalScreenOnClear, exercising the scrollback
// capture/dedup churn under an anchored viewport.
const repaintLoop = `n=0
while :; do
  printf '\033[H\033[2J'
  j=0
  while [ $j -lt 20 ]; do
    printf 'transcript-line-%02d\r\n' "$j"
    j=$((j+1))
  done
  printf 'spinner-%04d working...\r\n' "$n"
  n=$((n+1))
  sleep 0.1
done
`

// TestTmuxDeliversSynchronizedOutputToClient verifies the anti-flicker
// contract end to end: the terminal-features sync declaration in the session
// bootstrap makes tmux wrap its redraws in DEC 2026 markers, which the PTY
// trace observes as bytes delivered to amux's parser. Without the marker the
// vterm renders mid-repaint flushes as torn frames.
func TestTmuxDeliversSynchronizedOutputToClient(t *testing.T) {
	if !tmuxVersionSupportsSyncMarkers(t) {
		t.Skip("tmux version does not emit DEC 2026 synchronized-output markers")
	}
	script := deepHistoryScript(repaintLoop)
	server := fmt.Sprintf("amux-e2e-sync-%d", time.Now().UnixNano())
	_, _, _, home := dragScrollSessionOnServer(t, server, script, "spinner-", []string{"AMUX_PTY_TRACE=1"})

	if out, err := exec.Command("tmux", "-L", server, "show-options", "-s", "terminal-features").CombinedOutput(); err == nil {
		t.Logf("server terminal-features:\n%s", out)
	} else {
		t.Logf("show-options failed: %v: %s", err, out)
	}
	if out, err := exec.Command("tmux", "-L", server, "list-clients", "-F", "#{client_name} termfeatures=#{client_termfeatures} termtype=#{client_termtype}").CombinedOutput(); err == nil {
		t.Logf("clients:\n%s", out)
	} else {
		t.Logf("list-clients failed: %v: %s", err, out)
	}

	// The trace lands under <home>/.amux/logs as hex.Dump chunks; poll for a
	// sync-begin marker in the decoded bytes while repaints stream through.
	logDir := filepath.Join(home, ".amux", "logs")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(logDir)
		if err == nil {
			for _, e := range entries {
				if !strings.HasPrefix(e.Name(), "amux-pty-claude-") {
					continue
				}
				data, err := os.ReadFile(filepath.Join(logDir, e.Name()))
				if err != nil {
					continue
				}
				if strings.Contains(decodeTraceDump(string(data)), "\x1b[?2026h") {
					return
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("no DEC 2026 sync-begin found in PTY traces under %s; tmux is not wrapping redraws in synchronized output", logDir)
}

func tmuxVersionSupportsSyncMarkers(t *testing.T) bool {
	t.Helper()
	out, err := exec.Command("tmux", "-V").CombinedOutput()
	if err != nil {
		t.Fatalf("tmux -V failed: %v: %s", err, out)
	}
	m := regexp.MustCompile(`tmux\s+([0-9]+)\.([0-9]+)`).FindStringSubmatch(string(out))
	if len(m) != 3 {
		t.Fatalf("could not parse tmux version from %q", strings.TrimSpace(string(out)))
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	return major > 3 || (major == 3 && minor >= 3)
}

// decodeTraceDump reassembles the raw bytes from a PTY trace file, whose data
// lines are encoding/hex.Dump output (offset, spaced hex groups, ASCII column)
// interleaved with RECV/SEND chunk headers.
func decodeTraceDump(trace string) string {
	var out strings.Builder
	for _, line := range strings.Split(trace, "\n") {
		// hex.Dump line: "00000000  1b 5b 3f 32 30 32 36 68  ...  |.[?2026h...|"
		if len(line) < 10 || strings.IndexByte(line, '|') < 0 {
			continue
		}
		hexCols := line[10:]
		if bar := strings.IndexByte(hexCols, '|'); bar >= 0 {
			hexCols = hexCols[:bar]
		}
		for _, tok := range strings.Fields(hexCols) {
			if len(tok) != 2 {
				continue
			}
			var b byte
			if _, err := fmt.Sscanf(tok, "%02x", &b); err == nil {
				out.WriteByte(b)
			}
		}
	}
	return out.String()
}
