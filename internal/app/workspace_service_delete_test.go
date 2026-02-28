package app

import (
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

// mockGitOps implements GitOperations for tests.
type mockGitOps struct {
	createWorkspace    func(repoPath, workspacePath, branch, base string) error
	removeWorkspace    func(repoPath, workspacePath string) error
	deleteBranch       func(repoPath, branch string) error
	discoverWorkspaces func(project *data.Project) ([]data.Workspace, error)
}

func (m *mockGitOps) CreateWorkspace(repoPath, workspacePath, branch, base string) error {
	if m.createWorkspace != nil {
		return m.createWorkspace(repoPath, workspacePath, branch, base)
	}
	return nil
}

func (m *mockGitOps) RemoveWorkspace(repoPath, workspacePath string) error {
	if m.removeWorkspace != nil {
		return m.removeWorkspace(repoPath, workspacePath)
	}
	return nil
}

func (m *mockGitOps) DeleteBranch(repoPath, branch string) error {
	if m.deleteBranch != nil {
		return m.deleteBranch(repoPath, branch)
	}
	return nil
}

func (m *mockGitOps) DiscoverWorkspaces(project *data.Project) ([]data.Workspace, error) {
	if m.discoverWorkspaces != nil {
		return m.discoverWorkspaces(project)
	}
	return nil, nil
}

func TestDeleteWorkspaceRejectsMissingProjectPath(t *testing.T) {
	var removeCalled bool
	mock := &mockGitOps{
		removeWorkspace: func(repoPath, workspacePath string) error {
			removeCalled = true
			return nil
		},
	}

	project := &data.Project{Name: "repo", Path: ""}
	ws := data.NewWorkspace("feature", "feature", "main", "/tmp/repo", "/tmp/workspaces/repo/feature")

	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	svc.gitOps = mock
	msg := svc.DeleteWorkspace(project, ws)()

	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(failed.Err.Error(), "project path is empty") {
		t.Fatalf("expected 'project path is empty' error, got: %v", failed.Err)
	}
	if removeCalled {
		t.Fatal("removeWorkspace should not have been called")
	}
}

func TestDeleteWorkspaceRejectsMissingWorkspaceRepo(t *testing.T) {
	var removeCalled bool
	mock := &mockGitOps{
		removeWorkspace: func(repoPath, workspacePath string) error {
			removeCalled = true
			return nil
		},
	}

	project := data.NewProject("/tmp/repo")
	ws := data.NewWorkspace("feature", "feature", "main", "", "/tmp/workspaces/repo/feature")

	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	svc.gitOps = mock
	msg := svc.DeleteWorkspace(project, ws)()

	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(failed.Err.Error(), "workspace repo is empty") {
		t.Fatalf("expected 'workspace repo is empty' error, got: %v", failed.Err)
	}
	if removeCalled {
		t.Fatal("removeWorkspace should not have been called")
	}
}

func TestDeleteWorkspaceRejectsRepoMismatch(t *testing.T) {
	var removeCalled bool
	mock := &mockGitOps{
		removeWorkspace: func(repoPath, workspacePath string) error {
			removeCalled = true
			return nil
		},
	}

	project := data.NewProject("/tmp/repo-a")
	ws := data.NewWorkspace("feature", "feature", "main", "/tmp/repo-b", "/tmp/workspaces/repo-a/feature")

	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	svc.gitOps = mock
	msg := svc.DeleteWorkspace(project, ws)()

	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(failed.Err.Error(), "does not match") {
		t.Fatalf("expected 'does not match' error, got: %v", failed.Err)
	}
	if removeCalled {
		t.Fatal("removeWorkspace should not have been called")
	}
}

func TestDeleteWorkspaceRejectsPathOutsideManagedProjectRoot(t *testing.T) {
	var removeCalled bool
	mock := &mockGitOps{
		removeWorkspace: func(repoPath, workspacePath string) error {
			removeCalled = true
			return nil
		},
	}

	project := data.NewProject("/tmp/repo")
	// Repo matches but root is outside managed project root.
	ws := data.NewWorkspace("feature", "feature", "main", "/tmp/repo", "/tmp/other/feature")

	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	svc.gitOps = mock
	msg := svc.DeleteWorkspace(project, ws)()

	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(failed.Err.Error(), "outside managed project root") {
		t.Fatalf("expected 'outside managed project root' error, got: %v", failed.Err)
	}
	if removeCalled {
		t.Fatal("removeWorkspace should not have been called")
	}
}

func TestDeleteWorkspaceAllowsManagedPathWhenProjectNameDriftsFromRepoBasename(t *testing.T) {
	var removeCalled bool
	mock := &mockGitOps{
		removeWorkspace: func(repoPath, workspacePath string) error {
			removeCalled = true
			return nil
		},
	}

	project := &data.Project{Name: "repo-link", Path: "/tmp/repo-real"}
	ws := data.NewWorkspace("cursor-blink", "cursor-blink", "main", "/tmp/repo-real", "/tmp/workspaces/repo-real/cursor-blink")

	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	svc.gitOps = mock
	msg := svc.DeleteWorkspace(project, ws)()

	if _, ok := msg.(messages.WorkspaceDeleted); !ok {
		t.Fatalf("expected WorkspaceDeleted, got %T", msg)
	}
	if !removeCalled {
		t.Fatal("removeWorkspace should have been called")
	}
}

func TestDeleteWorkspaceRejectsUnsafeProjectNameSegment(t *testing.T) {
	var removeCalled bool
	mock := &mockGitOps{
		removeWorkspace: func(repoPath, workspacePath string) error {
			removeCalled = true
			return nil
		},
	}

	project := &data.Project{Name: "../unsafe", Path: "/tmp/repo"}
	ws := data.NewWorkspace("feature", "feature", "main", "/tmp/repo", "/tmp/workspaces/../unsafe/feature")

	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	svc.gitOps = mock
	msg := svc.DeleteWorkspace(project, ws)()

	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if removeCalled {
		t.Fatal("removeWorkspace should not have been called")
	}
}

func TestDeleteWorkspaceRejectsSameNameDifferentProjectScope(t *testing.T) {
	var removeCalled bool
	mock := &mockGitOps{
		removeWorkspace: func(repoPath, workspacePath string) error {
			removeCalled = true
			return nil
		},
	}

	// Two projects both named "repo" but different paths.
	project := data.NewProject("/tmp/repo-owner-a")
	project.Name = "repo"
	ws := data.NewWorkspace("feature", "feature", "main", "/tmp/repo-owner-b", "/tmp/workspaces/repo/feature")

	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	svc.gitOps = mock
	msg := svc.DeleteWorkspace(project, ws)()

	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(failed.Err.Error(), "does not match") {
		t.Fatalf("expected repo mismatch error, got: %v", failed.Err)
	}
	if removeCalled {
		t.Fatal("removeWorkspace should not have been called")
	}
}

func TestDeleteWorkspaceRejectsAliasCollisionWhenRepoDoesNotMatch(t *testing.T) {
	var removeCalled bool
	mock := &mockGitOps{
		removeWorkspace: func(repoPath, workspacePath string) error {
			removeCalled = true
			return nil
		},
	}

	// project.Name differs from the canonical repo basename; managed roots include
	// both name-based and path-basename aliases. Repo identity must still block a
	// delete for a workspace that belongs to a different repo with the same basename.
	project := &data.Project{Name: "repo-link", Path: "/tmp/owner-a/repo-real"}
	ws := data.NewWorkspace(
		"feature",
		"feature",
		"main",
		"/tmp/owner-b/repo-real",
		"/tmp/workspaces/repo-real/feature",
	)

	svc := newWorkspaceService(nil, nil, nil, "/tmp/workspaces")
	svc.gitOps = mock
	msg := svc.DeleteWorkspace(project, ws)()

	failed, ok := msg.(messages.WorkspaceDeleteFailed)
	if !ok {
		t.Fatalf("expected WorkspaceDeleteFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(failed.Err.Error(), "does not match") {
		t.Fatalf("expected repo mismatch error, got: %v", failed.Err)
	}
	if removeCalled {
		t.Fatal("removeWorkspace should not have been called")
	}
}
