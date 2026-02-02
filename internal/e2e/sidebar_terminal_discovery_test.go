package e2e

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestSidebarTerminalDiscoveryAvoidsExtraSession(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	repo := initRepo(t)
	server := fmt.Sprintf("amux-e2e-%d", time.Now().UnixNano())
	defer killTmuxServer(t, server)

	homeA := t.TempDir()
	homeB := t.TempDir()
	writeRegistry(t, homeA, repo)
	writeRegistry(t, homeB, repo)
	writeConfig(t, homeA, true)
	writeConfig(t, homeB, true)

	binDirA := writeStubAssistant(t, homeA, "claude")
	binDirB := writeStubAssistant(t, homeB, "claude")

	envA := sessionEnv(binDirA, server)
	envB := sessionEnv(binDirB, server)

	sessionA, cleanupA, err := StartPTYSession(PTYOptions{
		Home:   homeA,
		Env:    envA,
		Width:  180,
		Height: 40,
	})
	if err != nil {
		t.Fatalf("start session A: %v", err)
	}
	defer cleanupA()

	waitForUIContains(t, sessionA, filepath.Base(repo), 10*time.Second)
	activatePrimaryWorkspace(t, sessionA)
	waitForUIContains(t, sessionA, "[New agent]", 15*time.Second)
	waitForUIContains(t, sessionA, "Terminal 1", 15*time.Second)

	opts := tmux.Options{ServerName: server, ConfigPath: "/dev/null"}
	wsID := workspaceIDForRepo(repo)

	waitForTerminalSessionCount(t, opts, wsID, 1, 15*time.Second)
	createSidebarTerminalTab(t, sessionA)
	waitForTerminalSessionCount(t, opts, wsID, 2, 15*time.Second)

	sessionB, cleanupB, err := StartPTYSession(PTYOptions{
		Home:   homeB,
		Env:    envB,
		Width:  180,
		Height: 40,
	})
	if err != nil {
		t.Fatalf("start session B: %v", err)
	}
	defer cleanupB()

	waitForUIContains(t, sessionB, filepath.Base(repo), 10*time.Second)
	activatePrimaryWorkspace(t, sessionB)
	waitForUIContains(t, sessionB, "Terminal 1", 15*time.Second)
	waitForUIContains(t, sessionB, "Terminal 2", 15*time.Second)

	waitForTerminalSessionCount(t, opts, wsID, 2, 15*time.Second)
}
