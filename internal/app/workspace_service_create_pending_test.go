package app

import (
	"errors"
	"os"
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
	gitErr := errors.New("git worktree add failed")

	project := data.NewProject("/tmp/repo")
	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	svc.gitOps = &mockGitOps{
		createWorkspace: func(repoPath, workspacePath, branch, base string) error {
			return gitErr
		},
	}
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
	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	svc.gitOps = &mockGitOps{
		createWorkspace: func(repoPath, workspacePath, branch, base string) error {
			return errors.New("stop")
		},
	}

	// /tmp/repo is not a real git repo, so GetBaseBranch returns an error
	// and the fallback to "HEAD" is used.
	project := data.NewProject("/tmp/repo")
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

func TestResolveBaseReturnsExplicitOverride(t *testing.T) {
	// An explicit base is returned as-is, regardless of the repo.
	got := resolveBase("/nonexistent", "feature")
	if got != "feature" {
		t.Fatalf("expected 'feature', got %q", got)
	}
}

func TestResolveBaseFallsBackToHEADForNonRepo(t *testing.T) {
	// A non-repo path can't determine a default branch; fallback is HEAD.
	got := resolveBase("/nonexistent", "")
	if got != "HEAD" {
		t.Fatalf("expected 'HEAD', got %q", got)
	}
}

func TestResolveBaseDetectsMainBranch(t *testing.T) {
	skipIfNoGit(t)

	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	// With a real repo that has a "main" branch, empty base should resolve
	// to "main" — not "HEAD".
	got := resolveBase(repo, "")
	if got != "main" {
		t.Fatalf("expected 'main', got %q", got)
	}
}

func TestCreateWorkspaceEmptyBaseResolvesToMainBranch(t *testing.T) {
	skipIfNoGit(t)

	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	// Switch to a feature branch so HEAD != main, simulating the bug
	// scenario where the user is on a different workspace branch.
	runGit(t, repo, "checkout", "-b", "openclaw")

	var capturedBase string
	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	svc.gitOps = &mockGitOps{
		createWorkspace: func(repoPath, workspacePath, branch, base string) error {
			capturedBase = base
			return errors.New("stop")
		},
	}

	project := data.NewProject(repo)
	cmd := svc.CreateWorkspace(project, "feature", "")
	cmd()

	if capturedBase != "main" {
		t.Fatalf("expected gitOps to receive base 'main', got %q", capturedBase)
	}
}

func TestCreateWorkspacePendingMatchesAppSidePath(t *testing.T) {
	gitErr := errors.New("git worktree add failed")

	workspacesRoot := "/tmp/workspaces"
	project := data.NewProject("/tmp/repo")
	svc := newWorkspaceService(nil, nil, nil, workspacesRoot)
	svc.gitPathWaitTimeout = 50 * time.Millisecond
	svc.gitOps = &mockGitOps{
		createWorkspace: func(repoPath, workspacePath, branch, base string) error {
			return gitErr
		},
	}

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
