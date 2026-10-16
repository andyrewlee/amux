package data

import (
	"errors"
	"io/fs"
	"os"
	"runtime"
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
	// User-authored data is never zeroed silently: Set preserves the
	// corrupt bytes to .bak and refuses, surfacing a distinguishable error.
	repo := t.TempDir()
	err := store.Set(repo, map[string]string{"A": "1"})
	var corruptErr *CorruptStoreError
	if !errors.As(err, &corruptErr) {
		t.Fatalf("Set() over corrupt file = %v, want *CorruptStoreError", err)
	}
	raw, readErr := os.ReadFile(store.Path() + ".bak")
	if readErr != nil || string(raw) != "{not json" {
		t.Fatalf(".bak = %q, %v — corrupt bytes must be preserved", raw, readErr)
	}
	if _, statErr := os.Stat(store.Path()); !os.IsNotExist(statErr) {
		t.Fatalf("primary still present after refusal (%v) — it must move to .bak", statErr)
	}
	if got := store.ForRepo(repo); len(got) != 0 {
		t.Fatalf("ForRepo() after refusal = %v, want empty", got)
	}
	// The refused write left the primary absent, so a retry lands clean.
	if err := store.Set(repo, map[string]string{"A": "1"}); err != nil {
		t.Fatalf("Set() retry after preservation error = %v", err)
	}
	if got := store.ForRepo(repo); got["A"] != "1" {
		t.Fatalf("ForRepo() = %v after recovery write", got)
	}
	// A clean write must not touch the preserved corrupt bytes (Windows'
	// fsatomic fallback reclaims the .bak slot, so only assert on POSIX).
	if runtime.GOOS != "windows" {
		raw, _ = os.ReadFile(store.Path() + ".bak")
		if string(raw) != "{not json" {
			t.Fatalf(".bak rewritten to %q — stale preservation must survive a clean write", raw)
		}
	}
}

func TestProjectEnvStore_CorruptSetPreservesOccupiedBackup(t *testing.T) {
	// A second corruption while .bak is occupied refuses without destroying
	// either file — the registry's never-recover-a-stale-backup rule.
	dir := t.TempDir()
	store := NewProjectEnvStore(dir)
	if err := os.WriteFile(store.Path()+".bak", []byte("{first corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), []byte("{second corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := store.Set(t.TempDir(), map[string]string{"A": "1"})
	var corruptErr *CorruptStoreError
	if !errors.As(err, &corruptErr) {
		t.Fatalf("Set() = %v, want *CorruptStoreError", err)
	}
	if !corruptErr.BackupOccupied || corruptErr.Preserved != "" {
		t.Fatalf("occupied-backup refusal = %+v, want BackupOccupied with no move", corruptErr)
	}
	if raw, _ := os.ReadFile(store.Path()); string(raw) != "{second corrupt" {
		t.Fatalf("primary = %q — corrupt file must be left in place when .bak is occupied", raw)
	}
	if raw, _ := os.ReadFile(store.Path() + ".bak"); string(raw) != "{first corrupt" {
		t.Fatalf(".bak = %q — earlier preservation must not be overwritten", raw)
	}
}

func TestProjectEnvStore_SetReadFailureIsNotCorruption(t *testing.T) {
	// A read failure that is not decode corruption (permissions, transient
	// I/O) refuses the write as-is — the file may be perfectly healthy and
	// must not be moved to .bak.
	dir := t.TempDir()
	store := NewProjectEnvStore(dir)
	ioErr := fs.ErrPermission
	store.readFile = func(string) ([]byte, error) { return nil, ioErr }
	err := store.Set(t.TempDir(), map[string]string{"A": "1"})
	var corruptErr *CorruptStoreError
	if errors.As(err, &corruptErr) {
		t.Fatalf("Set() with read failure = %v, must not be CorruptStoreError", err)
	}
	if !errors.Is(err, ioErr) {
		t.Fatalf("Set() = %v, want the read failure surfaced", err)
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
