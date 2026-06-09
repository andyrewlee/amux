package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

// failingDeleteStore is a WorkspaceStore whose Delete fails; everything else is
// a benign no-op so DeleteWorkspace reaches the metadata-delete step.
type failingDeleteStore struct {
	deleteErr error
	saveErr   error
	saved     *data.Workspace
}

func (s *failingDeleteStore) ListByRepo(string) ([]*data.Workspace, error) { return nil, nil }
func (s *failingDeleteStore) ListByRepoIncludingArchived(string) ([]*data.Workspace, error) {
	return nil, nil
}
func (s *failingDeleteStore) LoadMetadataFor(*data.Workspace) (bool, error) { return false, nil }
func (s *failingDeleteStore) UpsertFromDiscovery(*data.Workspace) error     { return nil }
func (s *failingDeleteStore) Save(ws *data.Workspace) error {
	cp := *ws
	s.saved = &cp
	return s.saveErr
}
func (s *failingDeleteStore) Delete(data.WorkspaceID) error    { return s.deleteErr }
func (s *failingDeleteStore) ResolvedDefaultAssistant() string { return data.DefaultAssistant }

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

	mock := &mockGitOps{
		removeWorkspace: func(repoPath, workspacePath string) error { return nil },
	}
	store := &failingDeleteStore{deleteErr: errors.New("metadata delete boom")}

	svc := newWorkspaceService(nil, store, nil, workspacesRoot)
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
	if store.saved == nil || !store.saved.Archived {
		t.Fatalf("expected surviving metadata to be archived, got %+v", store.saved)
	}
	if store.saved.ArchivedAt.IsZero() {
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

	mock := &mockGitOps{
		removeWorkspace: func(repoPath, workspacePath string) error { return nil },
	}
	store := &failingDeleteStore{
		deleteErr: errors.New("metadata delete boom"),
		saveErr:   errors.New("metadata archive boom"),
	}

	svc := newWorkspaceService(nil, store, nil, workspacesRoot)
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
	if store.saved == nil || !store.saved.Archived {
		t.Fatalf("expected archive fallback to be attempted, got %+v", store.saved)
	}
}
