package e2e

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestExternalAgentTabsRenderUniqueLabels(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	home := t.TempDir()
	repo := initRepo(t)
	writeRegistry(t, home, repo)
	writeConfig(t, home, false)
	binDir := writeStubAssistant(t, home, "codex")
	_ = writeStubAssistant(t, home, "claude")
	server := fmt.Sprintf("amux-e2e-tablabels-%d", time.Now().UnixNano())
	defer killTmuxServer(t, server)

	createCode, createEnv, _, _ := runAmuxJSON(
		t,
		home,
		server,
		"workspace",
		"create",
		"feature",
		"--project",
		repo,
		"--assistant",
		"codex",
	)
	if createCode != 0 || !createEnv.OK {
		t.Fatalf("workspace create failed: code=%d env=%+v", createCode, createEnv)
	}
	wsID := jsonStringField(t, createEnv.Data, "id")
	if wsID == "" {
		t.Fatal("workspace create returned empty id")
	}

	for i := 0; i < 2; i++ {
		runCode, runEnv, _, _ := runAmuxJSON(
			t,
			home,
			server,
			"agent",
			"run",
			"--workspace",
			wsID,
			"--assistant",
			"codex",
		)
		if runCode != 0 || !runEnv.OK {
			t.Fatalf("agent run #%d failed: code=%d env=%+v", i+1, runCode, runEnv)
		}
	}

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
	waitForUIContains(t, session, "feature", workspaceAgentTimeout)

	// Focus the workspace row and open it.
	if err := session.SendString("k"); err != nil {
		t.Fatalf("move to workspace row: %v", err)
	}
	if err := session.SendString("\r"); err != nil {
		t.Fatalf("activate workspace: %v", err)
	}

	waitForUIContains(t, session, "codex", workspaceAgentTimeout)
	waitForUIContains(t, session, "codex 1", workspaceAgentTimeout)
}
