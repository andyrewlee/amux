package e2e

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestWorkspaceCreateAgentsHaveDistinctSessions(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	home := t.TempDir()
	repo := initRepo(t)
	writeRegistry(t, home, repo)
	writeConfig(t, home, false)
	binDir := writeStubAssistant(t, home, "claude")
	_ = writeStubAssistant(t, home, "codex")
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

	createAgentTabWithSelection(t, session, 0, workspaceAgentTimeout) // claude
	if err := session.WaitForAbsent("New Agent", 3*time.Second); err != nil {
		t.Fatalf("wait for picker close: %v", err)
	}
	createAgentTabWithSelection(t, session, 1, workspaceAgentTimeout) // codex

	opts := tmux.Options{ServerName: server, ConfigPath: "/dev/null"}
	byAssistant := waitForAssistantSessions(t, opts, map[string]bool{"claude": true, "codex": true}, workspaceAgentTimeout)

	uniqueSessions := make(map[string]struct{})
	for _, sessions := range byAssistant {
		for _, name := range sessions {
			uniqueSessions[name] = struct{}{}
		}
	}
	if len(uniqueSessions) != 2 {
		t.Fatalf("expected 2 distinct agent sessions, got %d: %+v", len(uniqueSessions), byAssistant)
	}
}

func createAgentTabWithSelection(t *testing.T, session *PTYSession, down int, timeout time.Duration) {
	t.Helper()
	sendPrefixCommand(t, session, "a")
	waitForUIContains(t, session, "New Agent", timeout)
	for i := 0; i < down; i++ {
		if err := session.SendString("\t"); err != nil {
			t.Fatalf("select agent option: %v", err)
		}
	}
	if err := session.SendString("\r"); err != nil {
		t.Fatalf("confirm agent selection: %v", err)
	}
}
