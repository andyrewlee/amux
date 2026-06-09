package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/center"
)

// TestFinishInterruptedDelete_RemovesDirlessTombstoned proves the recovery pass
// finishes a delete whose tombstone survived but whose worktree is already gone,
// removing the metadata instead of surfacing a ghost.
func TestFinishInterruptedDelete_RemovesDirlessTombstoned(t *testing.T) {
	store := data.NewWorkspaceStore(t.TempDir())
	svc := newWorkspaceService(nil, store, nil, "")

	// Worktree root deliberately does not exist (simulating a crash after the
	// worktree was removed but before the metadata was).
	ws := data.NewWorkspace("gone", "feature", "main", "/repo", "/repo/.amux/gone")
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.MarkDeleting(ws.ID()); err != nil {
		t.Fatalf("MarkDeleting: %v", err)
	}

	if !svc.finishInterruptedDelete(ws) {
		t.Fatal("expected recovery to finish the interrupted delete")
	}
	if _, err := store.Load(ws.ID()); err == nil {
		t.Fatal("expected metadata removed by recovery")
	}
}

// TestFinishInterruptedDelete_KeepsTombstonedWithLiveWorktree proves a tombstone
// whose worktree still exists (a delete that failed before removing it) is NOT
// finished — the workspace stays usable.
func TestFinishInterruptedDelete_KeepsTombstonedWithLiveWorktree(t *testing.T) {
	store := data.NewWorkspaceStore(t.TempDir())
	svc := newWorkspaceService(nil, store, nil, "")

	wsRoot := t.TempDir() // worktree still present
	ws := data.NewWorkspace("live", "feature", "main", "/repo", wsRoot)
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.MarkDeleting(ws.ID()); err != nil {
		t.Fatalf("MarkDeleting: %v", err)
	}

	if svc.finishInterruptedDelete(ws) {
		t.Fatal("recovery must not finish a delete whose worktree still exists")
	}
	if _, err := store.Load(ws.ID()); err != nil {
		t.Fatalf("metadata must survive: %v", err)
	}
}

// TestPersistAllWorkspacesNow_SkipsDirlessDeleteInFlight proves shutdown persist
// does not re-create metadata for a delete-in-flight workspace whose worktree is
// already gone (which would resurrect a ghost), while still saving a sibling.
func TestPersistAllWorkspacesNow_SkipsDirlessDeleteInFlight(t *testing.T) {
	store := &recordingWorkspaceStore{}
	svc := newWorkspaceService(nil, store, nil, "")

	gone := data.NewWorkspace("gone", "feature", "main", "/repo", "/repo/.amux/gone-missing")
	live := data.NewWorkspace("live", "feature", "main", "/repo", t.TempDir())

	c := center.New(nil)
	for _, ws := range []*data.Workspace{gone, live} {
		c.SetWorkspace(ws)
		c.AddTab(&center.Tab{Name: "agent", Assistant: "claude", Workspace: ws})
	}

	app := &App{
		center:           c,
		workspaceService: svc,
		projects: []data.Project{{
			Name: "repo", Path: "/repo",
			Workspaces: []data.Workspace{*gone, *live},
		}},
		dirtyWorkspaces:      make(map[string]bool),
		deletingWorkspaceIDs: map[string]bool{string(gone.ID()): true, string(live.ID()): true},
	}

	app.persistAllWorkspacesNow()

	for _, id := range store.saved() {
		if id == string(gone.ID()) {
			t.Fatalf("dir-less delete-in-flight workspace must not be re-saved, saved=%v", store.saved())
		}
	}
	foundLive := false
	for _, id := range store.saved() {
		if id == string(live.ID()) {
			foundLive = true
		}
	}
	if !foundLive {
		t.Fatalf("dir-present delete-in-flight workspace must still be saved, saved=%v", store.saved())
	}
}
