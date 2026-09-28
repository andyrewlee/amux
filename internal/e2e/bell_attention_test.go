package e2e

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// TestAgentBellSurfacesAttentionBadge is the end-to-end check for the
// agent-bell → attention-surface channel: the fake agent emits BEL after its
// ready banner, and the workspace's sidebar row must render the "attention"
// badge (the same latch `n` jumps to) — the workspace stays a running agent,
// so this also pins attention outranking the working marker.
func TestAgentBellSurfacesAttentionBadge(t *testing.T) {
	skipIfNoGit(t)
	requireRealTmux(t)

	home := t.TempDir()
	repo := initRepo(t)
	writeRegistry(t, home, repo)

	logPath := filepath.Join(t.TempDir(), "agent_input.log")
	binDir := writeFakeAgentBell(t, home, "claude", logPath)

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

	// The agent's BEL lands ~150ms after the banner as its own PTY chunk; the
	// sidebar row for the workspace must show the attention badge.
	waitForUIContains(t, session, "attention", closeLoopTimeout)
}
