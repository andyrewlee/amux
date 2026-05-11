package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
)

func TestRollbackWorkspaceCreationCleansRecoverableUnregisteredWorkspace(t *testing.T) {
	tmp := t.TempDir()
	workspacesRoot := filepath.Join(tmp, "workspaces")
	projectPath := filepath.Join(tmp, "repo-real")
	workspacePath := filepath.Join(workspacesRoot, "repo-real", "feature")
	if err := os.MkdirAll(filepath.Join(workspacePath, "nested"), 0o755); err != nil {
		t.Fatalf("MkdirAll(workspacePath) error = %v", err)
	}

	var deleteBranchCalls int
	mock := &mockGitOps{
		removeWorkspace: func(repoPath, gotWorkspacePath string) error {
			if repoPath != projectPath {
				t.Fatalf("repoPath = %q, want %q", repoPath, projectPath)
			}
			if gotWorkspacePath != workspacePath {
				t.Fatalf("workspacePath = %q, want %q", gotWorkspacePath, workspacePath)
			}
			return git.ErrUnregisteredWorkspacePath
		},
		deleteBranch: func(repoPath, branch string) error {
			deleteBranchCalls++
			if repoPath != projectPath {
				t.Fatalf("deleteBranch repoPath = %q, want %q", repoPath, projectPath)
			}
			if branch != "feature" {
				t.Fatalf("branch = %q, want %q", branch, "feature")
			}
			return nil
		},
	}

	project := &data.Project{Name: "repo-link", Path: projectPath}
	rollbackWorkspaceCreation(mock, workspacesRoot, project, projectPath, workspacePath, "feature")

	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Fatalf("expected stale workspace path to be removed, err=%v", err)
	}
	if deleteBranchCalls != 1 {
		t.Fatalf("deleteBranch calls = %d, want 1", deleteBranchCalls)
	}
}
