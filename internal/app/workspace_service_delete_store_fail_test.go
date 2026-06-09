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
type failingDeleteStore struct{ deleteErr error }

func (s failingDeleteStore) ListByRepo(string) ([]*data.Workspace, error) { return nil, nil }
func (s failingDeleteStore) ListByRepoIncludingArchived(string) ([]*data.Workspace, error) {
	return nil, nil
}
func (s failingDeleteStore) LoadMetadataFor(*data.Workspace) (bool, error) { return false, nil }
func (s failingDeleteStore) UpsertFromDiscovery(*data.Workspace) error     { return nil }
func (s failingDeleteStore) Save(*data.Workspace) error                    { return nil }
func (s failingDeleteStore) Delete(data.WorkspaceID) error                 { return s.deleteErr }
func (s failingDeleteStore) ResolvedDefaultAssistant() string              { return data.DefaultAssistant }

// TestDeleteWorkspace_StoreDeleteFailurePropagates proves a metadata-delete
// failure surfaces as WorkspaceDeleteFailed instead of being swallowed — without
// which the orphaned metadata would resurface the just-deleted workspace on the
// next store-first load, pointing at a missing worktree.
func TestDeleteWorkspace_StoreDeleteFailurePropagates(t *testing.T) {
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
	store := failingDeleteStore{deleteErr: errors.New("metadata delete boom")}

	svc := newWorkspaceService(nil, store, nil, workspacesRoot)
	svc.gitOps = mock

	project := data.NewProject(projectPath)
	ws := data.NewWorkspace("feature", "feature", "main", projectPath, workspacePath)

	msg := svc.DeleteWorkspace(project, ws)()
	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed when store.Delete fails, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatal("expected the store.Delete error to be preserved")
	}
}
