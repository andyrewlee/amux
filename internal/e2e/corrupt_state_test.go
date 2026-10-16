package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Corrupt ~/.amux state must degrade, not wedge: the per-store policies are
// unit-tested individually, but only a real boot proves the app survives the
// warn-and-continue path end-to-end — first frame renders, registry entries
// still load, and the input loop answers the quit dialog.
func TestCorruptConfigBootsToDashboard(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	home := t.TempDir()
	repo := initRepo(t)
	writeRegistry(t, home, repo)
	binDir := writeStubAssistant(t, home, "claude")
	server := fmt.Sprintf("amux-e2e-%d", time.Now().UnixNano())
	defer killTmuxServer(t, server)

	configPath := filepath.Join(home, ".amux", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	// Truncated JSON: the loader warns, keeps valid sections, and boots on
	// defaults rather than refusing to start.
	if err := os.WriteFile(configPath, []byte(`{"ui": {"theme": "amux`), 0o644); err != nil {
		t.Fatalf("write corrupt config: %v", err)
	}

	session, cleanup, err := StartPTYSession(PTYOptions{
		Home: home,
		Env:  sessionEnv(binDir, server),
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer cleanup()

	// First frame: the registered project still renders — config degraded to
	// defaults did not take the registry (or the render loop) down with it.
	waitForUIContains(t, session, filepath.Base(repo), persistenceTimeout)

	// The input loop is alive too: the quit dialog answers the prefix, and a
	// clean exit proves nothing in the degraded path wedged a later stage.
	quitApp(t, session)
	if err := session.WaitForExit(persistenceTimeout); err != nil {
		t.Fatalf("waiting for exit: %v", err)
	}
}
