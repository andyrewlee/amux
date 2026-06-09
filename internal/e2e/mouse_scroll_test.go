package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMouseWheelScrollsNormalScreenChatRedrawHistory(t *testing.T) {
	skipIfNoGit(t)
	requireRealTmux(t)

	home := t.TempDir()
	repo := initRepo(t)
	writeRegistry(t, home, repo)
	binDir := writeRedrawAssistant(t, home, "claude")

	server := fmt.Sprintf("amux-e2e-scroll-%d", time.Now().UnixNano())
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
	waitForUIContains(t, session, "REDRAW READY", closeLoopTimeout)
	if err := session.SendString(centerPaneLeftClickInput(120, 30, 2, 3)); err != nil {
		t.Fatalf("focus center pane by mouse: %v", err)
	}
	if err := session.SendString("go\r"); err != nil {
		t.Fatalf("trigger redraw: %v", err)
	}
	waitForUIContains(t, session, "new-frame-00", closeLoopTimeout)

	if stringsContains(session.ScreenASCII(), "old-frame-00") {
		t.Fatalf("expected old frame to start off-screen before scroll\n\nscreen:\n%s", session.ScreenASCII())
	}

	wheelInput := centerPaneWheelUpInput(120, 30, 2, 3)
	for i := 0; i < 4; i++ {
		if err := session.SendString(wheelInput.outer); err != nil {
			t.Fatalf("send mouse wheel: %v", err)
		}
	}
	waitForUIContains(t, session, "old-frame-00", closeLoopTimeout)
	waitForUIContains(t, session, "SCROLL:", closeLoopTimeout)

	sendPrefixCommand(t, session, "d")
	waitForUIContains(t, session, "new-frame-00", closeLoopTimeout)

	sendPrefixCommand(t, session, "u")
	waitForUIContains(t, session, "old-frame-00", closeLoopTimeout)
	waitForUIContains(t, session, "SCROLL:", closeLoopTimeout)
}

func writeRedrawAssistant(t *testing.T, home, name string) string {
	t.Helper()
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	scriptPath := filepath.Join(binDir, name)
	script := `#!/bin/sh
printf 'REDRAW READY\r\n'
IFS= read -r _
printf '\033[?2026h'
for i in 00 01 02 03 04 05 06 07 08 09 10 11; do
  printf 'old-frame-%s\r\n' "$i"
done
printf '\033[2J\033[3J'
for i in 00 01 02 03 04 05 06 07 08 09 10 11; do
  printf 'new-frame-%s\r\n' "$i"
done
printf '\033[?2026l'
sleep 1000
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write redraw assistant: %v", err)
	}
	return binDir
}
