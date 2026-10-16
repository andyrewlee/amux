package data

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
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
	// User-authored data is never zeroed silently: Set preserves the
	// corrupt bytes to .bak and refuses, surfacing a distinguishable error.
	err := store.Set(repo, ScriptsConfig{Run: "make dev"})
	var corruptErr *CorruptStoreError
	if !errors.As(err, &corruptErr) {
		t.Fatalf("Set() over corrupt file = %v, want *CorruptStoreError", err)
	}
	raw, readErr := os.ReadFile(store.Path() + ".bak")
	if readErr != nil || string(raw) != "{not json" {
		t.Fatalf(".bak = %q, %v — corrupt bytes must be preserved", raw, readErr)
	}
	// The refused write left the primary absent, so a retry lands clean.
	if err := store.Set(repo, ScriptsConfig{Run: "make dev"}); err != nil {
		t.Fatalf("Set() retry after preservation error = %v", err)
	}
	if got := store.ForRepo(repo); got.Run != "make dev" {
		t.Fatalf("ForRepo() after recovery = %+v", got)
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

func TestProjectScriptStore_CorruptSetPreservesOccupiedBackup(t *testing.T) {
	// A second corruption while .bak is occupied refuses without destroying
	// either file — the registry's never-recover-a-stale-backup rule.
	dir := t.TempDir()
	store := NewProjectScriptStore(dir)
	if err := os.WriteFile(store.Path()+".bak", []byte("{first corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), []byte("{second corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := store.Set(t.TempDir(), ScriptsConfig{Run: "make dev"})
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

func TestProjectScriptStore_SetReadFailureIsNotCorruption(t *testing.T) {
	// A read failure that is not decode corruption refuses the write as-is —
	// the file may be perfectly healthy and must not be moved to .bak.
	dir := t.TempDir()
	store := NewProjectScriptStore(dir)
	ioErr := fs.ErrPermission
	store.readFile = func(string) ([]byte, error) { return nil, ioErr }
	err := store.Set(t.TempDir(), ScriptsConfig{Run: "make dev"})
	var corruptErr *CorruptStoreError
	if errors.As(err, &corruptErr) {
		t.Fatalf("Set() with read failure = %v, must not be CorruptStoreError", err)
	}
	if !errors.Is(err, ioErr) {
		t.Fatalf("Set() = %v, want the read failure surfaced", err)
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
	// Set refuses a newer-schema file via the version error, not the
	// corruption path — the bytes belong to a newer binary, so nothing is
	// moved to .bak and the refusal stays distinguishable.
	err := store.Set(t.TempDir(), ScriptsConfig{Run: "make dev"})
	var corruptErr *CorruptStoreError
	if errors.As(err, &corruptErr) {
		t.Fatalf("Set() on future schema = %v, must not be CorruptStoreError", err)
	}
	if !errors.Is(err, ErrUnsupportedSchemaVersion) {
		t.Fatalf("Set() on future schema = %v, want ErrUnsupportedSchemaVersion", err)
	}
	// And the file is not destroyed by either the failed read or the refused write.
	raw, _ := os.ReadFile(store.Path())
	if !strings.Contains(string(raw), "99") {
		t.Fatal("future-schema file must not be clobbered")
	}
	if _, statErr := os.Stat(store.Path() + ".bak"); !os.IsNotExist(statErr) {
		t.Fatal(".bak must not be created for a schema refusal")
	}
}
