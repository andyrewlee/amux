//go:build !windows

package workspacesvc

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/testutil"
)

// lifecycleTeardownFixture builds a service with a real ScriptRunner and a
// live workspace whose repo config defines a blocking setup plus an archive
// marker. The returned paths let tests prove teardown ordering on disk.
type lifecycleTeardownFixture struct {
	svc     *Service
	scripts *process.ScriptRunner
	project *data.Project
	ws      *data.Workspace
	repo    string
	tmp     string
}

func newLifecycleTeardownFixture(t *testing.T, setupJSON string) *lifecycleTeardownFixture {
	t.Helper()
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "managed-workspaces")
	repo := filepath.Join(tmp, "repo")
	workspacePath := filepath.Join(workspacesRoot, "repo", "feature")
	if err := os.MkdirAll(filepath.Join(repo, ".amux"), 0o755); err != nil {
		t.Fatalf("MkdirAll(repo/.amux): %v", err)
	}
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath): %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".amux", "workspaces.json"), []byte(setupJSON), 0o644); err != nil {
		t.Fatalf("WriteFile(workspaces.json): %v", err)
	}
	scripts := process.NewScriptRunner(16200, 10)
	t.Cleanup(scripts.StopAll)
	if err := scripts.TrustRepoScripts(repo); err != nil {
		t.Fatalf("TrustRepoScripts: %v", err)
	}
	project := data.NewProject(repo)
	ws := data.NewWorkspace("feature", "feature", "main", repo, workspacePath)
	svc := New(nil, nil, scripts, workspacesRoot)
	return &lifecycleTeardownFixture{svc: svc, scripts: scripts, project: project, ws: ws, repo: repo, tmp: tmp}
}

