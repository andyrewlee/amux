package data

import (
	"testing"
)

// TestWorkspaceStore_SaveIfAbsent_ReturnsConcurrentWinner pins the
// create-if-absent contract: a record already committed for the identity
// wins — the sparse candidate is not written and the stored record comes
// back for the caller to merge or adopt.
func TestWorkspaceStore_SaveIfAbsent_ReturnsConcurrentWinner(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())

	winner := &Workspace{
		Name:   "kept",
		Repo:   "/repo",
		Root:   "/root/ws",
		Env:    map[string]string{"X": "winner"},
		Branch: "main",
	}
	if err := store.Save(winner); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	candidate := &Workspace{Repo: "/repo", Root: "/root/ws", Branch: "feature"}
	stored, created, err := store.SaveIfAbsent(candidate)
	if err != nil {
		t.Fatalf("SaveIfAbsent() error = %v", err)
	}
	if created {
		t.Fatal("SaveIfAbsent reported created against an existing record")
	}
	if stored == nil || stored.Env["X"] != "winner" {
		t.Fatalf("returned stored = %+v, want the committed record", stored)
	}
	loaded, err := store.Load(winner.ID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Env["X"] != "winner" || loaded.Name != "kept" {
		t.Fatalf("committed record was clobbered: %+v", loaded)
	}
}

// TestWorkspaceStore_SaveIfAbsent_CreatesWhenAbsent covers the other half:
// no competing record → the candidate is written and reported created.
func TestWorkspaceStore_SaveIfAbsent_CreatesWhenAbsent(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())

	candidate := &Workspace{Repo: "/repo", Root: "/root/ws", Branch: "feature"}
	stored, created, err := store.SaveIfAbsent(candidate)
	if err != nil {
		t.Fatalf("SaveIfAbsent() error = %v", err)
	}
	if !created || stored == nil {
		t.Fatalf("SaveIfAbsent = (%v, %v), want the created record", stored, created)
	}
	if _, err := store.Load(candidate.ID()); err != nil {
		t.Fatalf("Load() after create error = %v", err)
	}
}

// TestWorkspaceStore_UpsertFromDiscovery_MergesConcurrentCreate is the
// lost-update regression: the unlocked lookup misses, a concurrent create
// commits in the window, and the upsert must merge the winner rather than
// overwrite it with the sparse discovered snapshot.
func TestWorkspaceStore_UpsertFromDiscovery_MergesConcurrentCreate(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())

	winner := &Workspace{
		Name:     "concurrent",
		Repo:     "/repo",
		Root:     "/root/ws",
		Branch:   "main",
		Env:      map[string]string{"TOKEN": "kept"},
		OpenTabs: []TabInfo{{Assistant: "claude", Name: "tab"}},
	}
	upsertAbsentProbe = func() {
		if err := store.Save(winner); err != nil {
			t.Errorf("probe Save() error = %v", err)
		}
	}
	defer func() { upsertAbsentProbe = nil }()

	discovered := &Workspace{Repo: "/repo", Root: "/root/ws", Branch: "feature"}
	if err := store.UpsertFromDiscovery(discovered); err != nil {
		t.Fatalf("UpsertFromDiscovery() error = %v", err)
	}

	loaded, err := store.Load(winner.ID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Env["TOKEN"] != "kept" || len(loaded.OpenTabs) != 1 {
		t.Fatalf("concurrent record lost fields: %+v", loaded)
	}
	if loaded.Branch != "feature" {
		t.Fatalf("Branch = %q, want the discovered merge %q", loaded.Branch, "feature")
	}
}
