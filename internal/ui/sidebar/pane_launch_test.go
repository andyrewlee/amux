package sidebar

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/panelaunch"
	"github.com/andyrewlee/amux/internal/pty"
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

// TestPaneLaunchSidebarEnvConfidentiality proves the sidebar caller renders
// the private invocation (never env values) into the PTY command string,
// and that a pre-start failure discards the prepared attempt so no payload
// is left for the expiry sweep.
func TestPaneLaunchSidebarEnvConfidentiality(t *testing.T) {
	oldNewPTY, oldEnsure, oldState := newPTYWithSizeFn, ensureTmuxAvailableFn, sessionStateForFn
	t.Cleanup(func() {
		newPTYWithSizeFn = oldNewPTY
		ensureTmuxAvailableFn = oldEnsure
		sessionStateForFn = oldState
	})

	attempts := t.TempDir()
	t.Setenv("TMPDIR", attempts)
	t.Setenv("SHELL", "/bin/sh")

	const marker = "sidebar-env-marker-3b7"
	ensureTmuxAvailableFn = func() error { return nil }
	sessionStateForFn = func(string, tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{}, errors.New("no server")
	}

	var gotCommand string
	newPTYWithSizeFn = func(command, dir string, env []string, rows, cols uint16) (*pty.Terminal, error) {
		gotCommand = command
		return nil, errors.New("pty start refused")
	}

	m := NewTerminalModel()
	m.sessionEnvProvider = func(ws *data.Workspace) ([]string, error) {
		return []string{"SIDEBAR_SECRET=" + marker}, nil
	}
	ws := data.NewWorkspace("ws", "main", "main", t.TempDir(), t.TempDir())

	msg := m.createTerminalTab(ws)()
	if _, ok := msg.(SidebarTerminalCreateFailed); !ok {
		t.Fatalf("expected SidebarTerminalCreateFailed, got %T", msg)
	}
	if !strings.Contains(gotCommand, "--internal-pane-launch") {
		t.Fatalf("rendered command lacks private invocation: %q", gotCommand)
	}
	if strings.Contains(gotCommand, marker) || strings.Contains(gotCommand, "SIDEBAR_SECRET") {
		t.Fatalf("rendered command leaked env material: %q", gotCommand)
	}
	entries, err := os.ReadDir(attempts)
	if err != nil {
		t.Fatalf("list attempt root: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "amux-pane-launch-") {
			t.Fatalf("pre-start failure left launch attempt %s", filepath.Join(attempts, e.Name()))
		}
	}
}
