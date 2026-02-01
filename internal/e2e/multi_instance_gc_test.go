package e2e

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestMultiInstanceOrphanGCDoesNotKillNewWorkspace(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	repo := initRepo(t)
	server := fmt.Sprintf("amux-e2e-%d", time.Now().UnixNano())
	defer killTmuxServer(t, server)

	homeA := t.TempDir()
	homeB := t.TempDir()
	writeRegistry(t, homeA, repo)
	writeRegistry(t, homeB, repo)
	writeConfig(t, homeA, false)
	writeConfig(t, homeB, false)

	binDirA := writeStubAssistant(t, homeA, "claude")
	binDirB := writeStubAssistant(t, homeB, "claude")

	envA := append(sessionEnv(binDirA, server), "AMUX_TMUX_SYNC_INTERVAL=1s")
	envB := append(sessionEnv(binDirB, server), "AMUX_TMUX_SYNC_INTERVAL=1s")

	sessionB, cleanupB, err := StartPTYSession(PTYOptions{
		Home: homeB,
		Env:  envB,
	})
	if err != nil {
		t.Fatalf("start session B: %v", err)
	}
	defer cleanupB()

	waitForUIContains(t, sessionB, filepath.Base(repo), 10*time.Second)

	sessionA, cleanupA, err := StartPTYSession(PTYOptions{
		Home: homeA,
		Env:  envA,
	})
	if err != nil {
		t.Fatalf("start session A: %v", err)
	}
	defer cleanupA()

	waitForUIContains(t, sessionA, filepath.Base(repo), 10*time.Second)

	createWorkspaceFromDashboard(t, sessionA, "feature-gc")
	waitForUIContains(t, sessionA, "feature-gc", 15*time.Second)

	if err := sessionA.SendString("k"); err != nil {
		t.Fatalf("move to workspace row: %v", err)
	}
	if err := sessionA.SendString("\r"); err != nil {
		t.Fatalf("activate workspace: %v", err)
	}
	waitForUIContains(t, sessionA, "[New agent]", 15*time.Second)

	createAgentTab(t, sessionA)
	waitForUIContains(t, sessionA, "claude", 15*time.Second)

	opts := tmux.Options{ServerName: server, ConfigPath: "/dev/null"}
	waitForAgentSessions(t, opts, 15*time.Second)
	assertAgentSessionsStayLive(t, opts, 8*time.Second)
}
