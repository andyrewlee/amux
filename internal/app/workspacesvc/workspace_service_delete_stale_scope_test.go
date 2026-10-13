package workspacesvc

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/testutil"
)

func TestDeleteWorkspaceRejectsStaleCleanupForManagedProjectRoot(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	projectPath := filepath.Join(tmp, "repo")
	workspacePath := filepath.Join(workspacesRoot, "repo")
	if err := os.MkdirAll(filepath.Join(workspacePath, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}

	removeCalls := 0
	mock := &testutil.FakeGitOps{
		RemoveWorkspaceFunc: func(repoPath, gotWorkspacePath string) error {
			removeCalls++
			return git.ErrUnregisteredWorkspacePath
		},
	}

	project := data.NewProject(projectPath)
	ws := data.NewWorkspace("feature", "feature", "main", projectPath, workspacePath)

	svc := New(nil, nil, nil, workspacesRoot)
	svc.gitOps = mock
	msg := svc.DeleteWorkspace(project, ws)()

	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatal("expected error, got nil")
	}
	// The strict managed-child gate must reject the managed project root
	// before any git call — the path is the container of every workspace
	// for this project, never a deletable workspace itself.
	if removeCalls != 0 {
		t.Fatalf("RemoveWorkspace calls = %d, want 0 (gate must reject before git)", removeCalls)
	}
	if _, err := os.Stat(workspacePath); err != nil {
		t.Fatalf("expected managed project root to remain on disk, err=%v", err)
	}
}
