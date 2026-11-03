package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// TestTwoInstancesShareWorkspaceWithoutAttachWar reproduces the multi-instance
// detach war: two amux instances with the same workspace open both discover and
// attach the workspace's agent sessions. When any attach path carried -d, each
// instance kicked the other's client, both EOF-restarted, and the war ended
// only when one side exhausted its PTY restart budget — the losing tab showed
// tmux's "[detached (from session …)]" before the winner settled. amux now
// never emits attach -d at all (see tmux.NewClientCommand), so this test
// asserts the war cannot start, including during the create→attach window
// where a fresh session briefly has no clients and a probe-based "detach only
// when unattended" policy used to race the creator.
func TestTwoInstancesShareWorkspaceWithoutAttachWar(t *testing.T) {
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
	opts := tmux.Options{ServerName: server, ConfigPath: "/dev/null"}

	// Instance A creates the workspace and its agent tab.
	sessionA, cleanupA, err := StartPTYSession(PTYOptions{Home: home, Env: env})
	if err != nil {
		t.Fatalf("start instance A: %v", err)
	}
	defer cleanupA()
	waitForUIContains(t, sessionA, filepath.Base(repo), workspaceAgentTimeout)
	createWorkspaceWithAgent(t, sessionA, "feature", opts, workspaceAgentTimeout)

	// Instance B opens the same project and activates the same workspace; its
	// discovery adopts the live agent session. B polls tmux at the floor of
	// AMUX_TMUX_SYNC_INTERVAL (the app clamps values below 500ms), so its
	// adoption attempts land inside A's later create→attach windows — the
	// race a "probe for clients, else -d" policy used to lose.
	sessionB, cleanupB, err := StartPTYSession(PTYOptions{Home: home, Env: withSyncInterval(env, "500ms")})
	if err != nil {
		t.Fatalf("start instance B: %v", err)
	}
	defer cleanupB()
	waitForUIContains(t, sessionB, filepath.Base(repo), workspaceAgentTimeout)
	selectWorkspaceRow(t, sessionB, "feature", workspaceAgentTimeout)

	// B adopted the agent tab…
	waitForUIContains(t, sessionB, "claude", workspaceAgentTimeout)

	// …without evicting A: no detach message in A's tab, no restart-limit
	// detached/stop markers, and the shared session stays live for both.
	assertScreenNeverContains(t, sessionA, []string{"[detached", "DETACHED", "STOPPED"}, 3*time.Second)
	assertAgentSessionsStayLive(t, opts, 3*time.Second)

	// Race phase: while B keeps adopting at its fast sync cadence, A creates
	// two more agent tabs. Each create mints a session that is briefly
	// client-free between new-session and A's attach — exactly when a
	// detaching adoption would kick A's just-attached client. The war, if it
	// can still happen, happens here.
	stopsBefore := countPTYStoppedLines(t, home)
	createAgentTabWithSelection(t, sessionA, 1, workspaceAgentTimeout) // codex
	waitForUIContains(t, sessionB, "codex", workspaceAgentTimeout)
	createAgentTabWithSelection(t, sessionA, 0, workspaceAgentTimeout) // claude

	// Settle and assert the war never fired, three independent ways: neither
	// screen ever shows tmux's detach notice (or a restart-limit marker), the
	// shared log shows zero new "PTY stopped for tab" EOF-restart lines
	// (screen-repaint-independent), and every agent session stays live.
	assertScreenNeverContains(t, sessionA, []string{"[detached", "DETACHED", "STOPPED"}, 4*time.Second)
	assertScreenNeverContains(t, sessionB, []string{"[detached", "DETACHED", "STOPPED"}, 4*time.Second)
	if stops := countPTYStoppedLines(t, home); stops != stopsBefore {
		t.Fatalf("attach war: %d new \"PTY stopped for tab\" log lines during shared-workspace tab creation (before: %d, after: %d) — an instance's client was evicted and its PTY restart loop fired", stops-stopsBefore, stopsBefore, stops)
	}
	assertAgentSessionsStayLive(t, opts, 4*time.Second)

	// Both instances must still be viewing the shared sessions concurrently —
	// sharing, not winner-takes-all.
	waitForAssistantSessions(t, opts, map[string]bool{"claude": true, "codex": true}, workspaceAgentTimeout)
}

// withSyncInterval returns env with AMUX_TMUX_SYNC_INTERVAL replaced (last
// binding wins in the child's environment only when the key is unique, so
// replace rather than append).
func withSyncInterval(env []string, interval string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "AMUX_TMUX_SYNC_INTERVAL=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "AMUX_TMUX_SYNC_INTERVAL="+interval)
}

// countPTYStoppedLines counts EOF-restart war signatures across the shared log
// directory both instances write to. Every eviction forces the victim's PTY
// to EOF, and each EOF logs exactly one "PTY stopped for tab" line, so a war
// cannot hide behind a lucky screen repaint.
func countPTYStoppedLines(t *testing.T, home string) int {
	t.Helper()
	dir := filepath.Join(home, ".amux", "logs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0 // no logs yet: zero war lines is vacuously true
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "amux-") || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		count += strings.Count(string(data), "PTY stopped for tab")
	}
	return count
}
