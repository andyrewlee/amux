package app

import (
	"errors"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/center"
)

type failingTombstoneWorkspaceStore struct {
	workspace *data.Workspace
	deleteErr error
	markCount int
}

func (s *failingTombstoneWorkspaceStore) ListByRepo(string) ([]*data.Workspace, error) {
	return nil, nil
}

func (s *failingTombstoneWorkspaceStore) ListByRepoIncludingArchived(string) ([]*data.Workspace, error) {
	return nil, nil
}

func (s *failingTombstoneWorkspaceStore) LoadMetadataFor(*data.Workspace) (bool, error) {
	return false, nil
}
func (s *failingTombstoneWorkspaceStore) UpsertFromDiscovery(*data.Workspace) error { return nil }
func (s *failingTombstoneWorkspaceStore) Save(*data.Workspace) error                { return nil }
func (s *failingTombstoneWorkspaceStore) Delete(data.WorkspaceID) error             { return s.deleteErr }
func (s *failingTombstoneWorkspaceStore) Rename(data.WorkspaceID, string) error     { return nil }

func (s *failingTombstoneWorkspaceStore) ResolvedDefaultAssistant() string {
	return data.DefaultAssistant
}

func (s *failingTombstoneWorkspaceStore) MarkDeleting(data.WorkspaceID) error {
	s.markCount++
	return nil
}

func (s *failingTombstoneWorkspaceStore) IsDeleting(id data.WorkspaceID) bool {
	return s.workspace != nil && id == s.workspace.ID()
}
func (s *failingTombstoneWorkspaceStore) ClearDeleting(data.WorkspaceID) error { return nil }

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

// TestFinishInterruptedDelete_SkipsDirlessTombstonedWhenMetadataDeleteFails
// proves a transient metadata cleanup failure does not surface a ghost
// workspace once the tombstone says delete passed validation and the worktree is
// gone.
func TestFinishInterruptedDelete_SkipsDirlessTombstonedWhenMetadataDeleteFails(t *testing.T) {
	ws := data.NewWorkspace("gone", "feature", "main", "/repo", "/repo/.amux/gone")
	store := &failingTombstoneWorkspaceStore{
		workspace: ws,
		deleteErr: errors.New("metadata busy"),
	}
	svc := newWorkspaceService(nil, store, nil, "")

	if !svc.finishInterruptedDelete(ws) {
		t.Fatal("expected recovery to suppress a dir-less tombstoned workspace")
	}
	if store.markCount == 0 {
		t.Fatal("expected failed cleanup to preserve the tombstone for a later retry")
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

// TestPersistAllWorkspacesNow_SkipsDeleteInFlight proves shutdown persist does
// not re-create metadata for any delete-in-flight workspace, while still saving
// a sibling that is not being deleted.
func TestPersistAllWorkspacesNow_SkipsDeleteInFlight(t *testing.T) {
	store := &recordingWorkspaceStore{}
	svc := newWorkspaceService(nil, store, nil, "")

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
			phases: map[string]lifecyclePhase{string(gone.ID()): lifecycleDeleting, string(live.ID()): lifecycleDeleting},
		},
	}

	app.persistAllWorkspacesNow()

	for _, id := range store.saved() {
		if id == string(gone.ID()) {
			t.Fatalf("dir-less delete-in-flight workspace must not be re-saved, saved=%v", store.saved())
		}
		if id == string(live.ID()) {
			t.Fatalf("dir-present delete-in-flight workspace must not be re-saved, saved=%v", store.saved())
		}
	}
	foundKept := false
	for _, id := range store.saved() {
		if id == string(kept.ID()) {
			foundKept = true
		}
	}
	if !foundKept {
		t.Fatalf("non-deleting sibling workspace must still be saved, saved=%v", store.saved())
	}
}