// TestDeleteWorkspace_DrainsSetupThenArchivesThenRemoves is the service-level
// proof of the plan's ordering: an in-flight setup is killed and reaped BEFORE
// the archive hook runs, and BEFORE the worktree removal. The setup records
// its own PID; the archive asserts that PID is already dead — it runs only
// after the drain, never while setup is still writing.
func TestDeleteWorkspace_DrainsSetupThenArchivesThenRemoves(t *testing.T) {
	tmp := t.TempDir()
	pidFile := filepath.Join(tmp, "setup.pid")
	archiveSawDead := filepath.Join(tmp, "archive-saw-dead")
	archiveSawAlive := filepath.Join(tmp, "archive-saw-alive")

	fx := newLifecycleTeardownFixture(t, `{
		"setup-workspace": ["echo $$ > `+pidFile+`; sleep 30"],
		"archive": "if kill -0 $(cat `+pidFile+`) 2>/dev/null; then touch `+archiveSawAlive+`; else touch `+archiveSawDead+`; fi"
	}`)

	setupDone := make(chan error, 1)
	go func() { setupDone <- fx.scripts.RunSetup(fx.ws) }()
	testutil.Eventually(t, 3*time.Second, 10*time.Millisecond, func() bool {
		_, err := os.Stat(pidFile)
		return err == nil
	}, "setup never wrote its pid before delete")

	removeObservedLiveScript := false
	fx.svc.gitOps = &testutil.FakeGitOps{
		RemoveWorkspaceFunc: func(_, _ string) error {
			if fx.scripts.IsRunning(fx.ws) {
				removeObservedLiveScript = true
			}
			return nil
		},
	}
	fx.svc.killWorkspaceSessions = func(string) error { return nil }

	msg := fx.svc.DeleteWorkspace(fx.project, fx.ws)()
	if _, ok := msg.(messages.WorkspaceDeleted); !ok {
		t.Fatalf("expected WorkspaceDeleted, got %T", msg)
	}

	select {
	case err := <-setupDone:
		if err == nil {
			t.Fatal("canceled setup returned nil error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("in-flight setup never exited after delete")
	}
	if _, err := os.Stat(archiveSawDead); err != nil {
		if _, aliveErr := os.Stat(archiveSawAlive); aliveErr == nil {
			t.Fatal("archive observed the in-flight setup still alive — drain did not precede it")
		}
		t.Fatalf("archive hook never ran: %v", err)
	}
	if removeObservedLiveScript {
		t.Fatal("worktree removal observed live lifecycle work")
	}
}

// TestShelveWorkspace_DrainsSetupBeforeRemovingTree is the shelve twin of the
// delete drain: shelving removes the worktree too, so an in-flight setup must
// be dead before RemoveWorkspace runs.
func TestShelveWorkspace_DrainsSetupBeforeRemovingTree(t *testing.T) {
	tmp := t.TempDir()
	managed := filepath.Join(tmp, "managed-workspaces")
	repo := filepath.Join(tmp, "repo")
	workspacePath := filepath.Join(managed, "repo", "feat")
	for _, dir := range []string{filepath.Join(repo, ".amux"), workspacePath} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, ".amux", "workspaces.json"),
		[]byte(`{"setup-workspace": ["sleep 30"]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	store := data.NewWorkspaceStore(filepath.Join(tmp, "metadata"))
	project := data.NewProject(repo)
	ws := data.NewWorkspace("feat", "feat", "main", repo, workspacePath)
	if err := store.Save(ws); err != nil {
		t.Fatalf("seed Save: %v", err)
	}
	scripts := process.NewScriptRunner(16210, 10)
	t.Cleanup(scripts.StopAll)
	if err := scripts.TrustRepoScripts(repo); err != nil {
		t.Fatalf("TrustRepoScripts: %v", err)
	}

	setupDone := make(chan error, 1)
	go func() { setupDone <- scripts.RunSetup(ws) }()
	testutil.Eventually(t, 3*time.Second, 10*time.Millisecond, func() bool {
		return scripts.IsRunning(ws)
	}, "setup never became live before shelve")

	removeObservedLive := false
	svc := New(nil, store, scripts, managed)
	svc.gitOps = &testutil.FakeGitOps{
		RemoveWorkspaceFunc: func(_, _ string) error {
			if scripts.IsRunning(ws) {
				removeObservedLive = true
			}
			return nil
		},
	}
	svc.killWorkspaceSessions = func(string) error { return nil }

	msg := svc.ShelveWorkspace(project, ws)()
	if _, ok := msg.(messages.WorkspaceShelved); !ok {
		t.Fatalf("expected WorkspaceShelved, got %T (%v)", msg, msg)
	}
	select {
	case <-setupDone:
	case <-time.After(3 * time.Second):
		t.Fatal("in-flight setup never exited after shelve")
	}
	if removeObservedLive {
		t.Fatal("shelve removed the worktree while setup was still running")
	}
}

// TestDeleteWorkspace_DrainsOnDoneBeforeRemoval covers the detached-hook leg:
// a fire-and-forget on-done process in flight when delete begins must also be
// reaped before the tree goes.
func TestDeleteWorkspace_DrainsOnDoneBeforeRemoval(t *testing.T) {
	tmp := t.TempDir()
	hookReady := filepath.Join(tmp, "hook-started")

	fx := newLifecycleTeardownFixture(t, `{}`)
	fx.ws.Scripts.OnDone = "touch " + hookReady + "; sleep 30"

	if err := fx.scripts.RunOnDone(fx.ws, "amux-x-tab-1"); err != nil {
		t.Fatalf("RunOnDone: %v", err)
	}
	testutil.Eventually(t, 3*time.Second, 10*time.Millisecond, func() bool {
		_, err := os.Stat(hookReady)
		return err == nil
	}, "on-done hook never started")

	fx.svc.gitOps = &testutil.FakeGitOps{}
	fx.svc.killWorkspaceSessions = func(string) error { return nil }

	msg := fx.svc.DeleteWorkspace(fx.project, fx.ws)()
	if _, ok := msg.(messages.WorkspaceDeleted); !ok {
		t.Fatalf("expected WorkspaceDeleted, got %T", msg)
	}
	if fx.scripts.IsRunning(fx.ws) {
		t.Fatal("IsRunning() = true after delete drained the on-done hook")
	}
}
