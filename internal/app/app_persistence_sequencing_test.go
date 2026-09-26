package app

import (
	"io/fs"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/testutil"
	"github.com/andyrewlee/amux/internal/ui/center"
)

// newSequencedPersistApp builds an App wired to a real store with one saved
// workspace and one center tab, returning handles for the fixture.
func newSequencedPersistApp(t *testing.T, tabName string) (*App, *data.Workspace, *data.WorkspaceStore) {
	t.Helper()
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	wsID := string(ws.ID())
	store := data.NewWorkspaceStore(t.TempDir())
	if err := store.Save(ws); err != nil {
		t.Fatalf("seed Save() error = %v", err)
	}
	svc := workspacesvc.New(nil, store, nil, "")

	c := center.New(nil)
	c.SetWorkspace(ws)
	c.AddTab(&center.Tab{Name: tabName, Assistant: "claude", Workspace: ws})

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
	return app, ws, store
}

// TestPersistDebounce_SnapshotDoesNotClobberConcurrentFieldWrite is the
// plan-039 core regression at the app seam: the debounced capture carries a
// stale workspace snapshot, but the write that runs later must change only
// the tab fields — a rename/env update committed between capture and
// execution survives.
func TestPersistDebounce_SnapshotDoesNotClobberConcurrentFieldWrite(t *testing.T) {
	app, ws, store := newSequencedPersistApp(t, "tab-a")
	wsID := string(ws.ID())

	cmd := app.handlePersistDebounce(persistDebounceMsg{token: app.lifecycle.persistToken})
	if cmd == nil {
		t.Fatal("expected a save command for the dirty workspace")
	}

	// Concurrent writers commit their own fields after the snapshot was
	// captured (the snapshot still carries the pre-rename record).
	if err := store.SetEnv(ws.MetadataID(), map[string]string{"LIVE": "1"}); err != nil {
		t.Fatalf("SetEnv() error = %v", err)
	}
	if err := store.Rename(ws.MetadataID(), "live-name"); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	if err := store.SetScripts(ws.MetadataID(), data.ScriptsConfig{Setup: "echo s"}, "concurrent"); err != nil {
		t.Fatalf("SetScripts() error = %v", err)
	}

	if msg := cmd(); msg != nil {
		t.Fatalf("persistence command returned %T, want nil", msg)
	}
	loaded, err := store.Load(ws.ID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.OpenTabs) != 1 || loaded.OpenTabs[0].Name != "tab-a" {
		t.Fatalf("intended tabs not persisted: %v", loaded.OpenTabs)
	}
	if loaded.Name != "live-name" || loaded.Env["LIVE"] != "1" || loaded.Scripts.Setup != "echo s" {
		t.Fatalf("stale snapshot clobbered concurrent writes: Name=%q Env=%v Scripts=%v",
			loaded.Name, loaded.Env, loaded.Scripts)
	}
	if app.lifecycle.dirty[wsID] {
		t.Fatal("workspace stayed dirty after a successful save")
	}
}

// TestPersistDebounce_NewerCaptureWinsOverReversedExecution executes two
// queued captures newest-first: the older command must be a benign no-op
// rather than reverting the record to its stale tabs.
func TestPersistDebounce_NewerCaptureWinsOverReversedExecution(t *testing.T) {
	app, ws, store := newSequencedPersistApp(t, "old-tab")
	wsID := string(ws.ID())

	oldCmd := app.handlePersistDebounce(persistDebounceMsg{token: app.lifecycle.persistToken})
	if oldCmd == nil {
		t.Fatal("expected a save command for the first capture")
	}

	// Center state moves on; a second capture records the newer tabs.
	app.center.CloseActiveTab()
	app.center.AddTab(&center.Tab{Name: "new-tab", Assistant: "claude", Workspace: ws})
	app.lifecycle.dirty[wsID] = true
	app.lifecycle.persistToken++
	newCmd := app.handlePersistDebounce(persistDebounceMsg{token: app.lifecycle.persistToken})
	if newCmd == nil {
		t.Fatal("expected a save command for the second capture")
	}

	// Reversed execution: the newer command runs first.
	if msg := newCmd(); msg != nil {
		t.Fatalf("newer command returned %T, want nil", msg)
	}
	if msg := oldCmd(); msg != nil {
		t.Fatalf("stale command returned %T, want a benign nil", msg)
	}

	loaded, err := store.Load(ws.ID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.OpenTabs) != 1 || loaded.OpenTabs[0].Name != "new-tab" {
		t.Fatalf("reversed execution reverted tabs to %v, want the newer capture", loaded.OpenTabs)
	}
}

// TestPersistAllWorkspacesNow_WaitsBehindBlockedOlderWrite covers shutdown
// ordering: a flush must wait behind a write already in progress (the
// per-workspace lock serializes them) and, carrying the newer sequence,
// still end up the committed state.
func TestPersistAllWorkspacesNow_WaitsBehindBlockedOlderWrite(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	wsID := string(ws.ID())

	// Seed a committed record so the writes take the Update path, not the
	// missing-record create fallback.
	fake := &testutil.FakeWorkspaceStore{}
	if err := fake.Save(ws); err != nil {
		t.Fatalf("seed Save() error = %v", err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	blocked := false // UpdateFunc calls serialize under the service's per-ID lock
	fake.UpdateFunc = func(id data.WorkspaceID, fn func(ws *data.Workspace) (bool, error)) error {
		if !blocked {
			blocked = true
			close(entered)
			<-release
		}
		// Mirror the fake's default: apply fn to the last saved record and
		// Save on change.
		var cur *data.Workspace
		for _, saved := range fake.SavedWorkspaces() {
			if saved.MetadataID() == id || saved.ID() == id {
				cur = saved
			}
		}
		if cur == nil {
			return fs.ErrNotExist
		}
		cp := *cur
		changed, err := fn(&cp)
		if err != nil || !changed {
			return err
		}
		return fake.Save(&cp)
	}
	svc := workspacesvc.New(nil, fake, nil, "")

	c := center.New(nil)
	c.SetWorkspace(ws)
	c.AddTab(&center.Tab{Name: "old-tab", Assistant: "claude", Workspace: ws})
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

	// The older capture's command goes first and blocks inside Update,
	// holding this workspace's write lock.
	oldCmd := app.handlePersistDebounce(persistDebounceMsg{token: app.lifecycle.persistToken})
	if oldCmd == nil {
		t.Fatal("expected a save command")
	}
	oldDone := make(chan tea.Msg)
	go func() { oldDone <- oldCmd() }()
	<-entered

	// Shutdown flush captures the newer state and must wait behind the
	// blocked write rather than racing it.
	app.center.CloseActiveTab()
	app.center.AddTab(&center.Tab{Name: "new-tab", Assistant: "claude", Workspace: ws})
	flushDone := make(chan struct{})
	go func() { app.persistAllWorkspacesNow(); close(flushDone) }()

	close(release)
	if err := <-oldDone; err != nil {
		t.Fatalf("older command returned %v, want nil", err)
	}
	<-flushDone

	last := fake.LastSaved()
	if last == nil {
		t.Fatal("no record was persisted")
	}
	if len(last.OpenTabs) != 1 || last.OpenTabs[0].Name != "new-tab" {
		t.Fatalf("final committed tabs = %v, want the newer capture", last.OpenTabs)
	}
}
