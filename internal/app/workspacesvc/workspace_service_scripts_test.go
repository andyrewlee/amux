package workspacesvc

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
)

// failingKillHost is a RunSessionHost that reports a live run session but
// cannot kill it — the Stop-failure path ToggleScriptAsync must surface.
type failingKillHost struct {
	killErr error
	names   []string
}

func (f *failingKillHost) Ensure(name, workDir, cmd string, env []string, meta process.RunSessionMeta) error {
	return errors.New("ensure not needed")
}

func (f *failingKillHost) Status(string) (bool, bool, int, error) {
	return true, true, -1, nil
}
func (f *failingKillHost) Kill(string) error       { return f.killErr }
func (f *failingKillHost) Tail(string, int) string { return "" }
func (f *failingKillHost) Find(string) ([]string, error) {
	return f.names, nil
}

// failingEnsureHost reports nothing alive and fails every Ensure — the
// start-failure path.
type failingEnsureHost struct {
	ensureErr error
}

func (f *failingEnsureHost) Ensure(string, string, string, []string, process.RunSessionMeta) error {
	return f.ensureErr
}

func (f *failingEnsureHost) Status(string) (bool, bool, int, error) {
	return false, false, -1, nil
}
func (f *failingEnsureHost) Kill(string) error       { return nil }
func (f *failingEnsureHost) Tail(string, int) string { return "" }
func (f *failingEnsureHost) Find(string) ([]string, error) {
	return nil, nil
}

// scriptsFixture builds a workspace + real ScriptRunner against a throwaway
// HOME (the trust registry resolves at construction).
func scriptsFixture(t *testing.T, repoConfig string) (*Service, *data.Workspace, *process.ScriptRunner) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	wsRoot := filepath.Join(tmp, "managed", "feature")
	if err := os.MkdirAll(wsRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(wsRoot): %v", err)
	}
	if repoConfig != "" {
		configDir := filepath.Join(repo, ".amux")
		if err := os.MkdirAll(configDir, 0o755); err != nil {
			t.Fatalf("MkdirAll(.amux): %v", err)
		}
		if err := os.WriteFile(filepath.Join(configDir, "workspaces.json"), []byte(repoConfig), 0o644); err != nil {
			t.Fatalf("write workspaces.json: %v", err)
		}
	}
	scripts := process.NewScriptRunner(6700, 10)
	ws := data.NewWorkspace("feature", "feature", "main", repo, wsRoot)
	return New(nil, nil, scripts, filepath.Join(tmp, "managed")), ws, scripts
}

func TestRunSetupAsyncPropagatesError(t *testing.T) {
	svc, ws, _ := scriptsFixture(t, `{"setup-workspace":["touch should-not-run"]}`)

	msg, ok := svc.RunSetupAsync(ws)().(messages.WorkspaceSetupComplete)
	if !ok {
		t.Fatalf("expected WorkspaceSetupComplete, got %T", msg)
	}
	if msg.Err == nil {
		t.Fatal("untrusted setup script produced no error")
	}
	var trustErr *process.ScriptsNotTrustedError
	if !errors.As(msg.Err, &trustErr) {
		t.Fatalf("expected propagated ScriptsNotTrustedError, got %v", msg.Err)
	}
	if _, err := os.Stat(filepath.Join(ws.Root, "should-not-run")); !os.IsNotExist(err) {
		t.Fatal("untrusted setup command executed")
	}
}

func TestRunSetupAsyncNilScriptsCompletesWithoutError(t *testing.T) {
	svc := New(nil, nil, nil, t.TempDir())
	ws := data.NewWorkspace("w", "w", "main", "/repo", "/repo/w")
	msg, ok := svc.RunSetupAsync(ws)().(messages.WorkspaceSetupComplete)
	if !ok {
		t.Fatalf("expected WorkspaceSetupComplete, got %T", msg)
	}
	if msg.Err != nil {
		t.Fatalf("nil-scripts service reported Err=%v, want silent complete", msg.Err)
	}
}

