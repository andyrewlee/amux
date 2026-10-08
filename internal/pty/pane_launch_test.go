package pty

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/panelaunch"
	"github.com/andyrewlee/amux/internal/tmux"
)

// TestMain lets this test binary stand in for the amux launcher: prepared
// pane commands invoke os.Executable, which under `go test` is this binary.
// Dispatch the private helper argv before testing's flag parse or any test
// runs — a pane spawn must never recurse into the test suite.
func TestMain(m *testing.M) {
	if handled, code := panelaunch.HandleInvocation(os.Args[1:]); handled {
		os.Exit(code)
	}
	os.Exit(m.Run())
}

// TestPaneLaunchAgentStartFailureDiscardsPayload proves the caller-side
// ownership rule: when NewTmuxClientWithSize fails before any child can
// start (a deleted workspace root), the prepared attempt is discarded — no
// payload is left for the expiry sweep.
func TestPaneLaunchAgentStartFailureDiscardsPayload(t *testing.T) {
	if err := tmux.EnsureAvailable(); err != nil {
		t.Skipf("tmux unavailable: %v", err)
	}
	root := t.TempDir()
	attempts := filepath.Join(root, "attempts")
	t.Setenv("TMPDIR", attempts)
	if err := os.MkdirAll(attempts, 0o700); err != nil {
		t.Fatal(err)
	}

	serverName := fmt.Sprintf("amux-ptytest-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", serverName, "kill-server").Run()
	})
	m := NewAgentManager(testConfig())
	m.SetTmuxOptions(tmux.Options{
		ServerName:     serverName,
		ConfigPath:     "/dev/null",
		CommandTimeout: 5 * time.Second,
	})

	// A missing workspace root fails PTY start synchronously after prepare.
	ws := &data.Workspace{
		Name: "gone-ws",
		Root: filepath.Join(t.TempDir(), "deleted"),
		Repo: "/tmp/test-repo",
	}
	if _, err := m.CreateViewer(ws, "sleep 1", "dead-on-arrival", 24, 80); err == nil {
		t.Fatal("CreateViewer succeeded on a missing workspace root")
	}
	entries, err := os.ReadDir(attempts)
	if err != nil {
		t.Fatalf("list attempt root: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "amux-pane-launch-") {
			t.Fatalf("start failure left launch attempt %s", e.Name())
		}
	}
}

// TestPaneLaunchViewerDeliversEnv exercises the viewer shape end-to-end:
// env values reach the pane through the payload, not argv.
func TestPaneLaunchViewerDeliversEnv(t *testing.T) {
	if err := tmux.EnsureAvailable(); err != nil {
		t.Skipf("tmux unavailable: %v", err)
	}
	serverName := fmt.Sprintf("amux-ptytest-%d", time.Now().UnixNano())
	opts := tmux.Options{
		ServerName:     serverName,
		ConfigPath:     "/dev/null",
		CommandTimeout: 5 * time.Second,
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", serverName, "kill-server").Run()
	})
	m := NewAgentManager(testConfig())
	m.SetTmuxOptions(opts)

	outFile := filepath.Join(t.TempDir(), "env.out")
	ws := &data.Workspace{Name: "viewer-ws", Root: t.TempDir(), Repo: "/tmp/r"}
	sessionName := fmt.Sprintf("amux-test-viewer-%d", time.Now().UnixNano())
	agent, err := m.CreateViewer(ws,
		fmt.Sprintf("env > '%s'; sleep 60", outFile),
		sessionName, 24, 80)
	if err != nil {
		t.Fatalf("CreateViewer failed: %v", err)
	}
	t.Cleanup(func() { _ = m.CloseAgent(agent) })

	// The outer argv must not carry env values — the payload delivers them.
	cmdStr := strings.Join(agent.Terminal.cmd.Args, " ")
	if strings.Contains(cmdStr, "WORKSPACE_ROOT=") {
		t.Fatal("outer command argv contains env assignments")
	}

	waitForPTYTestSession(t, sessionName, opts)
	deadline := time.Now().Add(ptyTestTimeout)
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(outFile)
		if s := string(raw); s != "" {
			if !strings.Contains(s, "WORKSPACE_ROOT="+ws.Root) {
				t.Fatal("pane env missing WORKSPACE_ROOT")
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("pane never wrote env output")
}
