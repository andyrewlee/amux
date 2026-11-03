package e2e

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// copyExecutable duplicates the built amux binary so one test can delete its
// path without disturbing the process-wide shared build other tests run.
func copyExecutable(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src) // #nosec G304 -- test-owned build path
	if err != nil {
		t.Fatalf("open built binary: %v", err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatalf("create binary copy: %v", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copy binary: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close binary copy: %v", err)
	}
	if err := os.Chmod(dst, 0o755); err != nil {
		t.Fatalf("chmod binary copy: %v", err)
	}
}

// TestAgentTabSurvivesRunningBinaryDeletion pins the dev-machine failure
// where the running amux binary's file is deleted (git clean, rebuild to a
// different path) while the TUI keeps running. Every pane launched
// afterwards embeds that now-missing path, dies before its first attach,
// and the tab surfaces tmux's "can't find session" instead of an agent.
// The stable launcher published at startup must keep tab creation working.
func TestAgentTabSurvivesRunningBinaryDeletion(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	home := t.TempDir()
	repo := initRepo(t)
	writeRegistry(t, home, repo)
	writeConfig(t, home, false)
	binDir := writeStubAssistant(t, home, "claude")
	server := fmt.Sprintf("amux-e2e-%d", time.Now().UnixNano())
	defer killTmuxServer(t, server)

	shared, _, err := buildAmuxBinary()
	if err != nil {
		t.Fatalf("build amux binary: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "amux")
	copyExecutable(t, shared, bin)
	t.Setenv("AMUX_E2E_BIN", bin)

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

	// The field condition: the running binary's original path goes away.
	if err := os.Remove(bin); err != nil {
		t.Fatalf("delete running binary copy: %v", err)
	}

	opts := tmux.Options{ServerName: server, ConfigPath: "/dev/null"}
	createWorkspaceWithAgent(t, session, "feature", opts, workspaceAgentTimeout)
	waitForUIContains(t, session, "claude", workspaceAgentTimeout)
	assertAgentSessionsStayLive(t, opts, 4*time.Second)
	assertScreenNeverContains(t, session, []string{"can't find session", "DETACHED", "STOPPED"}, 4*time.Second)
}
