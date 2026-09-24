package data

import (
	"os"
	"testing"
)

func TestProjectEnvStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewProjectEnvStore(dir)
	repo := t.TempDir()

	if got := store.ForRepo(repo); len(got) != 0 {
		t.Fatalf("ForRepo() on empty store = %v, want empty", got)
	}
	if err := store.Set(repo, map[string]string{"A": "1", "B": "2"}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	got := store.ForRepo(repo)
	if got["A"] != "1" || got["B"] != "2" || len(got) != 2 {
		t.Fatalf("ForRepo() = %v, want A=1 B=2", got)
	}

	// A fresh store over the same file sees the persisted map.
	reloaded := NewProjectEnvStore(dir)
	if got := reloaded.ForRepo(repo); got["A"] != "1" {
		t.Fatalf("reloaded ForRepo() = %v, want persisted map", got)
	}
}

func TestProjectEnvStore_KeysAreNormalizedAndIsolated(t *testing.T) {
	dir := t.TempDir()
	store := NewProjectEnvStore(dir)
	repo := t.TempDir()
	other := t.TempDir()

	if err := store.Set(repo, map[string]string{"A": "1"}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if got := store.ForRepo(other); len(got) != 0 {
		t.Fatalf("ForRepo(other) = %v, want isolation", got)
	}
	// Normalized path aliases address the same project.
	if got := store.ForRepo(NormalizePath(repo)); got["A"] != "1" {
		t.Fatalf("ForRepo(normalized) = %v, want same project map", got)
	}
}

func TestProjectEnvStore_SetEmptyRemovesEntry(t *testing.T) {
	dir := t.TempDir()
	store := NewProjectEnvStore(dir)
	repo := t.TempDir()

	if err := store.Set(repo, map[string]string{"A": "1"}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := store.Set(repo, nil); err != nil {
		t.Fatalf("Set(nil) error = %v", err)
	}
	if got := store.ForRepo(repo); len(got) != 0 {
		t.Fatalf("ForRepo() after clear = %v, want empty", got)
	}
	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("file vanished instead of persisting an empty map")
	}
}

func TestProjectEnvStore_CorruptFileNeverBlocks(t *testing.T) {
	dir := t.TempDir()
	store := NewProjectEnvStore(dir)
	if err := os.WriteFile(store.Path(), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}
	if got := store.ForRepo(t.TempDir()); len(got) != 0 {
		t.Fatalf("ForRepo() on corrupt file = %v, want empty (spawn must not fail)", got)
	}
	// Set replaces the unparseable bytes wholesale.
	repo := t.TempDir()
	if err := store.Set(repo, map[string]string{"A": "1"}); err != nil {
		t.Fatalf("Set() over corrupt file error = %v", err)
	}
	if got := store.ForRepo(repo); got["A"] != "1" {
		t.Fatalf("ForRepo() = %v after recovery write", got)
	}
}

func TestProjectEnvStore_ReturnedMapIsACopy(t *testing.T) {
	dir := t.TempDir()
	store := NewProjectEnvStore(dir)
	repo := t.TempDir()
	if err := store.Set(repo, map[string]string{"A": "1"}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	got := store.ForRepo(repo)
	got["A"] = "mutated"
	if again := store.ForRepo(repo); again["A"] != "1" {
		t.Fatal("mutating the returned map changed the store")
	}
}

func TestProjectEnvStore_RejectsEmptyRepoPath(t *testing.T) {
	store := NewProjectEnvStore(t.TempDir())
	if err := store.Set("", map[string]string{"A": "1"}); err == nil {
		t.Fatal("Set(\"\") = nil, want error")
	}
	if len(store.ForRepo("")) != 0 {
		t.Fatal("ForRepo(\"\") non-empty")
	}
}
