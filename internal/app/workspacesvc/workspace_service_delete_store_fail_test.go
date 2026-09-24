package workspacesvc

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/testutil"
)

// TestDeleteWorkspace_StoreDeleteFailureReportsPartialSuccess proves a
// metadata-delete failure is reported without using the generic failed-delete
// path. At this point the worktree and sessions are already gone, so the app
// must still run WorkspaceDeleted cleanup and then surface the metadata error.
func TestDeleteWorkspace_StoreDeleteFailureReportsPartialSuccess(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "managed-workspaces")
	projectPath := filepath.Join(tmp, "repo")
	workspacePath := filepath.Join(workspacesRoot, "repo", "feature")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}

	mock := &testutil.FakeGitOps{
		RemoveWorkspaceFunc: func(repoPath, workspacePath string) error { return nil },
	}
	store := &testutil.FakeWorkspaceStore{DeleteFunc: func(data.WorkspaceID) error { return errors.New("metadata delete boom") }}

	svc := New(nil, store, nil, workspacesRoot)
	svc.gitOps = mock

	project := data.NewProject(projectPath)
	ws := data.NewWorkspace("feature", "feature", "main", projectPath, workspacePath)

	msg := svc.DeleteWorkspace(project, ws)()
	deleted, ok := msg.(messages.WorkspaceDeleted)
	if !ok {
		t.Fatalf("expected WorkspaceDeleted when only store.Delete fails, got %T", msg)
	}
	if deleted.Err == nil {
		t.Fatal("expected the store.Delete error to be preserved")
	}
	if store.LastSaved() == nil || !store.LastSaved().Archived {
		t.Fatalf("expected surviving metadata to be archived, got %+v", store.LastSaved())
	}
	if store.LastSaved().ArchivedAt.IsZero() {
		t.Fatal("expected archived metadata to set ArchivedAt")
	}
}

func TestDeleteWorkspace_StoreDeleteAndArchiveFailureReportsFailure(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "managed-workspaces")
	projectPath := filepath.Join(tmp, "repo")
	workspacePath := filepath.Join(workspacesRoot, "repo", "feature")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}

	mock := &testutil.FakeGitOps{
		RemoveWorkspaceFunc: func(repoPath, workspacePath string) error { return nil },
	}
	store := &testutil.FakeWorkspaceStore{
		DeleteFunc: func(data.WorkspaceID) error { return errors.New("metadata delete boom") },
		SaveFunc:   func(*data.Workspace) error { return errors.New("metadata archive boom") },
	}

	svc := New(nil, store, nil, workspacesRoot)
	svc.gitOps = mock

	project := data.NewProject(projectPath)
	ws := data.NewWorkspace("feature", "feature", "main", projectPath, workspacePath)

	msg := svc.DeleteWorkspace(project, ws)()
	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed when metadata delete and archive both fail, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatal("expected joined metadata errors to be preserved")
	}
	if store.LastSaved() == nil || !store.LastSaved().Archived {
		t.Fatalf("expected archive fallback to be attempted, got %+v", store.LastSaved())
	}
}

// TestDeleteWorkspace_BranchDeleteFailureFailsDelete proves a failed branch
// delete is treated as an incomplete delete: the delete fails (not silently
// swallowed as a warning), so the tombstone and metadata survive for
// finishInterruptedDelete to retry the branch deletion.
func TestDeleteWorkspace_BranchDeleteFailureFailsDelete(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "managed-workspaces")
	projectPath := filepath.Join(tmp, "repo")
	workspacePath := filepath.Join(workspacesRoot, "repo", "feature")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}

	mock := &testutil.FakeGitOps{
		RemoveWorkspaceFunc: func(repoPath, workspacePath string) error { return nil },
		DeleteBranchFunc:    func(repoPath, branch string) error { return errors.New("branch checked out elsewhere") },
	}
	svc := New(nil, nil, nil, workspacesRoot)
	svc.gitOps = mock

	project := data.NewProject(projectPath)
	ws := data.NewWorkspace("feature", "feature", "main", projectPath, workspacePath)

	msg := svc.DeleteWorkspace(project, ws)()
	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed when branch delete fails, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatal("expected a non-nil error when branch delete fails")
	}
}
