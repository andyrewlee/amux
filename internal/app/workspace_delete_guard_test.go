package app

import (
	"errors"
	"sync"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

func TestMarkWorkspaceDeleteInFlightPreservesDirtyState(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	wsID := string(ws.ID())

	app := &App{
		dirtyWorkspaces:      map[string]bool{wsID: true},
		deletingWorkspaceIDs: make(map[string]bool),
	}

	app.markWorkspaceDeleteInFlight(ws, true)

	if !app.dirtyWorkspaces[wsID] {
		t.Fatal("expected dirty workspace marker to be preserved when delete starts")
	}
	if !app.isWorkspaceDeleteInFlight(wsID) {
		t.Fatal("expected workspace to be marked delete-in-flight")
	}
}

func TestHandleWorkspaceDeleteFailedRequeuesWorkspacePersistence(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	wsID := string(ws.ID())

	app := &App{
		dashboard:            dashboard.New(),
		dirtyWorkspaces:      make(map[string]bool),
		deletingWorkspaceIDs: map[string]bool{wsID: true},
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
	if !app.dirtyWorkspaces[wsID] {
		t.Fatal("expected workspace persistence to be re-queued on delete failure")
	}
}

func TestWorkspaceDeleteInFlightConcurrentAccess(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	wsID := string(ws.ID())

	app := &App{
		deletingWorkspaceIDs: make(map[string]bool),
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
