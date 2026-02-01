package e2e

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

const workspaceAgentTimeout = 20 * time.Second

func TestWorkspaceCreateAgentTabStaysRunning(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	home := t.TempDir()
	repo := initRepo(t)
	writeRegistry(t, home, repo)
	writeConfig(t, home, false)
	binDir := writeStubAssistant(t, home, "claude")
	server := fmt.Sprintf("amux-e2e-%d", time.Now().UnixNano())
	defer killTmuxServer(t, server)

	env := sessionEnv(binDir, server)
	env = append(env, "AMUX_TMUX_SYNC_INTERVAL=1s")
	session, cleanup, err := StartPTYSession(PTYOptions{
		Home: home,
		Env:  env,
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer cleanup()

	waitForUIContains(t, session, filepath.Base(repo), workspaceAgentTimeout)

	createWorkspaceFromDashboard(t, session, "feature")
	waitForUIContains(t, session, "feature", workspaceAgentTimeout)

	// Select the newly created workspace (one row above "New").
	if err := session.SendString("k"); err != nil {
		t.Fatalf("move to workspace row: %v", err)
	}
	if err := session.SendString("\r"); err != nil {
		t.Fatalf("activate workspace: %v", err)
	}
	waitForUIContains(t, session, "[New agent]", workspaceAgentTimeout)

	createAgentTab(t, session)
	waitForUIContains(t, session, "claude", workspaceAgentTimeout)

	opts := tmux.Options{ServerName: server, ConfigPath: "/dev/null"}
	waitForAgentSessions(t, opts, workspaceAgentTimeout)
	assertAgentSessionsStayLive(t, opts, 8*time.Second)
	assertScreenNeverContains(t, session, []string{"STOPPED", "DETACHED"}, 5*time.Second)
}
