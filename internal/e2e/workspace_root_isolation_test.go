package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/tmux"
)

// startIsolatedFixture boots amux under the given fixture HOME on a unique
// tmux server and creates one workspace with the first assistant option. It
// returns the worktree path under `root` — the workspaces root the fixture
// should have resolved.
func startIsolatedFixture(t *testing.T, home, root string, extraEnv []string) (expectedWorktree string) {
	t.Helper()
	repo := initRepo(t)
	writeRegistry(t, home, repo)
	writeConfig(t, home, false)
	binDir := writeStubAssistant(t, home, "claude")
	server := fmt.Sprintf("amux-e2e-%d", time.Now().UnixNano())
	t.Cleanup(func() { killTmuxServer(t, server) })

	env := append(sessionEnv(binDir, server), extraEnv...)
	session, cleanup, err := StartPTYSession(PTYOptions{Home: home, Env: env})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	t.Cleanup(cleanup)

	waitForUIContains(t, session, filepath.Base(repo), workspaceAgentTimeout)
	createWorkspaceWithAgent(t, session, "feature", tmux.Options{ServerName: server, ConfigPath: "/dev/null"}, workspaceAgentTimeout)
	return filepath.Join(root, filepath.Base(repo), "feature")
}

// TestPTYSessionIgnoresAmbientWorkspaceRoot proves a PTY-launched amux does not
// create fixture worktrees in an inherited AMUX_WORKSPACES_ROOT — the defect
// that redirected e2e runs into a real user's workspace directory.
func TestPTYSessionIgnoresAmbientWorkspaceRoot(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	ambient := t.TempDir()
	sentinel := filepath.Join(ambient, "sentinel")
	if err := os.WriteFile(sentinel, []byte("do not touch"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Process-global; no t.Parallel.
	t.Setenv(config.WorkspacesRootEnvVar, ambient)

	home := t.TempDir()
	expected := startIsolatedFixture(t, home, filepath.Join(home, ".amux", "workspaces"), nil)

	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("worktree not under fixture root %q: %v", expected, err)
	}
	entries, err := os.ReadDir(ambient)
	if err != nil {
		t.Fatalf("read ambient root: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "sentinel" {
		t.Fatalf("ambient root mutated: entries=%v", entries)
	}
}

// TestPTYSessionHonorsExplicitWorkspaceRoot proves an explicit PTYOptions.Env
// override still wins over the fixture default — deliberate relocation stays
// possible when the target is test-owned.
func TestPTYSessionHonorsExplicitWorkspaceRoot(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	custom := t.TempDir()
	expected := startIsolatedFixture(t, t.TempDir(), custom, []string{config.WorkspacesRootEnvVar + "=" + custom})

	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("worktree not under explicit root %q: %v", expected, err)
	}
}
