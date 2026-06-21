package app

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

func TestMarkWorkspaceDeleteInFlightPreservesDirtyState(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	wsID := string(ws.ID())

	app := &App{
		lifecycle: workspaceLifecycleState{
			dirty:  map[string]bool{wsID: true},
			phases: make(map[string]lifecyclePhase),
		},
	}

	app.markWorkspaceDeleteInFlight(ws, true)

	if !app.lifecycle.dirty[wsID] {
		t.Fatal("expected dirty workspace marker to be preserved when delete starts")
	}
	if !app.isWorkspaceDeleteInFlight(wsID) {
		t.Fatal("expected workspace to be marked delete-in-flight")
	}
}

func TestHandleWorkspaceDeleteFailedRequeuesWorkspacePersistence(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	wsID := string(ws.ID())
	project := data.NewProject("/repo")
	project.Workspaces = []data.Workspace{*ws}

	app := &App{
		dashboard:        dashboard.New(),
		workspaceService: newWorkspaceService(&fakeProjectRegistry{}, nil, nil, ""),
		lifecycle: workspaceLifecycleState{
			dirty:  make(map[string]bool),
			phases: map[string]lifecyclePhase{wsID: lifecycleDeleting},
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
	if app.isWorkspaceDeleteInFlight(wsID) {
		t.Fatal("expected delete-in-flight marker to be cleared on delete failure")
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

func TestWorkspaceDeleteInFlightConcurrentAccess(t *testing.T) {
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
					app.markWorkspaceDeleteInFlight(ws, true)
				} else {
					app.markWorkspaceDeleteInFlight(ws, false)
				}
				if idx%2 == 0 {
					_ = app.isWorkspaceDeleteInFlight(wsID)
				}
			}
		}(i)
	}

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 2000; j++ {
				_ = app.isWorkspaceDeleteInFlight(wsID)
			}
		}()
	}

	wg.Wait()
}

func TestRunUnlessWorkspaceDeleteInFlightSkipsWhenDeleting(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	wsID := string(ws.ID())
	app := &App{lifecycle: workspaceLifecycleState{phases: map[string]lifecyclePhase{wsID: lifecycleDeleting}}}

	ran := false
	ok := app.runUnlessWorkspaceDeleteInFlight(wsID, func() {
		ran = true
	})
	if ok {
		t.Fatal("expected guard to skip callback when workspace is delete-in-flight")
	}
	if ran {
		t.Fatal("callback should not have run while workspace is delete-in-flight")
	}
}

func TestRunUnlessWorkspaceDeleteInFlightBlocksDeleteMarkUntilCallbackReturns(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	wsID := string(ws.ID())
	app := &App{lifecycle: workspaceLifecycleState{phases: make(map[string]lifecyclePhase)}}

	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan bool, 1)

	go func() {
		done <- app.runUnlessWorkspaceDeleteInFlight(wsID, func() {
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
		app.markWorkspaceDeleteInFlight(ws, true)
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

	if !app.isWorkspaceDeleteInFlight(wsID) {
		t.Fatal("expected workspace to be marked delete-in-flight after callback completion")
	}
}
