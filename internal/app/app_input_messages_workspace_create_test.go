package app

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/testutil"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

func TestHandleCreateWorkspaceSkipsPendingTrackingWithoutService(t *testing.T) {
	app := &App{
		dashboard: dashboard.New(),
		lifecycle: workspaceLifecycleState{
			phases: make(map[string]lifecyclePhase),
		},
		// workspaceService intentionally nil
	}

	project := data.NewProject("/tmp/repo")
	msg := messages.CreateWorkspace{
		Project:   project,
		Name:      "feature",
		Base:      "main",
		Assistant: "claude",
	}

	cmds := app.handleCreateWorkspace(msg)
	// Should not panic and should not track any pending IDs
	if len(app.lifecycle.snapshotCreating()) != 0 {
		t.Fatalf("expected no pending IDs without workspace service, got %d", len(app.lifecycle.snapshotCreating()))
	}
	// Should still return the createWorkspace cmd (which will be nil since service is nil)
	_ = cmds
}

func TestHandleCreateWorkspaceTracksAndClearsPendingIDOnFailure(t *testing.T) {
	gitErr := errors.New("git worktree add failed")

	workspacesRoot := "/tmp/workspaces"
	store := data.NewWorkspaceStore(t.TempDir())
	svc := workspacesvc.New(nil, store, nil, workspacesRoot)
	svc.Configure(workspacesvc.Deps{
		GitPathWaitTimeout: 50 * time.Millisecond,
		GitOps: &testutil.FakeGitOps{
			CreateWorkspaceFunc: func(repoPath, workspacePath, branch, base string) (bool, error) {
				return false, gitErr
			},
		},
	})

	app := &App{
		dashboard: dashboard.New(),
		lifecycle: workspaceLifecycleState{
			phases: make(map[string]lifecyclePhase),
		},
		workspaceService: svc,
	}

	project := data.NewProject("/tmp/repo")
	msg := messages.CreateWorkspace{
		Project:   project,
		Name:      "feature",
		Base:      "main",
		Assistant: "claude",
	}

	// Step 1: handleCreateWorkspace should track the pending ID
	cmds := app.handleCreateWorkspace(msg)
	if len(app.lifecycle.snapshotCreating()) != 1 {
		t.Fatalf("expected 1 pending ID after handleCreateWorkspace, got %d", len(app.lifecycle.snapshotCreating()))
	}

	// Capture the tracked ID
	var trackedID string
	for id := range app.lifecycle.snapshotCreating() {
		trackedID = id
	}

	// Verify tracked ID matches expected path
	expectedPath := filepath.Join(workspacesRoot, project.Name, "feature")
	pending := svc.PendingWorkspace(project, "feature", "main")
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
			if len(app.lifecycle.snapshotCreating()) != 0 {
				t.Fatalf("expected 0 pending IDs after failure, got %d", len(app.lifecycle.snapshotCreating()))
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
	svc := workspacesvc.New(nil, store, nil, workspacesRoot)

	app := &App{
		dashboard: dashboard.New(),
		lifecycle: workspaceLifecycleState{
			phases: make(map[string]lifecyclePhase),
		},
		workspaceService: svc,
	}

	project := data.NewProject("/tmp/repo")
	msg := messages.CreateWorkspace{
		Project:   project,
		Name:      "bad/name",
		Base:      "main",
		Assistant: "claude",
	}

	cmds := app.handleCreateWorkspace(msg)
	if len(app.lifecycle.snapshotCreating()) != 1 {
		t.Fatalf("expected 1 pending ID after handleCreateWorkspace, got %d", len(app.lifecycle.snapshotCreating()))
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
		if len(app.lifecycle.snapshotCreating()) != 0 {
			t.Fatalf("expected 0 pending IDs after validation failure, got %d", len(app.lifecycle.snapshotCreating()))
		}
		return
	}
	t.Fatal("expected at least one cmd to produce WorkspaceCreateFailed")
}

// TestHandleCreateWorkspaceRejectsDuplicateAdmission asserts a second create
// dispatched while the first is still in flight is refused as "already in
// progress" — not admitted as a concurrent worktree add at the same root.
func TestHandleCreateWorkspaceRejectsDuplicateAdmission(t *testing.T) {
	workspacesRoot := "/tmp/workspaces"
	store := data.NewWorkspaceStore(t.TempDir())
	svc := workspacesvc.New(nil, store, nil, workspacesRoot)

	app := &App{
		dashboard: dashboard.New(),
		lifecycle: workspaceLifecycleState{
			phases: make(map[string]lifecyclePhase),
		},
		workspaceService: svc,
	}

	project := data.NewProject("/tmp/repo")
	msg := messages.CreateWorkspace{
		Project:   project,
		Name:      "feature",
		Base:      "main",
		Assistant: "claude",
	}

	// First dispatch is admitted and stays in flight (its cmd is not run).
	app.handleCreateWorkspace(msg)
	if len(app.lifecycle.snapshotCreating()) != 1 {
		t.Fatalf("expected 1 create in flight after first dispatch, got %d",
			len(app.lifecycle.snapshotCreating()))
	}

	// Second dispatch for the same workspace is refused, not queued.
	var refusal *messages.WorkspaceCreateFailed
	for _, cmd := range app.handleCreateWorkspace(msg) {
		if cmd == nil {
			continue
		}
		if failed, ok := cmd().(messages.WorkspaceCreateFailed); ok {
			failed := failed
			refusal = &failed
		}
	}
	if refusal == nil {
		t.Fatal("expected WorkspaceCreateFailed for the duplicate dispatch")
	}
	if !strings.Contains(refusal.Err.Error(), "already in progress") {
		t.Fatalf("expected an in-progress refusal, got %v", refusal.Err)
	}
	if len(app.lifecycle.snapshotCreating()) != 1 {
		t.Fatal("refused duplicate entered the creating phase")
	}
}
