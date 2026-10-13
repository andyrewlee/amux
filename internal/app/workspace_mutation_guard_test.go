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

// TestSyncWorkspaceTabsInsideGuardNeverReacquiresPhaseLock is the nested-RLock
// regression: the tmux tab-sync callback runs inside
// runUnlessWorkspaceMutationInFlight, which holds phaseMu.RLock across it.
// The pre-fix SyncWorkspaceTabs probed isMutationInFlight *inside* its store
// transaction — a second RLock that blocks forever once a writer queues,
// while the writer blocks on the still-held outer read. Go's RWMutex is
// writer-preferring, so a single queued markMutating completes the deadlock.
// The fix passes checkMutation=false: no in-transaction probe may run, so
// the guarded call must complete even with a writer queued.
func TestSyncWorkspaceTabsInsideGuardNeverReacquiresPhaseLock(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	wsID := string(ws.ID())
	app := &App{lifecycle: workspaceLifecycleState{
		phases:         make(map[string]lifecyclePhase),
		mutatingRootID: make(map[string]string),
	}}

	store := data.NewWorkspaceStore(t.TempDir())
	if err := store.Save(ws); err != nil {
		t.Fatalf("store.Save() error = %v", err)
	}
	svc := workspacesvc.New(&testutil.FakeProjectRegistry{}, store, nil, "")
	svc.Configure(workspacesvc.Deps{
		MutationInFlight:      app.lifecycle.isMutatingWorkspaceIDs,
		MutationInFlightGuard: app.lifecycle.runUnlessMutatingWorkspaceIDs,
	})

	entered := make(chan struct{})
	writerQueued := make(chan struct{})
	syncDone := make(chan error, 1)
	done := make(chan bool, 1)

	go func() {
		done <- app.runUnlessWorkspaceMutationInFlight(wsID, func() {
			close(entered)
			<-writerQueued
			// checkMutation=false — the atomic guard already covers the
			// check; an in-transaction probe would re-acquire phaseMu.
			_, err := svc.SyncWorkspaceTabs(ws, []data.TabInfo{{Name: "a"}}, false)
			syncDone <- err
		})
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for guarded callback to start")
	}

	// Queue a writer while the outer RLock is held — the exact interleaving
	// that turned the nested probe into a self-deadlock.
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

	close(writerQueued)

	select {
	case err := <-syncDone:
		if err != nil {
			t.Fatalf("SyncWorkspaceTabs() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SyncWorkspaceTabs blocked inside the guard — nested phaseMu acquisition")
	}

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
}
