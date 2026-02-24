package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceFirstActivation_DoesNotFlashTabActive(t *testing.T) {
	skipIfNoGit(t)
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	home := t.TempDir()
	repo := initRepo(t)
	writeRegistry(t, home, repo)
	writeConfig(t, home, false)
	// Emit a small startup line, then idle. This mirrors real sessions that have
	// existing content but no current work.
	binDir := writeStubAssistantScript(t, home, "claude", "#!/bin/sh\necho booted\nsleep 1000\n")
	server := "amux-e2e-first-activation"
	defer killTmuxServer(t, server)

	env := sessionEnv(binDir, server)
	first, firstCleanup, err := StartPTYSession(PTYOptions{
		Home: home,
		Env:  env,
	})
	if err != nil {
		t.Fatalf("start first session: %v", err)
	}

	waitForUIContains(t, first, filepath.Base(repo), workspaceAgentTimeout)
	if err := first.SendString("j"); err != nil {
		t.Fatalf("move to workspace row: %v", err)
	}
	if err := first.SendString("\r"); err != nil {
		t.Fatalf("activate workspace: %v", err)
	}
	waitForUIContains(t, first, "[New agent]", workspaceAgentTimeout)
	createAgentTab(t, first)
	waitForUIContains(t, first, "claude", workspaceAgentTimeout)
	quitApp(t, first)
	if err := first.WaitForExit(persistenceTimeout); err != nil {
		t.Fatalf("waiting first exit: %v", err)
	}
	firstCleanup()

	second, secondCleanup, err := StartPTYSession(PTYOptions{
		Home: home,
		Env:  env,
	})
	if err != nil {
		t.Fatalf("start second session: %v", err)
	}
	defer secondCleanup()

	waitForUIContains(t, second, filepath.Base(repo), workspaceAgentTimeout)
	if err := second.SendString("j"); err != nil {
		t.Fatalf("move to workspace row on second start: %v", err)
	}
	if err := second.SendString("\r"); err != nil {
		t.Fatalf("activate workspace on second start: %v", err)
	}
	waitForUIContains(t, second, "claude", workspaceAgentTimeout)
	assertLabelNeverBold(t, second, "claude", 2200*time.Millisecond)
}

func assertLabelNeverBold(t *testing.T, session *PTYSession, label string, duration time.Duration) {
	t.Helper()
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		if labelBoldInScreen(session, label) {
			t.Fatalf("label %q became bold (active flash) during observation window", label)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func labelBoldInScreen(session *PTYSession, label string) bool {
	session.mu.Lock()
	defer session.mu.Unlock()
	screen := session.term.VisibleScreen()
	for _, row := range screen {
		runes := make([]rune, 0, len(row))
		bold := make([]bool, 0, len(row))
		for _, cell := range row {
			if cell.Width == 0 {
				continue
			}
			r := cell.Rune
			if r == 0 {
				r = ' '
			}
			runes = append(runes, r)
			bold = append(bold, cell.Style.Bold)
		}
		if len(runes) < len(label) {
			continue
		}
		for i := 0; i+len(label) <= len(runes); i++ {
			match := true
			segmentBold := false
			for j, want := range label {
				if runes[i+j] != want {
					match = false
					break
				}
				if bold[i+j] {
					segmentBold = true
				}
			}
			if match && segmentBold {
				return true
			}
		}
	}
	return false
}

func writeStubAssistantScript(t *testing.T, home, name, script string) string {
	t.Helper()
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	scriptPath := filepath.Join(binDir, name)
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub assistant: %v", err)
	}
	return binDir
}
