package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/center"
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
		dirtyWorkspaces:  make(map[string]bool),
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

func TestPersistWorkspaceTabsInitializesDirtyMap(t *testing.T) {
	app := &App{
		dirtyWorkspaces: nil, // explicitly nil
	}

	cmd := app.persistWorkspaceTabs("ws-123")
	if cmd == nil {
		t.Fatal("expected a debounce command, got nil")
	}
	if app.dirtyWorkspaces == nil {
		t.Fatal("expected dirtyWorkspaces to be initialized")
	}
	if !app.dirtyWorkspaces["ws-123"] {
		t.Fatal("expected ws-123 to be marked dirty")
	}
}

func TestHandlePersistDebounceSkipsWhenPersistenceDependenciesMissing(t *testing.T) {
	// nil center
	app := &App{
		center:           nil,
		workspaceService: newWorkspaceService(nil, nil, nil, ""),
		persistToken:     1,
		dirtyWorkspaces:  map[string]bool{"ws": true},
	}
	cmd := app.handlePersistDebounce(persistDebounceMsg{token: 1})
	if cmd != nil {
		t.Fatal("expected nil cmd when center is nil")
	}

	// nil workspaceService
	app2 := &App{
		center:           center.New(nil),
		workspaceService: nil,
		persistToken:     1,
		dirtyWorkspaces:  map[string]bool{"ws": true},
	}
	cmd2 := app2.handlePersistDebounce(persistDebounceMsg{token: 1})
	if cmd2 != nil {
		t.Fatal("expected nil cmd when workspaceService is nil")
	}
}
