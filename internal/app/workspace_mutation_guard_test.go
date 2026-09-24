package app

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/testutil"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

func TestMarkWorkspaceMutationInFlightPreservesDirtyState(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	wsID := string(ws.ID())

	app := &App{
		lifecycle: workspaceLifecycleState{
			dirty:  map[string]bool{wsID: true},
			phases: make(map[string]lifecyclePhase),
		},
	}

	app.markWorkspaceMutationInFlight(ws, true)

	if !app.lifecycle.dirty[wsID] {
		t.Fatal("expected dirty workspace marker to be preserved when delete starts")
	}
	if !app.isWorkspaceMutationInFlight(wsID) {
		t.Fatal("expected workspace to be marked mutation-in-flight")
	}
}

func TestHandleWorkspaceDeleteFailedRequeuesWorkspacePersistence(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	wsID := string(ws.ID())
	project := data.NewProject("/repo")
	project.Workspaces = []data.Workspace{*ws}

	app := &App{
		dashboard:        dashboard.New(),
		workspaceService: workspacesvc.New(&testutil.FakeProjectRegistry{}, nil, nil, ""),
		lifecycle: workspaceLifecycleState{
			dirty:  make(map[string]bool),
			phases: map[string]lifecyclePhase{wsID: lifecycleMutating},
		},
	}

	app.handleProjectsLoaded(messages.ProjectsLoaded{Projects: []data.Project{*project}})
	if len(app.projects) != 1 || len(app.projects[0].Workspaces) != 0 {
		t.Fatalf("expected in-flight delete reload to hide workspace, got %+v", app.projects)
	}

	cmd := app.handleWorkspaceDeleteFailed(messages.WorkspaceDeleteFailed{
		Workspace: ws,
		Err:       errors.New("delete failed"),
	})
	if cmd == nil {
		t.Fatal("expected non-nil command for delete failure handling")
	}
	if app.isWorkspaceMutationInFlight(wsID) {
		t.Fatal("expected mutation-in-flight marker to be cleared on delete failure")
	}
	if !app.lifecycle.dirty[wsID] {
		t.Fatal("expected workspace persistence to be re-queued on delete failure")
	}
	if app.lifecycle.projectsLoadToken == 0 {
		t.Fatal("expected delete failure to schedule a project reload")
	}

	app.handleProjectsLoaded(messages.ProjectsLoaded{
		Projects:  []data.Project{*project},
		LoadToken: int(app.lifecycle.projectsLoadToken),
	})
	if len(app.projects) != 1 || len(app.projects[0].Workspaces) != 1 {
		t.Fatalf("expected reload after failed delete to restore workspace, got %+v", app.projects)
	}
}

func TestWorkspaceMutationInFlightConcurrentAccess(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	wsID := string(ws.ID())

	app := &App{
		lifecycle: workspaceLifecycleState{
			phases: make(map[string]lifecyclePhase),
		},
	}

	const goroutines = 8
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 2000; j++ {
				if j%2 == 0 {
					app.markWorkspaceMutationInFlight(ws, true)
				} else {
					app.markWorkspaceMutationInFlight(ws, false)
				}
				if idx%2 == 0 {
					_ = app.isWorkspaceMutationInFlight(wsID)
				}
			}
		}(i)
	}

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 2000; j++ {
				_ = app.isWorkspaceMutationInFlight(wsID)
			}
		}()
	}

	wg.Wait()
}

func TestRunUnlessWorkspaceMutationInFlightSkipsWhenMutating(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	wsID := string(ws.ID())
	app := &App{lifecycle: workspaceLifecycleState{phases: map[string]lifecyclePhase{wsID: lifecycleMutating}}}

	ran := false
	ok := app.runUnlessWorkspaceMutationInFlight(wsID, func() {
		ran = true
	})
	if ok {
		t.Fatal("expected guard to skip callback when workspace is mutation-in-flight")
	}
	if ran {
		t.Fatal("callback should not have run while workspace is mutation-in-flight")
	}
}

func TestRunUnlessWorkspaceMutationInFlightBlocksDeleteMarkUntilCallbackReturns(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	wsID := string(ws.ID())
	app := &App{lifecycle: workspaceLifecycleState{phases: make(map[string]lifecyclePhase)}}

	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan bool, 1)

	go func() {
		done <- app.runUnlessWorkspaceMutationInFlight(wsID, func() {
			close(entered)
			<-release
		})
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for guarded callback to start")
	}

	markDone := make(chan struct{})
	go func() {
		app.markWorkspaceMutationInFlight(ws, true)
		close(markDone)
	}()

	select {
	case <-markDone:
		t.Fatal("expected delete mark to block while guarded callback is running")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)

	select {
	case ok := <-done:
		if !ok {
			t.Fatal("expected guarded callback to run")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for guarded callback completion")
	}

	select {
	case <-markDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for delete mark after callback completion")
	}

	if !app.isWorkspaceMutationInFlight(wsID) {
		t.Fatal("expected workspace to be marked mutation-in-flight after callback completion")
	}
}
