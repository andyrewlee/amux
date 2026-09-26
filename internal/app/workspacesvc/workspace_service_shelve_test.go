package workspacesvc

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/testutil"
)

// newShelveHarness builds a service backed by a real store and a mock git
// layer, with one live workspace saved inside the managed root.
func newShelveHarness(t *testing.T, mock *testutil.FakeGitOps) (*Service, *data.Project, *data.Workspace, *data.WorkspaceStore) {
	t.Helper()
	repo := t.TempDir()
	managed := t.TempDir()
	store := data.NewWorkspaceStore(t.TempDir())
	project := &data.Project{Name: "repo", Path: repo}
	ws := data.NewWorkspace("feat", "feat", "main", repo, filepath.Join(managed, "repo", "feat"))
	if err := store.Save(ws); err != nil {
		t.Fatalf("seed Save() error = %v", err)
	}
	svc := New(nil, store, nil, managed)
	svc.gitOps = mock
	return svc, project, ws, store
}

func TestShelveWorkspace_KeepsBranchAndMetadata(t *testing.T) {
	var removed, branchDeleted bool
	mock := &testutil.FakeGitOps{
		RemoveWorkspaceFunc: func(_, _ string) error { removed = true; return nil },
		DeleteBranchFunc:    func(_, _ string) error { branchDeleted = true; return nil },
	}
	svc, project, ws, store := newShelveHarness(t, mock)
	// The worktree exists on disk (shelve removes it, so make it real).
	if err := os.MkdirAll(ws.Root, 0o755); err != nil {
		t.Fatalf("mkdir ws root: %v", err)
	}

	msg := svc.ShelveWorkspace(project, ws)()
	if _, ok := msg.(messages.WorkspaceShelved); !ok {
		t.Fatalf("expected WorkspaceShelved, got %T (%v)", msg, msg)
	}
	if !removed {
		t.Fatal("RemoveWorkspace was not called")
	}
	if branchDeleted {
		t.Fatal("DeleteBranch called — shelve must keep the branch")
	}
	loaded, err := store.Load(ws.MetadataID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded == nil {
		t.Fatal("shelve deleted the workspace metadata")
	}
	if !loaded.Archived || !loaded.Shelved {
		t.Fatalf("loaded Archived=%v Shelved=%v, want both", loaded.Archived, loaded.Shelved)
	}
	if loaded.Branch != "feat" {
		t.Fatalf("branch = %q, want feat", loaded.Branch)
	}
}

func TestShelveWorkspace_KillsSessionsBeforeReturn(t *testing.T) {
	var killed []string
	svc, project, ws, _ := newShelveHarness(t, &testutil.FakeGitOps{})
	svc.killWorkspaceSessions = func(wsID string) error {
		killed = append(killed, wsID)
		return nil
	}
	if err := os.MkdirAll(ws.Root, 0o755); err != nil {
		t.Fatalf("mkdir ws root: %v", err)
	}
	if _, ok := svc.ShelveWorkspace(project, ws)().(messages.WorkspaceShelved); !ok {
		t.Fatal("shelve failed")
	}
	if len(killed) == 0 {
		t.Fatal("shelve did not tear down workspace sessions")
	}
}

func TestRestoreWorkspace_DoesNotResurrectSessions(t *testing.T) {
	mock := &testutil.FakeGitOps{
		CreateWorkspaceFunc: func(_, workspacePath, _, _ string) error {
			return os.MkdirAll(filepath.Join(workspacePath, ".git"), 0o755)
		},
	}
	svc, project, ws, store := newShelveHarness(t, mock)
	svc.killWorkspaceSessions = func(wsID string) error {
		t.Fatalf("restore must not touch session teardown, got kill for %s", wsID)
		return nil
	}
	ws.Archived = true
	ws.Shelved = true
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if _, ok := svc.RestoreWorkspace(project, ws)().(messages.WorkspaceRestored); !ok {
		t.Fatal("restore failed")
	}
}

func TestShelveWorkspace_RejectsPrimaryAndArchived(t *testing.T) {
	svc, project, ws, _ := newShelveHarness(t, &testutil.FakeGitOps{})
	ws.Root = ws.Repo // primary checkout
	if msg := svc.ShelveWorkspace(project, ws)(); true {
		if _, ok := msg.(messages.WorkspaceShelveFailed); !ok {
			t.Fatalf("shelving primary checkout = %T, want WorkspaceShelveFailed", msg)
		}
	}
	ws2 := data.NewWorkspace("gone", "gone", "main", project.Path, "/nowhere")
	ws2.Archived = true
	if _, ok := svc.ShelveWorkspace(project, ws2)().(messages.WorkspaceShelveFailed); !ok {
		t.Fatal("shelving an already-archived workspace must fail")
	}
}

func TestShelveWorkspace_RemoveFailureRollsBackIntent(t *testing.T) {
	mock := &testutil.FakeGitOps{
		RemoveWorkspaceFunc: func(_, _ string) error { return errors.New("nope") },
	}
	svc, project, ws, store := newShelveHarness(t, mock)
	if err := os.MkdirAll(ws.Root, 0o755); err != nil {
		t.Fatalf("mkdir ws root: %v", err)
	}
	msg := svc.ShelveWorkspace(project, ws)()
	failed, ok := msg.(messages.WorkspaceShelveFailed)
	if !ok {
		t.Fatalf("expected WorkspaceShelveFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatal("expected error")
	}
	loaded, err := store.Load(ws.MetadataID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Shelved {
		t.Fatal("worktree still exists but Shelved flag was not rolled back")
	}
}

func TestRestoreWorkspace_RecreatesFromKeptBranch(t *testing.T) {
	var created struct {
		path, branch string
	}
	mock := &testutil.FakeGitOps{
		CreateWorkspaceFunc: func(_, workspacePath, branch, _ string) error {
			created.path, created.branch = workspacePath, branch
			return os.MkdirAll(filepath.Join(workspacePath, ".git"), 0o755)
		},
	}
	svc, project, ws, store := newShelveHarness(t, mock)
	// Shelve it first via the store state (service op covered above).
	ws.Archived = true
	ws.Shelved = true
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	msg := svc.RestoreWorkspace(project, ws)()
	if _, ok := msg.(messages.WorkspaceRestored); !ok {
		t.Fatalf("expected WorkspaceRestored, got %T (%v)", msg, msg)
	}
	if created.branch != "feat" {
		t.Fatalf("worktree add used branch %q, want the kept branch feat", created.branch)
	}
	loaded, err := store.Load(ws.MetadataID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Archived || loaded.Shelved {
		t.Fatalf("restored record still Archived=%v Shelved=%v", loaded.Archived, loaded.Shelved)
	}
}

func TestRestoreWorkspace_SkipsWhenRecordAlreadyLive(t *testing.T) {
	// A stale duplicate restore — e.g. a second Enter racing the first
	// restore's completion — carries a snapshot that still says shelved while
	// the store record was already unarchived. The op must skip benignly
	// rather than re-running worktree add against the restored dir.
	var createCalls int
	svc, project, ws, _ := newShelveHarness(t, &testutil.FakeGitOps{
		CreateWorkspaceFunc: func(_, _, _, _ string) error {
			createCalls++
			return nil
		},
	})
	// Snapshot claims shelved; the store record stayed live.
	ws.Archived = true
	ws.Shelved = true
	msg := svc.RestoreWorkspace(project, ws)()
	if _, ok := msg.(messages.WorkspaceRestoreSkipped); !ok {
		t.Fatalf("duplicate restore of a live record = %T, want WorkspaceRestoreSkipped", msg)
	}
	if createCalls != 0 {
		t.Fatal("skipped restore must not run worktree add")
	}
}

func TestRestoreWorkspace_RejectsUnshelvedAndExistingRoot(t *testing.T) {
	svc, project, ws, store := newShelveHarness(t, &testutil.FakeGitOps{})
	if _, ok := svc.RestoreWorkspace(project, ws)().(messages.WorkspaceRestoreFailed); !ok {
		t.Fatal("restoring a live workspace must fail")
	}
	ws.Archived = true
	ws.Shelved = true
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := os.MkdirAll(ws.Root, 0o755); err != nil {
		t.Fatalf("mkdir ws root: %v", err)
	}
	msg := svc.RestoreWorkspace(project, ws)()
	failed, ok := msg.(messages.WorkspaceRestoreFailed)
	if !ok {
		t.Fatalf("restoring over an existing path = %T, want WorkspaceRestoreFailed", msg)
	}
	if failed.Err == nil || !strings.Contains(failed.Err.Error(), "already exists") {
		t.Fatalf("expected 'already exists' error, got %v", failed.Err)
	}
}

func TestShelveWorkspace_IntentWriteSetsArchivedBeforeRemoval(t *testing.T) {
	// Crash-safety: the intent write must persist Archived+Shelved together
	// BEFORE the worktree is removed — a Shelved-only record would be a ghost
	// (filtered from live rows, not on the shelf either) if the process died
	// between the two writes.
	var flagsAtRemove struct{ archived, shelved bool }
	mock := &testutil.FakeGitOps{}
	svc, project, ws, store := newShelveHarness(t, mock)
	mock.RemoveWorkspaceFunc = func(_, _ string) error {
		loaded, err := store.Load(ws.MetadataID())
		if err != nil || loaded == nil {
			t.Fatalf("Load during RemoveWorkspace: %v", err)
		}
		flagsAtRemove.archived = loaded.Archived
		flagsAtRemove.shelved = loaded.Shelved
		return nil
	}
	if err := os.MkdirAll(ws.Root, 0o755); err != nil {
		t.Fatalf("mkdir ws root: %v", err)
	}
	if _, ok := svc.ShelveWorkspace(project, ws)().(messages.WorkspaceShelved); !ok {
		t.Fatal("shelve failed")
	}
	if !flagsAtRemove.archived || !flagsAtRemove.shelved {
		t.Fatalf("at worktree removal Archived=%v Shelved=%v, want both true", flagsAtRemove.archived, flagsAtRemove.shelved)
	}
}

func TestShelveWorkspace_RemoveFailureRollsBackArchivedToo(t *testing.T) {
	svc, project, ws, store := newShelveHarness(t, &testutil.FakeGitOps{
		RemoveWorkspaceFunc: func(_, _ string) error { return errors.New("nope") },
	})
	if err := os.MkdirAll(ws.Root, 0o755); err != nil {
		t.Fatalf("mkdir ws root: %v", err)
	}
	if _, ok := svc.ShelveWorkspace(project, ws)().(messages.WorkspaceShelveFailed); !ok {
		t.Fatal("expected shelve failure")
	}
	loaded, err := store.Load(ws.MetadataID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Shelved || loaded.Archived {
		t.Fatalf("rollback left Archived=%v Shelved=%v on a live worktree", loaded.Archived, loaded.Shelved)
	}
}

func TestRestoreWorkspace_AdoptsLeftoverWorktree(t *testing.T) {
	// A restore that died after `worktree add` leaves the dir behind; the next
	// restore must adopt it rather than wedging on "already exists".
	var createCalls int
	svc, project, ws, store := newShelveHarness(t, &testutil.FakeGitOps{
		CreateWorkspaceFunc: func(_, _, _, _ string) error {
			createCalls++
			return errors.New("must not be called")
		},
	})
	ws.Archived = true
	ws.Shelved = true
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(ws.Root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir leftover worktree: %v", err)
	}
	msg := svc.RestoreWorkspace(project, ws)()
	if _, ok := msg.(messages.WorkspaceRestored); !ok {
		t.Fatalf("adopting a leftover worktree = %T (%v), want WorkspaceRestored", msg, msg)
	}
	if createCalls != 0 {
		t.Fatal("CreateWorkspace called despite adoptable existing worktree")
	}
	loaded, err := store.Load(ws.MetadataID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Archived || loaded.Shelved {
		t.Fatalf("adopted restore left Archived=%v Shelved=%v", loaded.Archived, loaded.Shelved)
	}
}

func TestRestoreWorkspace_RollsBackCreatedWorktreeOnUnarchiveFailure(t *testing.T) {
	var removed bool
	svc, project, ws, store := newShelveHarness(t, &testutil.FakeGitOps{
		CreateWorkspaceFunc: func(_, workspacePath, _, _ string) error {
			return os.MkdirAll(filepath.Join(workspacePath, ".git"), 0o755)
		},
		RemoveWorkspaceFunc: func(_, _ string) error { removed = true; return nil },
	})
	ws.Archived = true
	ws.Shelved = true
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	// Break the unarchive save by removing the record: Load returns nil.
	if err := store.Delete(ws.MetadataID()); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	msg := svc.RestoreWorkspace(project, ws)()
	if _, ok := msg.(messages.WorkspaceRestoreFailed); !ok {
		t.Fatalf("restore with broken unarchive = %T, want WorkspaceRestoreFailed", msg)
	}
	if !removed {
		t.Fatal("created worktree was not rolled back after unarchive failure")
	}
}

func TestListShelvedWorkspaces_FiltersAccidentalArchives(t *testing.T) {
	svc, project, ws, store := newShelveHarness(t, &testutil.FakeGitOps{})
	ws.Archived = true
	ws.Shelved = true
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	accidental := data.NewWorkspace("vanished", "vanished", "main", project.Path, "/elsewhere")
	accidental.Archived = true
	if err := store.Save(accidental); err != nil {
		t.Fatalf("Save(accidental) error = %v", err)
	}
	shelved := svc.listShelvedWorkspaces(nil, project.Path)
	if len(shelved) != 1 || shelved[0].Name != "feat" {
		t.Fatalf("listShelvedWorkspaces = %v, want only the shelved record", shelved)
	}
}
