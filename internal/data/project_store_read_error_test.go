package data

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
	"testing"
)

// injectReadError installs a per-instance failing reader that reports it
// was invoked, then returns the cleanup restoring os.ReadFile behavior.
func injectReadError(set func(func(string) ([]byte, error)), err error) *bool {
	called := new(bool)
	set(func(string) ([]byte, error) {
		*called = true
		return nil, err
	})
	return called
}

// TestProjectEnvStore_ReadErrorPreservesState: a read failure that is not a
// missing file must refuse Set — the file may hold other projects' env that
// a blind empty-map write would erase. Covers add and delete writes,
// permission/EIO/arbitrary errors, byte preservation, and tolerant lookups.
func TestProjectEnvStore_ReadErrorPreservesState(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"permission", &os.PathError{Op: "read", Path: "project-env.json", Err: fs.ErrPermission}},
		{"EIO", &os.PathError{Op: "read", Path: "project-env.json", Err: syscall.EIO}},
		{"arbitrary", errors.New("injected reader failure")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			store := NewProjectEnvStore(dir)
			repoA, repoB, repoC := t.TempDir(), t.TempDir(), t.TempDir()
			if err := store.Set(repoA, map[string]string{"A": "1"}); err != nil {
				t.Fatal(err)
			}
			if err := store.Set(repoB, map[string]string{"B": "2"}); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(store.Path())
			if err != nil {
				t.Fatal(err)
			}

			called := injectReadError(func(f func(string) ([]byte, error)) { store.readFile = f }, tc.err)

			if err := store.Set(repoC, map[string]string{"C": "3"}); !errors.Is(err, tc.err) {
				t.Fatalf("Set(add) error = %v, want cause %v", err, tc.err)
			}
			if err := store.Set(repoA, map[string]string{}); !errors.Is(err, tc.err) {
				t.Fatalf("Set(delete) error = %v, want cause %v", err, tc.err)
			}
			if !*called {
				t.Fatal("injected reader was not invoked")
			}
			// Read-only lookups stay tolerant and must not mutate the file.
			if got := store.ForRepo(repoA); len(got) != 0 {
				t.Fatalf("ForRepo under read error = %v, want empty", got)
			}
			after, err := os.ReadFile(store.Path())
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatalf("read failure must preserve bytes\nbefore: %s\nafter:  %s", before, after)
			}
			// Once the read heals, the untouched entries are still there.
			store.readFile = nil
			if got := store.ForRepo(repoA); got["A"] != "1" {
				t.Fatalf("repoA entry lost under read failure: %v", got)
			}
			if got := store.ForRepo(repoB); got["B"] != "2" {
				t.Fatalf("repoB entry lost under read failure: %v", got)
			}
		})
	}
}

// TestProjectScriptStore_ReadErrorPreservesState is the script-store
// counterpart — same refusal contract over its versioned envelope.
func TestProjectScriptStore_ReadErrorPreservesState(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"permission", &os.PathError{Op: "read", Path: "project-scripts.json", Err: fs.ErrPermission}},
		{"EIO", &os.PathError{Op: "read", Path: "project-scripts.json", Err: syscall.EIO}},
		{"arbitrary", errors.New("injected reader failure")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			store := NewProjectScriptStore(dir)
			repoA, repoB, repoC := t.TempDir(), t.TempDir(), t.TempDir()
			if err := store.Set(repoA, ScriptsConfig{Setup: "make init"}); err != nil {
				t.Fatal(err)
			}
			if err := store.Set(repoB, ScriptsConfig{Run: "make dev"}); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(store.Path())
			if err != nil {
				t.Fatal(err)
			}

			called := injectReadError(func(f func(string) ([]byte, error)) { store.readFile = f }, tc.err)

			if err := store.Set(repoC, ScriptsConfig{Archive: "make clean"}); !errors.Is(err, tc.err) {
				t.Fatalf("Set(add) error = %v, want cause %v", err, tc.err)
			}
			if err := store.Set(repoA, ScriptsConfig{}); !errors.Is(err, tc.err) {
				t.Fatalf("Set(delete) error = %v, want cause %v", err, tc.err)
			}
			if !*called {
				t.Fatal("injected reader was not invoked")
			}
			if got := store.ForRepo(repoA); got != (ScriptsConfig{}) {
				t.Fatalf("ForRepo under read error = %v, want zero config", got)
			}
			after, err := os.ReadFile(store.Path())
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatalf("read failure must preserve bytes\nbefore: %s\nafter:  %s", before, after)
			}
			store.readFile = nil
			if got := store.ForRepo(repoA); got.Setup != "make init" {
				t.Fatalf("repoA entry lost under read failure: %v", got)
			}
			if got := store.ForRepo(repoB); got.Run != "make dev" {
				t.Fatalf("repoB entry lost under read failure: %v", got)
			}
		})
	}
}
