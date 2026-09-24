package workspacesvc

import (
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/testutil"
)

func TestCreateWorkspaceRejectsInvalidName(t *testing.T) {
	var createCalled bool
	mock := &testutil.FakeGitOps{
		CreateWorkspaceFunc: func(repoPath, workspacePath, branch, base string) error {
			createCalled = true
			return nil
		},
	}

	project := data.NewProject("/tmp/repo")
	svc := New(nil, nil, nil, "/tmp/workspaces")
	svc.gitOps = mock
	msg := svc.CreateWorkspace(project, "bad/name", "main")()

	failed, ok := msg.(messages.WorkspaceCreateFailed)
	if !ok {
		t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
	}
	if failed.Workspace == nil {
		t.Fatal("expected pending workspace in validation failure")
	}
	if failed.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if createCalled {
		t.Fatal("CreateWorkspace should not have been called")
	}
}

func TestCreateWorkspaceRejectsInvalidBaseRef(t *testing.T) {
	for _, base := range []string{"bad ref", "--help"} {
		t.Run(base, func(t *testing.T) {
			var createCalled bool
			mock := &testutil.FakeGitOps{
				CreateWorkspaceFunc: func(repoPath, workspacePath, branch, base string) error {
					createCalled = true
					return nil
				},
			}

			project := data.NewProject("/tmp/repo")
			svc := New(nil, nil, nil, "/tmp/workspaces")
			svc.gitOps = mock
			msg := svc.CreateWorkspace(project, "feature", base)()

			failed, ok := msg.(messages.WorkspaceCreateFailed)
			if !ok {
				t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
			}
			if failed.Workspace == nil {
				t.Fatal("expected pending workspace in validation failure")
			}
			if failed.Err == nil {
				t.Fatal("expected error, got nil")
			}
			if createCalled {
				t.Fatal("CreateWorkspace should not have been called")
			}
		})
	}
}

func TestCreateWorkspaceRejectsPathOutsideManagedRoot(t *testing.T) {
	var createCalled bool
	mock := &testutil.FakeGitOps{
		CreateWorkspaceFunc: func(repoPath, workspacePath, branch, base string) error {
			createCalled = true
			return nil
		},
	}

	// Use a project name with ".." to try to escape the managed root.
	// projectNameSegment rejects ".." in the name, so isManagedWorkspacePathForProject fails.
	project := &data.Project{Name: "../escape", Path: "/tmp/repo"}
	svc := New(nil, nil, nil, "/tmp/workspaces")
	svc.gitOps = mock
	msg := svc.CreateWorkspace(project, "feature", "main")()

	failed, ok := msg.(messages.WorkspaceCreateFailed)
	if !ok {
		t.Fatalf("expected WorkspaceCreateFailed, got %T", msg)
	}
	if failed.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(failed.Err.Error(), "outside managed project root") {
		t.Fatalf("expected 'outside managed project root' error, got: %v", failed.Err)
	}
	if createCalled {
		t.Fatal("CreateWorkspace should not have been called")
	}
}
