package app

import (
	"sync"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

type blockingWorkspaceStore struct {
	saveStarted chan struct{}
	releaseSave chan struct{}
	mu          sync.Mutex
	saveCalls   int
}

func newBlockingWorkspaceStore() *blockingWorkspaceStore {
	return &blockingWorkspaceStore{
		saveStarted: make(chan struct{}),
		releaseSave: make(chan struct{}),
	}
}

func (s *blockingWorkspaceStore) ListByRepo(repo string) ([]*data.Workspace, error) {
	return nil, nil
}

func (s *blockingWorkspaceStore) ListByRepoIncludingArchived(repo string) ([]*data.Workspace, error) {
	return nil, nil
}

func (s *blockingWorkspaceStore) LoadMetadataFor(workspace *data.Workspace) (bool, error) {
	return false, nil
}

func (s *blockingWorkspaceStore) UpsertFromDiscovery(workspace *data.Workspace) error {
	return nil
}

func (s *blockingWorkspaceStore) Save(workspace *data.Workspace) error {
	close(s.saveStarted)
	<-s.releaseSave
	s.mu.Lock()
	s.saveCalls++
	s.mu.Unlock()
	return nil
}

func (s *blockingWorkspaceStore) Delete(id data.WorkspaceID) error {
	return nil
}

func (s *blockingWorkspaceStore) ResolvedDefaultAssistant() string {
	return data.DefaultAssistant
}

func (s *blockingWorkspaceStore) SaveCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveCalls
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
	svc := newWorkspaceService(nil, store, nil, "")
	app := &App{
		workspaceService:     svc,
		projects:             []data.Project{{Name: "repo", Path: "/repo", Workspaces: []data.Workspace{*ws}}},
		deletingWorkspaceIDs: make(map[string]bool),
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
		app.markWorkspaceDeleteInFlight(ws, true)
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
	if !app.isWorkspaceDeleteInFlight(wsID) {
		t.Fatal("expected workspace to be marked delete-in-flight after save section")
	}
}
