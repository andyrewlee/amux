package workspacesvc

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/testutil"
)

// TestFinishInterruptedDelete_RemovesDirlessTombstoned proves the recovery pass
// finishes a delete whose tombstone survived but whose worktree is already gone,
// removing the metadata instead of surfacing a ghost.
func TestFinishInterruptedDelete_RemovesDirlessTombstoned(t *testing.T) {
	store := data.NewWorkspaceStore(t.TempDir())
	var deletedBranch string
	svc := New(nil, store, nil, "")
	svc.gitOps = &testutil.FakeGitOps{DeleteBranchFunc: func(repoPath, branch string) error {
		deletedBranch = branch
		return nil
	}}

	// Worktree root deliberately does not exist (simulating a crash after the
	// worktree was removed but before the metadata was).
	ws := data.NewWorkspace("gone", "feature", "main", "/repo", "/repo/.amux/gone")
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.MarkDeleting(ws.ID()); err != nil {
		t.Fatalf("MarkDeleting: %v", err)
	}
	var killed []string
	svc.killWorkspaceSessions = func(id string) error {
		killed = append(killed, id)
		return nil
	}

	if !svc.finishInterruptedDelete(ws) {
		t.Fatal("expected recovery to finish the interrupted delete")
	}
	if _, err := store.Load(ws.ID()); err == nil {
		t.Fatal("expected metadata removed by recovery")
	}
	if len(killed) != 1 || killed[0] != string(ws.ID()) {
		t.Fatalf("killed sessions = %v, want [%s]", killed, ws.ID())
	}
	if deletedBranch != "feature" {
		t.Fatalf("expected recovery to delete branch feature, got %q", deletedBranch)
	}
}

// TestFinishInterruptedDelete_SurfacesWorkspaceWhenBranchDeleteFails proves
// that when recovery cannot delete the branch, the workspace is surfaced
// (returns false) rather than suppressed. This prevents a UI/disk mismatch
// where the workspace is hidden in-session but its metadata survives on disk.
func TestFinishInterruptedDelete_SurfacesWorkspaceWhenBranchDeleteFails(t *testing.T) {
	store := data.NewWorkspaceStore(t.TempDir())
	svc := New(nil, store, nil, "")
	svc.gitOps = &testutil.FakeGitOps{DeleteBranchFunc: func(string, string) error {
		return errors.New("branch locked")
	}}
	ws := data.NewWorkspace("stuck", "feature", "main", "/repo", "/repo/.amux/stuck")
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.MarkDeleting(ws.ID()); err != nil {
		t.Fatalf("MarkDeleting: %v", err)
	}
	svc.killWorkspaceSessions = func(string) error { return nil }

	if svc.finishInterruptedDelete(ws) {
		t.Fatal("expected recovery to surface the workspace when branch delete fails, not suppress it")
	}
	if _, err := store.Load(ws.ID()); err != nil {
		t.Fatalf("metadata must survive for retry: %v", err)
	}
	if !store.IsDeleting(ws.ID()) {
		t.Fatal("tombstone must remain for a later retry")
	}
}

// TestFinishInterruptedDelete_CompletesWhenBranchAlreadyGone proves that when
// the branch was already deleted (e.g. a crash after branch delete but before
// metadata removal), recovery treats the "branch not found" error as success
// and finishes the delete instead of looping forever.
func TestFinishInterruptedDelete_CompletesWhenBranchAlreadyGone(t *testing.T) {
	store := data.NewWorkspaceStore(t.TempDir())
	svc := New(nil, store, nil, "")
	svc.gitOps = &testutil.FakeGitOps{DeleteBranchFunc: func(string, string) error {
		return &git.Error{Command: "branch -D", ExitCode: 1, Stderr: "error: branch 'feature' not found", Err: errors.New("exit status 1")}
	}}
	ws := data.NewWorkspace("gone", "feature", "main", "/repo", "/repo/.amux/gone")
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.MarkDeleting(ws.ID()); err != nil {
		t.Fatalf("MarkDeleting: %v", err)
	}
	svc.killWorkspaceSessions = func(string) error { return nil }

	if !svc.finishInterruptedDelete(ws) {
		t.Fatal("expected recovery to finish when branch is already gone")
	}
	if _, err := store.Load(ws.ID()); err == nil {
		t.Fatal("expected metadata removed by recovery")
	}
}

// TestFinishInterruptedDelete_SkipsDirlessTombstonedWhenMetadataDeleteFails
// proves a transient metadata cleanup failure does not surface a ghost
// workspace once the tombstone says delete passed validation and the worktree is
// gone.
func TestFinishInterruptedDelete_SkipsDirlessTombstonedWhenMetadataDeleteFails(t *testing.T) {
	ws := data.NewWorkspace("gone", "feature", "main", "/repo", "/repo/.amux/gone")
	var markCount int
	store := &testutil.FakeWorkspaceStore{
		IsDeletingFunc:   func(id data.WorkspaceID) bool { return id == ws.ID() },
		MarkDeletingFunc: func(data.WorkspaceID) error { markCount++; return nil },
		DeleteFunc:       func(data.WorkspaceID) error { return errors.New("metadata busy") },
	}
	svc := New(nil, store, nil, "")
	svc.gitOps = &testutil.FakeGitOps{}
	var killed []string
	svc.killWorkspaceSessions = func(id string) error {
		killed = append(killed, id)
		return nil
	}

	if !svc.finishInterruptedDelete(ws) {
		t.Fatal("expected recovery to suppress a dir-less tombstoned workspace")
	}
	if markCount == 0 {
		t.Fatal("expected failed cleanup to preserve the tombstone for a later retry")
	}
	if len(killed) != 1 || killed[0] != string(ws.ID()) {
		t.Fatalf("killed sessions = %v, want [%s]", killed, ws.ID())
	}
}

