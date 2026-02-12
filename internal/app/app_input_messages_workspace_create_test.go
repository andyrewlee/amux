package app

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

func TestHandleCreateWorkspaceSkipsPendingTrackingWithoutService(t *testing.T) {
	app := &App{
		dashboard:            dashboard.New(),
		creatingWorkspaceIDs: make(map[string]bool),
		// workspaceService intentionally nil
	}

	project := data.NewProject("/tmp/repo")
	msg := messages.CreateWorkspace{
		Project: project,
		Name:    "feature",
		Base:    "main",
	}

	cmds := app.handleCreateWorkspace(msg)
	// Should not panic and should not track any pending IDs
	if len(app.creatingWorkspaceIDs) != 0 {
		t.Fatalf("expected no pending IDs without workspace service, got %d", len(app.creatingWorkspaceIDs))
	}
	// Should still return the createWorkspace cmd (which will be nil since service is nil)
	_ = cmds
}

func TestHandleCreateWorkspaceTracksAndClearsPendingIDOnFailure(t *testing.T) {
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
	store := data.NewWorkspaceStore(t.TempDir())
	svc := newWorkspaceService(nil, store, nil, workspacesRoot)

	app := &App{
		dashboard:            dashboard.New(),
		creatingWorkspaceIDs: make(map[string]bool),
		workspaceService:     svc,
	}

	project := data.NewProject("/tmp/repo")
	msg := messages.CreateWorkspace{
		Project: project,
		Name:    "feature",
		Base:    "main",
	}

	// Step 1: handleCreateWorkspace should track the pending ID
	cmds := app.handleCreateWorkspace(msg)
	if len(app.creatingWorkspaceIDs) != 1 {
		t.Fatalf("expected 1 pending ID after handleCreateWorkspace, got %d", len(app.creatingWorkspaceIDs))
	}

	// Capture the tracked ID
	var trackedID string
	for id := range app.creatingWorkspaceIDs {
		trackedID = id
	}

	// Verify tracked ID matches expected path
	expectedPath := filepath.Join(workspacesRoot, project.Name, "feature")
	pending := svc.pendingWorkspace(project, "feature", "main")
	if pending == nil {
		t.Fatal("expected non-nil pending workspace")
	}
	if pending.Root != expectedPath {
		t.Fatalf("expected root %q, got %q", expectedPath, pending.Root)
	}
	if string(pending.ID()) != trackedID {
		t.Fatalf("tracked ID %q does not match pending workspace ID %q", trackedID, string(pending.ID()))
	}

	// Step 2: Execute the create command to get the failure
	var createCmd func() interface{ String() string }
	_ = createCmd
	// Find the non-nil cmd (createWorkspace returns a tea.Cmd)
	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		result := cmd()
		if failed, ok := result.(messages.WorkspaceCreateFailed); ok {
			// Step 3: handleWorkspaceCreateFailed should clear the pending ID
			app.handleWorkspaceCreateFailed(failed)
			if len(app.creatingWorkspaceIDs) != 0 {
				t.Fatalf("expected 0 pending IDs after failure, got %d", len(app.creatingWorkspaceIDs))
			}
			// Verify the failure workspace ID matches what was tracked
			if failed.Workspace != nil && string(failed.Workspace.ID()) != trackedID {
				t.Fatalf("failure workspace ID %q does not match tracked ID %q",
					string(failed.Workspace.ID()), trackedID)
			}
			return
		}
	}
	t.Fatal("expected at least one cmd to produce WorkspaceCreateFailed")
}

func TestHandleCreateWorkspaceClearsPendingIDOnValidationFailure(t *testing.T) {
	workspacesRoot := "/tmp/workspaces"
	store := data.NewWorkspaceStore(t.TempDir())
	svc := newWorkspaceService(nil, store, nil, workspacesRoot)

	app := &App{
		dashboard:            dashboard.New(),
		creatingWorkspaceIDs: make(map[string]bool),
		workspaceService:     svc,
	}

	project := data.NewProject("/tmp/repo")
	msg := messages.CreateWorkspace{
		Project: project,
		Name:    "bad/name",
		Base:    "main",
	}

	cmds := app.handleCreateWorkspace(msg)
	if len(app.creatingWorkspaceIDs) != 1 {
		t.Fatalf("expected 1 pending ID after handleCreateWorkspace, got %d", len(app.creatingWorkspaceIDs))
	}

	for _, cmd := range cmds {
		if cmd == nil {
			continue
		}
		result := cmd()
		failed, ok := result.(messages.WorkspaceCreateFailed)
		if !ok {
			continue
		}
		if failed.Workspace == nil {
			t.Fatal("expected workspace in validation failure")
		}
		app.handleWorkspaceCreateFailed(failed)
		if len(app.creatingWorkspaceIDs) != 0 {
			t.Fatalf("expected 0 pending IDs after validation failure, got %d", len(app.creatingWorkspaceIDs))
		}
		return
	}
	t.Fatal("expected at least one cmd to produce WorkspaceCreateFailed")
}
