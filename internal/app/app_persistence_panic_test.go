package app

import (
	"io/fs"
	"sort"
	"testing"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/testutil"
	"github.com/andyrewlee/amux/internal/ui/center"
)

// TestHandlePersistDebouncePanicReDirtiesUnsaved proves a panic mid-batch in
// the debounced persist save does not silently drop cleared dirty markers:
// the save closure runs under panicAsMsg, whose fallback emits a
// persistSaveFailedMsg covering exactly the panicking snapshot and every
// not-yet-attempted one — snapshots committed before the panic must NOT be
// re-dirtied (re-dirtying a committed save would trigger a spurious re-save
// and local-save marker drift).
func TestHandlePersistDebouncePanicReDirtiesUnsaved(t *testing.T) {
	wsPanic := data.NewWorkspace("panic", "panic", "main", "/repo", "/repo/panic")
	wsB := data.NewWorkspace("other-b", "other-b", "main", "/repo", "/repo/other-b")
	wsC := data.NewWorkspace("other-c", "other-c", "main", "/repo", "/repo/other-c")
	all := []*data.Workspace{wsPanic, wsB, wsC}

	panicID := string(wsPanic.MetadataID())
	var attempted []string
	store := &testutil.FakeWorkspaceStore{
		UpdateFunc: func(id data.WorkspaceID, fn func(ws *data.Workspace) (bool, error)) error {
			attempted = append(attempted, string(id))
			if string(id) == panicID {
				panic("store update boom")
			}
			// Missing record → the service's intentional whole-create path
			// runs store.Save, which the fake records as a commit.
			return fs.ErrNotExist
		},
	}
	svc := workspacesvc.New(nil, store, nil, "")

	c := center.New(nil)
	project := data.Project{Name: "repo", Path: "/repo"}
	dirty := make(map[string]bool, len(all))
	for _, ws := range all {
		c.SetWorkspace(ws)
		c.AddTab(&center.Tab{Name: "agent", Assistant: "claude", Workspace: ws})
		project.Workspaces = append(project.Workspaces, *ws)
		dirty[string(ws.ID())] = true
	}

	app := &App{
		center:           c,
		workspaceService: svc,
		projects:         []data.Project{project},
		lifecycle: workspaceLifecycleState{
			persistToken: 1,
			dirty:        dirty,
			localSavesAt: make(map[string]localWorkspaceSaveMarker),
		},
	}

	cmd := app.handlePersistDebounce(persistDebounceMsg{token: 1})
	if cmd == nil {
		t.Fatal("expected a save command for the dirty workspaces")
	}
	if len(app.lifecycle.dirty) != 0 {
		t.Fatalf("expected dirty markers cleared at snapshot time, got %v", app.lifecycle.dirty)
	}

	// Pre-fix, the panic escapes to SafeCmd's generic error — no
	// persistSaveFailedMsg — and the cleared markers are dropped forever.
	msg := cmd()
	failMsg, ok := msg.(persistSaveFailedMsg)
	if !ok {
		t.Fatalf("expected persistSaveFailedMsg on panic, got %T", msg)
	}

	// Expected failures: the panicking snapshot plus every snapshot never
	// attempted (the dirty-map iteration order is intentionally not
	// controlled — attempted tells us which commits preceded the panic).
	want := map[string]bool{panicID: true}
	seen := map[string]bool{}
	for _, ws := range all {
		seen[string(ws.MetadataID())] = true
	}
	for _, id := range attempted {
		delete(seen, id)
	}
	for id := range seen {
		want[id] = true
	}
	// The panicking workspace must always be reported.
	gotIDs := append([]string(nil), failMsg.workspaceIDs...)
	sort.Strings(gotIDs)
	if len(failMsg.workspaceIDs) == 0 {
		t.Fatal("expected failed workspace IDs, got none")
	}
	got := make(map[string]bool, len(failMsg.workspaceIDs))
	for _, id := range failMsg.workspaceIDs {
		if id == "" {
			continue
		}
		got[id] = true
	}
	if !got[panicID] {
		t.Fatalf("expected panicking workspace %s in failed IDs %v", panicID, gotIDs)
	}
	if len(got) != len(want) {
		t.Fatalf("failed IDs = %v, want %v (attempted %v)", gotIDs, want, attempted)
	}
	for id := range want {
		if !got[id] {
			t.Fatalf("failed IDs = %v, missing %v (attempted %v)", gotIDs, id, attempted)
		}
	}
	// Nothing already committed may be reported: attempted non-panic IDs are
	// committed saves and re-dirtying them would be a spurious re-save.
	for _, id := range attempted {
		if id != panicID && got[id] {
			t.Fatalf("committed snapshot %s wrongly reported failed: %v", id, gotIDs)
		}
	}

	// The Update-loop handler re-dirties exactly the reported workspaces.
	reDirtyCmd := app.handlePersistSaveFailed(failMsg)
	if reDirtyCmd == nil {
		t.Fatal("expected a rescheduled debounce command after save failure")
	}
	for _, ws := range all {
		wsID := string(ws.ID())
		if got[string(ws.MetadataID())] && !app.lifecycle.dirty[wsID] {
			t.Fatalf("expected %s re-dirtied after panic", wsID)
		}
	}
}