func TestFinishInterruptedDelete_RetainsMetadataWhenSessionCleanupFails(t *testing.T) {
	store := data.NewWorkspaceStore(t.TempDir())
	svc := New(nil, store, nil, "")
	svc.gitOps = &testutil.FakeGitOps{}
	ws := data.NewWorkspace("gone", "feature", "main", "/repo", "/repo/.amux/gone")
	if err := store.Save(ws); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDeleting(ws.ID()); err != nil {
		t.Fatal(err)
	}
	svc.killWorkspaceSessions = func(string) error { return errors.New("tmux busy") }

	if !svc.finishInterruptedDelete(ws) {
		t.Fatal("expected recovery to suppress the dir-less tombstoned workspace")
	}
	if _, err := store.Load(ws.ID()); err != nil {
		t.Fatalf("metadata must remain for a later cleanup retry: %v", err)
	}
	if !store.IsDeleting(ws.ID()) {
		t.Fatal("delete tombstone must remain after session cleanup failure")
	}
}

func TestFinishInterruptedDelete_RemovesLegacyMetadataIDAndSessions(t *testing.T) {
	root := t.TempDir()
	store := data.NewWorkspaceStore(root)
	ws := data.NewWorkspace("gone", "feature", "main", "/repo", "/repo/.amux/gone")
	if err := store.Save(ws); err != nil {
		t.Fatal(err)
	}
	canonicalID := ws.ID()
	legacyID := data.WorkspaceID("legacy-workspace-id")
	if err := os.Rename(
		filepath.Join(root, string(canonicalID)),
		filepath.Join(root, string(legacyID)),
	); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(legacyID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.MetadataID() != legacyID {
		t.Fatalf("metadata ID = %s, want %s", loaded.MetadataID(), legacyID)
	}
	// Simulate another amux instance migrating the record while this instance
	// still holds a workspace value loaded from the legacy key.
	canonicalCopy := data.NewWorkspace(loaded.Name, loaded.Branch, loaded.Base, loaded.Repo, loaded.Root)
	canonicalCopy.OpenTabs = append([]data.TabInfo(nil), loaded.OpenTabs...)
	if err := store.Save(canonicalCopy); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDeleting(legacyID); err != nil {
		t.Fatal(err)
	}

	svc := New(nil, store, nil, "")
	svc.gitOps = &testutil.FakeGitOps{}
	var killed []string
	svc.killWorkspaceSessions = func(id string) error {
		killed = append(killed, id)
		return nil
	}
	if !svc.finishInterruptedDelete(loaded) {
		t.Fatal("expected recovery to finish legacy metadata delete")
	}
	if _, err := store.Load(legacyID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy metadata still exists: %v", err)
	}
	if _, err := store.Load(canonicalID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canonical metadata still exists: %v", err)
	}
	wantKilled := []string{string(legacyID), string(canonicalID)}
	if !reflect.DeepEqual(killed, wantKilled) {
		t.Fatalf("killed workspace IDs = %v, want %v", killed, wantKilled)
	}
}

func TestDeleteWorkspace_AbortsWhenTombstoneWriteFails(t *testing.T) {
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "workspaces", "repo", "feature")
	if err := os.MkdirAll(workspaceRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	ws := data.NewWorkspace("feature", "feature", "main", filepath.Join(root, "repo"), workspaceRoot)
	store := &testutil.FakeWorkspaceStore{
		IsDeletingFunc:   func(id data.WorkspaceID) bool { return id == ws.ID() },
		MarkDeletingFunc: func(data.WorkspaceID) error { return errors.New("metadata read-only") },
	}
	removeCalled := false
	svc := New(nil, store, nil, filepath.Join(root, "workspaces"))
	svc.gitOps = &testutil.FakeGitOps{RemoveWorkspaceFunc: func(string, string) error {
		removeCalled = true
		return nil
	}}

	msg := svc.DeleteWorkspace(data.NewProject(ws.Repo), ws)()
	if _, ok := msg.(messages.WorkspaceDeleteFailed); !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if removeCalled {
		t.Fatal("worktree removal must not begin without a durable tombstone")
	}
	if !DirExists(workspaceRoot) {
		t.Fatal("workspace root must remain after tombstone failure")
	}
}

// TestFinishInterruptedDelete_KeepsTombstonedWithLiveWorktree proves a tombstone
// whose worktree still exists (a delete that failed before removing it) is NOT
// finished — the workspace stays usable.
func TestFinishInterruptedDelete_KeepsTombstonedWithLiveWorktree(t *testing.T) {
	store := data.NewWorkspaceStore(t.TempDir())
	svc := New(nil, store, nil, "")

	wsRoot := t.TempDir() // worktree still present
	ws := data.NewWorkspace("live", "feature", "main", "/repo", wsRoot)
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.MarkDeleting(ws.ID()); err != nil {
		t.Fatalf("MarkDeleting: %v", err)
	}

	if svc.finishInterruptedDelete(ws) {
		t.Fatal("recovery must not finish a delete whose worktree still exists")
	}
	if _, err := store.Load(ws.ID()); err != nil {
		t.Fatalf("metadata must survive: %v", err)
	}
}
