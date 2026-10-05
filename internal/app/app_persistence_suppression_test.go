package app

import (
	"os"
	"testing"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

// newTabPersistHarness builds an App wired to a real temp-dir WorkspaceStore
// whose root doubles as config.Paths.MetadataRoot, so the store's on-disk
// path for wsID is exactly the path the reload guard fingerprints.
func newTabPersistHarness(t *testing.T, ws *data.Workspace) (*App, *data.WorkspaceStore) {
	t.Helper()
	metadataRoot := t.TempDir()
	store := data.NewWorkspaceStore(metadataRoot)
	if err := store.Save(ws); err != nil {
		t.Fatalf("seed Save() error = %v", err)
	}
	app := &App{
		workspaceService: workspacesvc.New(nil, store, nil, ""),
		stateWatcher:     &stateWatcher{},
		stateWatcherCh:   make(chan messages.StateWatcherEvent, 1),
		config:           &config.Config{Paths: &config.Paths{MetadataRoot: metadataRoot}},
	}
	return app, store
}

// watcherCmdCount delivers a watcher event for the workspace's file and
// reports how many commands the app produced: 1 means the reload was
// suppressed (only the watcher restart), 2 means it ran (load + restart).
func watcherCmdCount(t *testing.T, app *App, ws *data.Workspace) int {
	t.Helper()
	path := app.workspaceMetadataPath(string(ws.MetadataID()))
	if path == "" {
		t.Fatal("workspaceMetadataPath resolved empty")
	}
	cmds := app.handleStateWatcherEvent(messages.StateWatcherEvent{
		Reason: "workspaces",
		Paths:  []string{path},
	})
	return len(cmds)
}

// TestPersistOneTabSnapshot_NoOpRecordsNoMarker is the core regression: a
// tab save the store accepts as a no-op (the record already held these tabs)
// commits nothing, so no self-write marker may be recorded — otherwise the
// marker fingerprints whatever bytes are current and suppresses an external
// write's event.
func TestPersistOneTabSnapshot_NoOpRecordsNoMarker(t *testing.T) {
	tabs := []data.TabInfo{{Name: "agent", SessionName: "s1", Status: "running"}}
	ws := data.NewWorkspace("ws", "main", "main", "/repo", "/repo/ws")
	ws.OpenTabs = tabs
	app, _ := newTabPersistHarness(t, ws)

	committed, err := app.persistOneTabSnapshot(tabPersistSnapshot{
		wsID:      string(ws.MetadataID()),
		seq:       1,
		tabs:      tabs,
		activeIdx: ws.ActiveTabIndex,
		fallback:  snapshotWorkspaceForSave(ws),
	})
	if err != nil {
		t.Fatalf("persistOneTabSnapshot error = %v", err)
	}
	if committed {
		t.Fatal("identical tabs must report committed=false — nothing was written")
	}

	if got := watcherCmdCount(t, app, ws); got != 2 {
		t.Fatalf("no-op save must not suppress the reload: got %d cmds, want 2", got)
	}
}

// TestPersistOneTabSnapshot_ExternalChangeThenNoOp proves the exact defect:
// an external write changes the record between our snapshot and our save;
// our save still no-ops on tabs, so the external bytes must not be
// fingerprinted as ours. Pre-fix the marker recorded the external content
// and the watcher event for it was suppressed.
func TestPersistOneTabSnapshot_ExternalChangeThenNoOp(t *testing.T) {
	tabs := []data.TabInfo{{Name: "agent", SessionName: "s1", Status: "running"}}
	ws := data.NewWorkspace("ws", "main", "main", "/repo", "/repo/ws")
	ws.OpenTabs = tabs
	app, store := newTabPersistHarness(t, ws)

	// External writer changes an unrelated field — its watcher event must
	// stay visible even though our tab save runs after it.
	if err := store.Update(ws.MetadataID(), func(fresh *data.Workspace) (bool, error) {
		fresh.Name = "external-edit"
		return true, nil
	}); err != nil {
		t.Fatalf("external Update error = %v", err)
	}

	committed, err := app.persistOneTabSnapshot(tabPersistSnapshot{
		wsID:      string(ws.MetadataID()),
		seq:       1,
		tabs:      tabs,
		activeIdx: ws.ActiveTabIndex,
		fallback:  snapshotWorkspaceForSave(ws),
	})
	if err != nil {
		t.Fatalf("persistOneTabSnapshot error = %v", err)
	}
	if committed {
		t.Fatal("identical tabs must report committed=false even after an external field write")
	}

	if got := watcherCmdCount(t, app, ws); got != 2 {
		t.Fatalf("external change must not be suppressed by our no-op: got %d cmds, want 2", got)
	}
}

// TestPersistOneTabSnapshot_RealWriteSuppresses proves the guard still works
// for actual commits: a tab write that changes bytes records a marker and
// its own watcher event is suppressed.
func TestPersistOneTabSnapshot_RealWriteSuppresses(t *testing.T) {
	ws := data.NewWorkspace("ws", "main", "main", "/repo", "/repo/ws")
	ws.OpenTabs = []data.TabInfo{{Name: "agent", SessionName: "s1", Status: "running"}}
	app, _ := newTabPersistHarness(t, ws)

	newTabs := []data.TabInfo{
		{Name: "agent", SessionName: "s1", Status: "running"},
		{Name: "shell", SessionName: "s2", Status: "running"},
	}
	committed, err := app.persistOneTabSnapshot(tabPersistSnapshot{
		wsID:      string(ws.MetadataID()),
		seq:       1,
		tabs:      newTabs,
		activeIdx: ws.ActiveTabIndex,
		fallback:  snapshotWorkspaceForSave(ws),
	})
	if err != nil {
		t.Fatalf("persistOneTabSnapshot error = %v", err)
	}
	if !committed {
		t.Fatal("changed tabs must report committed=true")
	}

	if got := watcherCmdCount(t, app, ws); got != 1 {
		t.Fatalf("our own write's event must be suppressed: got %d cmds, want 1", got)
	}
}

// TestPersistOneTabSnapshot_SupersededRecordsNoMarker covers the other
// benign no-write outcome: a capture superseded by a newer committed seq is
// skipped before touching the store and must record nothing.
func TestPersistOneTabSnapshot_SupersededRecordsNoMarker(t *testing.T) {
	ws := data.NewWorkspace("ws", "main", "main", "/repo", "/repo/ws")
	ws.OpenTabs = []data.TabInfo{{Name: "agent", SessionName: "s1", Status: "running"}}
	app, _ := newTabPersistHarness(t, ws)

	newTabs := []data.TabInfo{
		{Name: "agent", SessionName: "s1", Status: "running"},
		{Name: "shell", SessionName: "s2", Status: "running"},
	}
	if _, err := app.persistOneTabSnapshot(tabPersistSnapshot{
		wsID:      string(ws.MetadataID()),
		seq:       5,
		tabs:      newTabs,
		activeIdx: ws.ActiveTabIndex,
		fallback:  snapshotWorkspaceForSave(ws),
	}); err != nil {
		t.Fatalf("seed persist error = %v", err)
	}

	// Clear the legitimately-recorded marker so this test isolates the
	// superseded path's own marker behavior.
	app.lifecycle.localSaveMu.Lock()
	app.lifecycle.localSavesAt = nil
	app.lifecycle.localSaveMu.Unlock()

	committed, err := app.persistOneTabSnapshot(tabPersistSnapshot{
		wsID:      string(ws.MetadataID()),
		seq:       3, // older than the committed seq 5
		tabs:      ws.OpenTabs,
		activeIdx: ws.ActiveTabIndex,
		fallback:  snapshotWorkspaceForSave(ws),
	})
	if err != nil {
		t.Fatalf("persistOneTabSnapshot error = %v", err)
	}
	if committed {
		t.Fatal("superseded capture must report committed=false")
	}

	// An external byte change after the superseded attempt must stay
	// visible — there is no marker to suppress it.
	path := app.workspaceMetadataPath(string(ws.MetadataID()))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}
	if err := os.WriteFile(path, append(raw, ' '), 0o644); err != nil {
		t.Fatalf("external WriteFile error = %v", err)
	}
	if got := watcherCmdCount(t, app, ws); got != 2 {
		t.Fatalf("superseded save must not leave a suppressing marker: got %d cmds, want 2", got)
	}
}
