package app

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestCreateWorkspaceNilProjectReturnsFailed(t *testing.T) {
	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	cmd := svc.CreateWorkspace(nil, "feature", "main")
	msg := cmd()
	failed, ok := msg.(messages.WorkspaceCreateFailed)
	if !ok {
		t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
	}
	if failed.Workspace != nil {
		t.Fatalf("expected nil workspace for nil project, got %+v", failed.Workspace)
	}
	if failed.Err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCreateWorkspaceEmptyNameReturnsFailed(t *testing.T) {
	project := data.NewProject("/tmp/repo")
	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	cmd := svc.CreateWorkspace(project, "  ", "main")
	msg := cmd()
	failed, ok := msg.(messages.WorkspaceCreateFailed)
	if !ok {
		t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
	}
	if failed.Workspace != nil {
		t.Fatalf("expected nil workspace for empty name, got %+v", failed.Workspace)
	}
	if failed.Err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCreateWorkspaceGitFailureIncludesPendingWorkspace(t *testing.T) {
	origCreate := createWorkspaceFn
	t.Cleanup(func() { createWorkspaceFn = origCreate })

	gitErr := errors.New("git worktree add failed")
	createWorkspaceFn = func(repoPath, workspacePath, branch, base string) error {
		return gitErr
	}

	project := data.NewProject("/tmp/repo")
	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	cmd := svc.CreateWorkspace(project, "feature", "main")
	msg := cmd()
	failed, ok := msg.(messages.WorkspaceCreateFailed)
	if !ok {
		t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
	}
	if failed.Workspace == nil {
		t.Fatal("expected pending workspace in failure message")
	}
	if failed.Workspace.Name != "feature" {
		t.Fatalf("expected name 'feature', got %q", failed.Workspace.Name)
	}
	if failed.Workspace.Base != "main" {
		t.Fatalf("expected base 'main', got %q", failed.Workspace.Base)
	}
	if !errors.Is(failed.Err, gitErr) {
		t.Fatalf("expected git error, got %v", failed.Err)
	}
}

func TestCreateWorkspaceEmptyBaseDefaultsToDefaultBranch(t *testing.T) {
	origCreate := createWorkspaceFn
	t.Cleanup(func() { createWorkspaceFn = origCreate })

	createWorkspaceFn = func(repoPath, workspacePath, branch, base string) error {
		return errors.New("stop")
	}

	// /tmp/repo is not a real git repo, so GetBaseBranch returns "main"
	// but BranchExists fails, causing fallback to "HEAD".
	project := data.NewProject("/tmp/repo")
	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	cmd := svc.CreateWorkspace(project, "feature", "")
	msg := cmd()
	failed, ok := msg.(messages.WorkspaceCreateFailed)
	if !ok {
		t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
	}
	if failed.Workspace == nil {
		t.Fatal("expected pending workspace")
	}
	if failed.Workspace.Base != "HEAD" {
		t.Fatalf("expected base 'HEAD', got %q", failed.Workspace.Base)
	}
}

func TestCreateWorkspacePendingMatchesAppSidePath(t *testing.T) {
	origCreate := createWorkspaceFn
	origTimeout := gitPathWaitTimeout
	t.Cleanup(func() {
		createWorkspaceFn = origCreate
		gitPathWaitTimeout = origTimeout
	})

	gitErr := errors.New("git worktree add failed")
	createWorkspaceFn = func(repoPath, workspacePath, branch, base string) error {
		return gitErr
	}
	gitPathWaitTimeout = 50 * time.Millisecond

	workspacesRoot := "/tmp/workspaces"
	project := data.NewProject("/tmp/repo")
	svc := newWorkspaceService(nil, nil, nil, workspacesRoot)

	// Get the pending workspace the app side would use
	pending := svc.pendingWorkspace(project, "feature", "main")
	if pending == nil {
		t.Fatal("expected non-nil pending workspace")
	}

	// Run CreateWorkspace and get the failure message
	cmd := svc.CreateWorkspace(project, "feature", "main")
	msg := cmd()
	failed, ok := msg.(messages.WorkspaceCreateFailed)
	if !ok {
		t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
	}
	if failed.Workspace == nil {
		t.Fatal("expected workspace in failure")
	}

	// Core identity consistency: IDs must match
	if failed.Workspace.ID() != pending.ID() {
		t.Fatalf("workspace ID mismatch: service=%s pending=%s", failed.Workspace.ID(), pending.ID())
	}

	// Verify the path is constructed consistently
	expectedPath := filepath.Join(workspacesRoot, project.Name, "feature")
	if failed.Workspace.Root != expectedPath {
		t.Fatalf("expected root %q, got %q", expectedPath, failed.Workspace.Root)
	}
	if pending.Root != expectedPath {
		t.Fatalf("expected pending root %q, got %q", expectedPath, pending.Root)
	}
}
