package app

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/testutil"
)

// blockingWorkspaceStore blocks inside Save until released — the blocking
// contract is the point of the fake; the embedded shared store provides the
// rest of WorkspaceStore and records the completed saves.
type blockingWorkspaceStore struct {
	testutil.FakeWorkspaceStore

	saveStarted chan struct{}
	releaseSave chan struct{}
}

func newBlockingWorkspaceStore() *blockingWorkspaceStore {
	return &blockingWorkspaceStore{
		saveStarted: make(chan struct{}),
		releaseSave: make(chan struct{}),
	}
}

func (s *blockingWorkspaceStore) Save(workspace *data.Workspace) error {
	close(s.saveStarted)
	<-s.releaseSave
	return s.FakeWorkspaceStore.Save(workspace)
}

func (s *blockingWorkspaceStore) SaveCalls() int {
	return len(s.SavedIDs())
}

func TestHandleTmuxTabsSyncResult_SaveAndDeleteMarkAreAtomic(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	wsID := string(ws.ID())
	ws.OpenTabs = []data.TabInfo{{
		Name:        "agent",
		Assistant:   "claude",
		SessionName: "sess-1",
		Status:      "running",
	}}

	store := newBlockingWorkspaceStore()
	svc := workspacesvc.New(nil, store, nil, "")
	app := &App{
		workspaceService: svc,
		projects:         []data.Project{{Name: "repo", Path: "/repo", Workspaces: []data.Workspace{*ws}}},
		lifecycle: workspaceLifecycleState{
			phases: make(map[string]lifecyclePhase),
		},
	}

	cmds := app.handleTmuxTabsSyncResult(tmuxTabsSyncResult{
		WorkspaceID: wsID,
		Updates: []tmuxTabStatusUpdate{{
			SessionName: "sess-1",
			Status:      "stopped",
		}},
	})
	if len(cmds) != 1 {
		t.Fatalf("expected exactly one save cmd, got %d", len(cmds))
	}

	cmdDone := make(chan struct{})
	go func() {
		_ = cmds[0]()
		close(cmdDone)
	}()

	select {
	case <-store.saveStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for sync save to start")
	}

	markDone := make(chan struct{})
	go func() {
		app.markWorkspaceMutationInFlight(ws, true)
		close(markDone)
	}()

	select {
	case <-markDone:
		t.Fatal("expected delete mark to block while sync save is in guarded section")
	case <-time.After(50 * time.Millisecond):
	}

	close(store.releaseSave)

	select {
	case <-cmdDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for sync save command to complete")
	}

	select {
	case <-markDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for delete mark after sync save completion")
	}

	if store.SaveCalls() != 1 {
		t.Fatalf("expected one save call, got %d", store.SaveCalls())
	}
	if !app.isWorkspaceMutationInFlight(wsID) {
		t.Fatal("expected workspace to be marked mutation-in-flight after save section")
	}
}

func (s *blockingWorkspaceStore) SetScripts(data.WorkspaceID, data.ScriptsConfig, string) error {
	return nil
}
