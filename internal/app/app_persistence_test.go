package app

import (
	"errors"
	"io/fs"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

func TestPersistAllWorkspacesNowSavesExplicitlyEmptyTabs(t *testing.T) {
	ws := data.NewWorkspace("test-ws", "main", "main", "/repo", "/repo")
	wsID := string(ws.ID())

	storeRoot := t.TempDir()
	store := data.NewWorkspaceStore(storeRoot)

	// Save initial workspace with a tab so we can verify it gets updated to empty
	ws.OpenTabs = []data.TabInfo{{Name: "old-tab", Assistant: "claude"}}
	if err := store.Save(ws); err != nil {
		t.Fatalf("initial save: %v", err)
	}

	c := center.New(nil)
	c.SetWorkspace(ws)
	// Add a tab then close it so the workspace has explicit empty state
	tab := &center.Tab{
		Name:      "agent",
		Assistant: "claude",
		Workspace: ws,
	}
	c.AddTab(tab)
	// Close the tab — tab has no session/agent so close is lightweight
	_ = c.CloseActiveTab()

	// After close: tabs list is empty but workspace state map entry exists
	tabs, _ := c.GetTabsInfoForWorkspace(wsID)
	if len(tabs) != 0 {
		t.Fatalf("expected 0 tabs after close, got %d", len(tabs))
	}
	if !c.HasWorkspaceState(wsID) {
		t.Fatal("expected HasWorkspaceState=true after close")
	}

	svc := newWorkspaceService(nil, store, nil, "")

	// Clear old tabs from in-memory workspace before persist
	ws.OpenTabs = nil
	app := &App{
		center:           c,
		workspaceService: svc,
		projects:         []data.Project{{Name: "p", Path: "/repo", Workspaces: []data.Workspace{*ws}}},
		lifecycle: workspaceLifecycleState{
			dirty: make(map[string]bool),
		},
	}

	app.persistAllWorkspacesNow()

	// Reload from store and verify the workspace was saved with empty tabs
	loaded, err := store.Load(ws.ID())
	if err != nil {
		t.Fatalf("load after persist: %v", err)
	}
	if len(loaded.OpenTabs) != 0 {
		t.Fatalf("expected 0 open tabs after persist, got %d", len(loaded.OpenTabs))
	}
}

func TestPersistAllWorkspacesNowSkipsDeleteInFlightWorkspace(t *testing.T) {
	// Shutdown must not save a workspace while its delete is in flight. The delete
	// can remove the worktree and metadata while shutdown persistence is still
	// collecting state, and a later save would recreate dir-less metadata.
	wsRoot := t.TempDir()
	ws := data.NewWorkspace("test-ws", "feature", "main", "/repo", wsRoot)
	wsID := string(ws.ID())

	storeRoot := t.TempDir()
	store := data.NewWorkspaceStore(storeRoot)

	c := center.New(nil)
	c.SetWorkspace(ws)
	tab := &center.Tab{
		Name:      "agent",
		Assistant: "claude",
		Workspace: ws,
	}
	c.AddTab(tab)

	svc := newWorkspaceService(nil, store, nil, "")
	app := &App{
		center:           c,
		workspaceService: svc,
		projects:         []data.Project{{Name: "p", Path: "/repo", Workspaces: []data.Workspace{*ws}}},
		lifecycle: workspaceLifecycleState{
			dirty:  make(map[string]bool),
			phases: map[string]lifecyclePhase{wsID: lifecycleDeleting},
		},
	}

	app.persistAllWorkspacesNow()

	if _, err := store.Load(ws.ID()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected delete-in-flight workspace metadata to remain absent, err=%v", err)
	}
}

func TestPersistWorkspaceTabsInitializesDirtyMap(t *testing.T) {
	app := &App{
		lifecycle: workspaceLifecycleState{
			dirty: nil, // explicitly nil
		},
	}

	cmd := app.persistWorkspaceTabs("ws-123")
	if cmd == nil {
		t.Fatal("expected a debounce command, got nil")
	}
	if app.lifecycle.dirty == nil {
		t.Fatal("expected dirtyWorkspaces to be initialized")
	}
	if !app.lifecycle.dirty["ws-123"] {
		t.Fatal("expected ws-123 to be marked dirty")
	}
}

func TestPersistWorkspaceTabsSkipsDeleteInFlightWorkspace(t *testing.T) {
	app := &App{
		lifecycle: workspaceLifecycleState{
			dirty:  make(map[string]bool),
			phases: map[string]lifecyclePhase{"ws-123": lifecycleDeleting},
		},
	}

	cmd := app.persistWorkspaceTabs("ws-123")
	if cmd != nil {
		t.Fatal("expected no debounce command for deleting workspace")
	}
	if app.lifecycle.dirty["ws-123"] {
		t.Fatal("did not expect deleting workspace to be marked dirty")
	}
}

func TestHandlePersistDebounceSkipsWhenPersistenceDependenciesMissing(t *testing.T) {
	// nil center
	app := &App{
		center:           nil,
		workspaceService: newWorkspaceService(nil, nil, nil, ""),
		lifecycle: workspaceLifecycleState{
			persistToken: 1,
			dirty:        map[string]bool{"ws": true},
		},
	}
	cmd := app.handlePersistDebounce(persistDebounceMsg{token: 1})
	if cmd != nil {
		t.Fatal("expected nil cmd when center is nil")
	}

	// nil workspaceService
	app2 := &App{
		center:           center.New(nil),
		workspaceService: nil,
		lifecycle: workspaceLifecycleState{
			persistToken: 1,
			dirty:        map[string]bool{"ws": true},
		},
	}
	cmd2 := app2.handlePersistDebounce(persistDebounceMsg{token: 1})
	if cmd2 != nil {
		t.Fatal("expected nil cmd when workspaceService is nil")
	}
}

func TestHandlePersistDebounceSkipsDeleteInFlightWorkspace(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	wsID := string(ws.ID())

	storeRoot := t.TempDir()
	store := data.NewWorkspaceStore(storeRoot)
	svc := newWorkspaceService(nil, store, nil, "")

	app := &App{
		center:           center.New(nil),
		workspaceService: svc,
		projects:         []data.Project{{Name: "repo", Path: "/repo", Workspaces: []data.Workspace{*ws}}},
		lifecycle: workspaceLifecycleState{
			persistToken: 1,
			dirty:        map[string]bool{wsID: true},
			phases:       map[string]lifecyclePhase{wsID: lifecycleDeleting},
			localSavesAt: make(map[string]localWorkspaceSaveMarker),
		},
	}

	cmd := app.handlePersistDebounce(persistDebounceMsg{token: 1})
	if cmd != nil {
		t.Fatal("expected nil cmd when only dirty workspace is delete-in-flight")
	}
	if !app.lifecycle.dirty[wsID] {
		t.Fatal("expected dirty marker to remain while workspace delete is in-flight")
	}
	if _, err := store.Load(ws.ID()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected workspace metadata to remain absent, err=%v", err)
	}
}

func TestDeleteFailureRequeuesAndDebouncedPersistSavesWorkspace(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	wsID := string(ws.ID())

	storeRoot := t.TempDir()
	store := data.NewWorkspaceStore(storeRoot)
	svc := newWorkspaceService(nil, store, nil, "")

	c := center.New(nil)
	c.SetWorkspace(ws)
	c.AddTab(&center.Tab{
		Name:      "agent",
		Assistant: "claude",
		Workspace: ws,
	})

	app := &App{
		center:           c,
		dashboard:        dashboard.New(),
		workspaceService: svc,
		projects:         []data.Project{{Name: "repo", Path: "/repo", Workspaces: []data.Workspace{*ws}}},
		lifecycle: workspaceLifecycleState{
			persistToken: 1,
			dirty:        map[string]bool{wsID: true},
			phases:       map[string]lifecyclePhase{wsID: lifecycleDeleting},
			localSavesAt: make(map[string]localWorkspaceSaveMarker),
		},
	}

	if cmd := app.handlePersistDebounce(persistDebounceMsg{token: 1}); cmd != nil {
		t.Fatal("expected nil cmd while workspace delete is in-flight")
	}
	if !app.lifecycle.dirty[wsID] {
		t.Fatal("expected dirty marker to remain while delete is in-flight")
	}

	if cmd := app.handleWorkspaceDeleteFailed(messages.WorkspaceDeleteFailed{
		Workspace: ws,
		Err:       errors.New("delete failed"),
	}); cmd == nil {
		t.Fatal("expected non-nil command on delete failure")
	}
	if app.isWorkspaceDeleteInFlight(wsID) {
		t.Fatal("expected delete-in-flight marker to be cleared on delete failure")
	}

	persistCmd := app.handlePersistDebounce(persistDebounceMsg{token: app.lifecycle.persistToken})
	if persistCmd == nil {
		t.Fatal("expected debounced persistence command after delete failure requeue")
	}
	if msg := persistCmd(); msg != nil {
		t.Fatalf("expected nil tea.Msg from persistence command, got %T", msg)
	}

	loaded, err := store.Load(ws.ID())
	if err != nil {
		t.Fatalf("load after persistence: %v", err)
	}
	if len(loaded.OpenTabs) == 0 {
		t.Fatal("expected workspace tabs to be persisted after delete failure requeue")
	}
	if app.lifecycle.dirty[wsID] {
		t.Fatal("expected workspace to be cleared from dirty set after save")
	}
}

// TestHandlePersistDebounceReDirtiesWorkspaceOnSaveFailure proves a failed
// debounced Save is not silently dropped: the save goroutine reports the
// failure via persistSaveFailedMsg (it must not touch a.lifecycle.dirty
// itself — that's App state, single-writer on the Update loop), and
// handlePersistSaveFailed — the Update-loop handler for that message —
// re-dirties the workspace so a later debounce or clean shutdown retries it.
func TestHandlePersistDebounceReDirtiesWorkspaceOnSaveFailure(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	wsID := string(ws.ID())

	store := &failingDeleteStore{saveErr: errors.New("disk full")}
	svc := newWorkspaceService(nil, store, nil, "")

	c := center.New(nil)
	c.SetWorkspace(ws)
	c.AddTab(&center.Tab{
		Name:      "agent",
		Assistant: "claude",
		Workspace: ws,
	})

	app := &App{
		center:           c,
		workspaceService: svc,
		projects:         []data.Project{{Name: "repo", Path: "/repo", Workspaces: []data.Workspace{*ws}}},
		lifecycle: workspaceLifecycleState{
			persistToken: 1,
			dirty:        map[string]bool{wsID: true},
			localSavesAt: make(map[string]localWorkspaceSaveMarker),
		},
	}

	cmd := app.handlePersistDebounce(persistDebounceMsg{token: 1})
	if cmd == nil {
		t.Fatal("expected a save command for the dirty workspace")
	}
	// handlePersistDebounce clears the dirty marker synchronously at
	// snapshot-collection time; the save itself (and any re-dirty on
	// failure) happens later, when the returned Cmd runs.
	if app.lifecycle.dirty[wsID] {
		t.Fatal("expected dirty marker cleared at snapshot time, before save runs")
	}

	msg := cmd()
	if store.saved == nil {
		t.Fatal("expected Save to have been called")
	}
	failMsg, ok := msg.(persistSaveFailedMsg)
	if !ok {
		t.Fatalf("expected persistSaveFailedMsg on save failure, got %T", msg)
	}
	if len(failMsg.workspaceIDs) != 1 || failMsg.workspaceIDs[0] != wsID {
		t.Fatalf("expected failed workspace IDs to be [%s], got %v", wsID, failMsg.workspaceIDs)
	}

	// The Update loop (not the save goroutine) performs the re-dirty.
	reDirtyCmd := app.handlePersistSaveFailed(failMsg)
	if reDirtyCmd == nil {
		t.Fatal("expected a rescheduled debounce command after save failure")
	}
	if !app.lifecycle.dirty[wsID] {
		t.Fatal("expected workspace to be re-dirtied after save failure")
	}
}

// TestHandlePersistDebounceSuccessDoesNotReDirty is the control case for
// TestHandlePersistDebounceReDirtiesWorkspaceOnSaveFailure: a successful save
// must not emit persistSaveFailedMsg and must leave the workspace clean.
func TestHandlePersistDebounceSuccessDoesNotReDirty(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	wsID := string(ws.ID())

	storeRoot := t.TempDir()
	store := data.NewWorkspaceStore(storeRoot)
	svc := newWorkspaceService(nil, store, nil, "")

	c := center.New(nil)
	c.SetWorkspace(ws)
	c.AddTab(&center.Tab{
		Name:      "agent",
		Assistant: "claude",
		Workspace: ws,
	})

	app := &App{
		center:           c,
		workspaceService: svc,
		projects:         []data.Project{{Name: "repo", Path: "/repo", Workspaces: []data.Workspace{*ws}}},
		lifecycle: workspaceLifecycleState{
			persistToken: 1,
			dirty:        map[string]bool{wsID: true},
			localSavesAt: make(map[string]localWorkspaceSaveMarker),
		},
	}

	cmd := app.handlePersistDebounce(persistDebounceMsg{token: 1})
	if cmd == nil {
		t.Fatal("expected a save command for the dirty workspace")
	}

	msg := cmd()
	if msg != nil {
		t.Fatalf("expected nil tea.Msg on successful save, got %T", msg)
	}
	if app.lifecycle.dirty[wsID] {
		t.Fatal("expected workspace to remain clean after a successful save")
	}

	loaded, err := store.Load(ws.ID())
	if err != nil {
		t.Fatalf("load after persistence: %v", err)
	}
	if len(loaded.OpenTabs) == 0 {
		t.Fatal("expected workspace tabs to be persisted")
	}
}
