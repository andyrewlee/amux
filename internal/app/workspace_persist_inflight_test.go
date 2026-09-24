package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/testutil"
	"github.com/andyrewlee/amux/internal/ui/center"
)

// TestPersistAllWorkspacesNow_SkipsMutationInFlight proves shutdown persist does
// not re-create metadata for any mutation-in-flight workspace, while still saving
// a sibling that is not being deleted.
func TestPersistAllWorkspacesNow_SkipsMutationInFlight(t *testing.T) {
	store := &testutil.FakeWorkspaceStore{}
	svc := workspacesvc.New(nil, store, nil, "")

	gone := data.NewWorkspace("gone", "feature", "main", "/repo", "/repo/.amux/gone-missing")
	live := data.NewWorkspace("live", "feature", "main", "/repo", t.TempDir())
	kept := data.NewWorkspace("kept", "feature", "main", "/repo", t.TempDir())

	c := center.New(nil)
	for _, ws := range []*data.Workspace{gone, live, kept} {
		c.SetWorkspace(ws)
		c.AddTab(&center.Tab{Name: "agent", Assistant: "claude", Workspace: ws})
	}

	app := &App{
		center:           c,
		workspaceService: svc,
		projects: []data.Project{{
			Name: "repo", Path: "/repo",
			Workspaces: []data.Workspace{*gone, *live, *kept},
		}},
		lifecycle: workspaceLifecycleState{
			dirty:  make(map[string]bool),
			phases: map[string]lifecyclePhase{string(gone.ID()): lifecycleMutating, string(live.ID()): lifecycleMutating},
		},
	}

	app.persistAllWorkspacesNow()

	for _, id := range store.SavedIDs() {
		if id == string(gone.ID()) {
			t.Fatalf("dir-less mutation-in-flight workspace must not be re-saved, saved=%v", store.SavedIDs())
		}
		if id == string(live.ID()) {
			t.Fatalf("dir-present mutation-in-flight workspace must not be re-saved, saved=%v", store.SavedIDs())
		}
	}
	foundKept := false
	for _, id := range store.SavedIDs() {
		if id == string(kept.ID()) {
			foundKept = true
		}
	}
	if !foundKept {
		t.Fatalf("non-mutating sibling workspace must still be saved, saved=%v", store.SavedIDs())
	}
}
