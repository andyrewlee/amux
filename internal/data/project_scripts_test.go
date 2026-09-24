package data

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectScriptStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewProjectScriptStore(dir)
	repo := t.TempDir()

	if got := store.ForRepo(repo); got != (ScriptsConfig{}) {
		t.Fatalf("ForRepo() on empty store = %+v, want zero", got)
	}
	want := ScriptsConfig{Setup: "make init", Run: "make dev", Archive: "make clean", OnDone: "make done"}
	if err := store.Set(repo, want); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if got := store.ForRepo(repo); got != want {
		t.Fatalf("ForRepo() = %+v, want %+v", got, want)
	}

	// A fresh store over the same file sees the persisted defaults.
	reloaded := NewProjectScriptStore(dir)
	if got := reloaded.ForRepo(repo); got != want {
		t.Fatalf("reloaded ForRepo() = %+v, want persisted %+v", got, want)
	}
}

func TestProjectScriptStore_EmptyConfigDeletes(t *testing.T) {
	dir := t.TempDir()
	store := NewProjectScriptStore(dir)
	repo := t.TempDir()

	if err := store.Set(repo, ScriptsConfig{Run: "make dev"}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	// Storing an all-empty config removes the entry — an empty layer is
	// absence, not override-to-empty.
	if err := store.Set(repo, ScriptsConfig{}); err != nil {
		t.Fatalf("Set(empty) error = %v", err)
	}
	if got := store.ForRepo(repo); got != (ScriptsConfig{}) {
		t.Fatalf("ForRepo() after empty Set = %+v, want zero", got)
	}
}

func TestProjectScriptStore_KeysAreNormalizedAndIsolated(t *testing.T) {
	dir := t.TempDir()
	store := NewProjectScriptStore(dir)
	repo := t.TempDir()
	other := t.TempDir()

	if err := store.Set(repo, ScriptsConfig{Run: "make dev"}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if got := store.ForRepo(other); got != (ScriptsConfig{}) {
		t.Fatalf("ForRepo(other) = %+v, want zero (isolation)", got)
	}
	// Trailing-slash form of the same path resolves to the same entry.
	if got := store.ForRepo(repo + string(filepath.Separator)); got.Run != "make dev" {
		t.Fatalf("ForRepo(trailing slash) = %+v, want normalized match", got)
	}
}

func TestProjectScriptStore_CorruptFileReadsEmpty(t *testing.T) {
	dir := t.TempDir()
	store := NewProjectScriptStore(dir)
	if err := os.WriteFile(store.Path(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	if got := store.ForRepo(repo); got != (ScriptsConfig{}) {
		t.Fatalf("ForRepo() on corrupt file = %+v, want zero", got)
	}
	// Set replaces the corrupt file wholesale.
	if err := store.Set(repo, ScriptsConfig{Run: "make dev"}); err != nil {
		t.Fatalf("Set() over corrupt file error = %v", err)
	}
	if got := store.ForRepo(repo); got.Run != "make dev" {
		t.Fatalf("ForRepo() after recovery = %+v", got)
	}
}

func TestProjectScriptStore_FutureVersionRejected(t *testing.T) {
	dir := t.TempDir()
	store := NewProjectScriptStore(dir)
	if err := os.WriteFile(store.Path(), []byte(`{"version": 99, "scripts": {}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := store.ForRepo(t.TempDir()); got != (ScriptsConfig{}) {
		t.Fatalf("ForRepo() on future schema = %+v, want zero", got)
	}
	// And the file is not destroyed by the failed read.
	raw, _ := os.ReadFile(store.Path())
	if !strings.Contains(string(raw), "99") {
		t.Fatal("future-schema file must not be clobbered by a read")
	}
}
