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

// TestHandleTmuxTabsSyncResult_PreservesConcurrentFieldEdits is the plan-032
// regression: the tmux-sync commit applies OpenTabs to the fresh record
// inside the store lock, so an Env/Name edit committed between the app's
// snapshot and the write survives — the pre-fix whole-record Save wrote the
// snapshot's stale fields back over it.
func TestHandleTmuxTabsSyncResult_PreservesConcurrentFieldEdits(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	ws.Env = map[string]string{"K": "orig"}
	ws.OpenTabs = []data.TabInfo{{
		Name:        "agent",
		Assistant:   "claude",
		SessionName: "sess-1",
		Status:      "running",
	}}

	metadataRoot := t.TempDir()
	store := data.NewWorkspaceStore(metadataRoot)
	if err := store.Save(ws); err != nil {
		t.Fatalf("seed Save() error = %v", err)
	}
	svc := workspacesvc.New(nil, store, nil, "")
	app := &App{
		workspaceService: svc,
		projects: []data.Project{{
			Name: "repo", Path: "/repo",
			Workspaces: []data.Workspace{*ws},
		}},
		lifecycle: newWorkspaceLifecycleState(),
	}

	cmds := app.handleTmuxTabsSyncResult(tmuxTabsSyncResult{
		WorkspaceID: string(ws.ID()),
		Updates: []tmuxTabStatusUpdate{{
			SessionName: "sess-1",
			Status:      "stopped",
		}},
	})
	if len(cmds) == 0 {
		t.Fatal("expected a sync save cmd")
	}

	// External edit lands after the sync captured its snapshot but before
	// the write commits — a whole-record Save would revert it.
	if err := store.Update(ws.MetadataID(), func(fresh *data.Workspace) (bool, error) {
		fresh.Name = "external-edit"
		fresh.Env = map[string]string{"K": "external"}
		return true, nil
	}); err != nil {
		t.Fatalf("external Update error = %v", err)
	}

	for _, cmd := range cmds {
		if cmd != nil {
			_ = cmd()
		}
	}

	loaded, err := store.Load(ws.MetadataID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Name != "external-edit" {
		t.Fatalf("Name = %q, want %q — sync reverted the external rename", loaded.Name, "external-edit")
	}
	if loaded.Env["K"] != "external" {
		t.Fatalf("Env K = %q, want %q — sync reverted the external env edit", loaded.Env["K"], "external")
	}
	if len(loaded.OpenTabs) != 1 || loaded.OpenTabs[0].Status != "stopped" {
		t.Fatalf("sync's own write missing: OpenTabs = %#v", loaded.OpenTabs)
	}
}

// TestHandleTmuxTabsSyncResult_NoChangeSkipsWrite proves a sync whose statuses
// already match the record performs no store write at all — no marker, no
// clobber opportunity.
func TestHandleTmuxTabsSyncResult_NoChangeSkipsWrite(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	ws.OpenTabs = []data.TabInfo{{
		Name:        "agent",
		Assistant:   "claude",
		SessionName: "sess-1",
		Status:      "running",
	}}

	store := &testutil.FakeWorkspaceStore{}
	app := &App{
		workspaceService: workspacesvc.New(nil, store, nil, ""),
		projects: []data.Project{{
			Name: "repo", Path: "/repo",
			Workspaces: []data.Workspace{*ws},
		}},
		lifecycle: newWorkspaceLifecycleState(),
	}

	cmds := app.handleTmuxTabsSyncResult(tmuxTabsSyncResult{
		WorkspaceID: string(ws.ID()),
		Updates: []tmuxTabStatusUpdate{{
			SessionName: "sess-1",
			Status:      "running", // unchanged
		}},
	})
	if len(cmds) != 0 {
		t.Fatalf("status already current — expected no cmds, got %d", len(cmds))
	}
	if saved := store.SavedIDs(); len(saved) != 0 {
		t.Fatalf("no store write expected, got %v", saved)
	}
}
