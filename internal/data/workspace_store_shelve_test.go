package data

import (
	"testing"
	"time"
)

func TestWorkspaceStore_ShelvedRoundTrips(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	repo := t.TempDir()
	ws := &Workspace{
		Name: "feat", Repo: repo, Root: repo + "/ws",
		Archived: true, ArchivedAt: time.Now(), Shelved: true,
	}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := store.Load(ws.ID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !loaded.Shelved || !loaded.Archived {
		t.Fatalf("loaded Archived=%v Shelved=%v, want both true", loaded.Archived, loaded.Shelved)
	}
}

func TestWorkspaceStore_PruneSkipsShelved(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	repo := t.TempDir()
	managed := t.TempDir()

	// A shelved record: archived, missing root, inside the managed root —
	// it must survive BOTH the archived-retention and missing-root reasons.
	shelved := &Workspace{
		Name: "shelf", Repo: repo, Root: managed + "/repo/shelf",
		Archived: true, ArchivedAt: time.Now().Add(-30 * 24 * time.Hour), Shelved: true,
	}
	// An accidental archive at the same age: expires.
	accidental := &Workspace{
		Name: "gone", Repo: repo, Root: managed + "/repo/gone",
		Archived: true, ArchivedAt: time.Now().Add(-30 * 24 * time.Hour),
	}
	// A plain missing-root live record past the orphan grace: expires.
	missing := &Workspace{
		Name: "lost", Repo: repo, Root: managed + "/repo/lost",
		Created: time.Now().Add(-2 * time.Hour),
	}
	for _, ws := range []*Workspace{shelved, accidental, missing} {
		if err := store.Save(ws); err != nil {
			t.Fatalf("Save(%s) error = %v", ws.Name, err)
		}
	}

	// The orphan grace keys off the metadata file's mtime (just written), so
	// shrink it to force the missing-root evaluation; the shelved record is
	// excluded by its flag regardless of either grace.
	result, err := store.PruneStale(WorkspacePruneOptions{
		RegisteredRepos:   []string{repo},
		ManagedRoot:       managed,
		Now:               time.Now(),
		OrphanGracePeriod: time.Nanosecond,
		ArchivedRetention: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("PruneStale() error = %v", err)
	}
	if result.ArchivedRemoved != 1 {
		t.Fatalf("ArchivedRemoved = %d, want 1 (the accidental archive)", result.ArchivedRemoved)
	}
	if result.MissingRootRemoved != 1 {
		t.Fatalf("MissingRootRemoved = %d, want 1 (the missing live root)", result.MissingRootRemoved)
	}
	kept, err := store.Load(shelved.ID())
	if err != nil || kept == nil {
		t.Fatalf("shelved record was pruned (err=%v)", err)
	}
	if !kept.Shelved {
		t.Fatal("shelved record lost its Shelved flag")
	}
}

func TestWorkspaceStore_DiscoveryClearsShelved(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	repo := t.TempDir()
	root := t.TempDir() // worktree exists again — discovery found it
	ws := &Workspace{
		Name: "feat", Repo: repo, Root: root,
		Archived: true, ArchivedAt: time.Now(), Shelved: true,
	}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	discovered := &Workspace{Name: "feat", Repo: repo, Root: root, Branch: "feat"}
	if err := store.UpsertFromDiscovery(discovered); err != nil {
		t.Fatalf("UpsertFromDiscovery() error = %v", err)
	}
	loaded, err := store.Load(ws.ID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Archived || loaded.Shelved {
		t.Fatalf("discovery must un-archive and un-shelve, got Archived=%v Shelved=%v", loaded.Archived, loaded.Shelved)
	}
}