// trustedRunFixture builds a workspace whose repo configures `run` and has
// been trusted — required before IsRunning/RunScript ever reach the host
// (runSessionsHosted skips the sweep for unconfigured, never-seen sessions).
func trustedRunFixture(t *testing.T) (*Service, *data.Workspace, *process.ScriptRunner) {
	t.Helper()
	svc, ws, scripts := scriptsFixture(t, `{"run":"sleep 60"}`)
	if err := scripts.TrustRepoScripts(ws.Repo); err != nil {
		t.Fatalf("TrustRepoScripts: %v", err)
	}
	return svc, ws, scripts
}

func TestToggleScriptAsyncFailedStopKeepsRunning(t *testing.T) {
	svc, ws, scripts := trustedRunFixture(t)
	killErr := errors.New("tmux kill-session failed")
	scripts.SetRunHost(&failingKillHost{killErr: killErr, names: []string{"amux-ws-x-run"}})

	msg, ok := svc.ToggleScriptAsync(ws)().(messages.WorkspaceScriptStateChanged)
	if !ok {
		t.Fatalf("expected WorkspaceScriptStateChanged, got %T", msg)
	}
	// The documented invariant: a failed stop leaves the workspace running,
	// and the message must say so — not optimistically report stopped.
	if !msg.Running {
		t.Fatal("failed stop reported Running=false — sidebar would lie")
	}
	if !errors.Is(msg.Err, killErr) {
		t.Fatalf("Err = %v, want the kill failure", msg.Err)
	}
}

func TestToggleScriptAsyncFailedStartReportsStopped(t *testing.T) {
	svc, ws, scripts := trustedRunFixture(t)
	ensureErr := errors.New("tmux new-session failed")
	scripts.SetRunHost(&failingEnsureHost{ensureErr: ensureErr})

	msg, ok := svc.ToggleScriptAsync(ws)().(messages.WorkspaceScriptStateChanged)
	if !ok {
		t.Fatalf("expected WorkspaceScriptStateChanged, got %T", msg)
	}
	if msg.Running {
		t.Fatal("failed start reported Running=true")
	}
	if !errors.Is(msg.Err, ensureErr) {
		t.Fatalf("Err = %v, want the ensure failure", msg.Err)
	}
}

// TestScriptAccessorsNilService asserts the nil-guard contract on every
// script-facing method — the TUI calls these unconditionally, so a service
// without a runner must degrade to documented zero values, never panic.
func TestScriptAccessorsNilService(t *testing.T) {
	var nilSvc *Service
	ws := data.NewWorkspace("w", "w", "main", "/repo", "/repo/w")

	for _, svc := range []*Service{nilSvc, New(nil, nil, nil, t.TempDir())} {
		if svc.IsScriptRunning(ws) {
			t.Fatal("IsScriptRunning on nil-scripts service = true")
		}
		if alive, exit := svc.RunScriptStatus(ws); alive || exit != -1 {
			t.Fatalf("RunScriptStatus = (%v, %d), want (false, -1)", alive, exit)
		}
		if out := svc.RunScriptOutput(ws, 10); out != "" {
			t.Fatalf("RunScriptOutput = %q, want empty", out)
		}
		if out, alive, exit := svc.RunScriptOutputAndStatus(ws, 10); out != "" || alive || exit != -1 {
			t.Fatalf("RunScriptOutputAndStatus = (%q, %v, %d), want (\"\", false, -1)", out, alive, exit)
		}
		svc.ReleaseWorkspacePort(ws) // must not panic
		if target, ok := svc.RunScriptAttachTarget(ws); ok || target != "" {
			t.Fatalf("RunScriptAttachTarget = (%q, %v), want (\"\", false)", target, ok)
		}
		if cmd := svc.ToggleScriptAsync(ws); cmd != nil {
			t.Fatal("ToggleScriptAsync on nil-scripts service returned a Cmd")
		}
	}
}
