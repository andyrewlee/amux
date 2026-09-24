package data

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Per-store schema-version coverage: every app-owned store writes a
// `version` stamp, reads v0 (pre-versioning) files identically, and fails
// closed on a version newer than it knows.

func TestRegistry_V0FileReadsUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	repo := t.TempDir()
	// v0 fixture: the exact pre-versioning shape.
	fixture := `{"projects":[{"name":"repo","path":"` + repo + `"}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(path)
	paths, err := reg.Load()
	if err != nil {
		t.Fatalf("Load(v0) error = %v", err)
	}
	if len(paths) != 1 || paths[0] != repo {
		t.Fatalf("Load(v0) = %v, want [%s]", paths, repo)
	}
}

func TestRegistry_V1RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	repo := t.TempDir()
	reg := NewRegistry(path)
	if err := reg.Save([]string{repo}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); !containsAll(got, `"version"`) {
		t.Fatalf("saved file missing version stamp: %s", got)
	}
	reloaded := NewRegistry(path)
	paths, err := reloaded.Load()
	if err != nil {
		t.Fatalf("Load(v1) error = %v", err)
	}
	if len(paths) != 1 || paths[0] != repo {
		t.Fatalf("Load(v1) = %v, want [%s]", paths, repo)
	}
}

func TestRegistry_UnknownVersionFailsClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	fixture := `{"version":99,"projects":[{"name":"repo","path":"/x"}]}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(path)
	if _, err := reg.Load(); err == nil {
		t.Fatal("Load(version:99) must error, not silently parse a newer format")
	}
}

func TestProjectEnv_V0FileReadsUnchanged(t *testing.T) {
	dir := t.TempDir()
	store := NewProjectEnvStore(dir)
	repo := t.TempDir()
	// v0 fixture: bare map keyed by normalized repo path.
	fixture := `{"` + NormalizePath(repo) + `":{"A":"1"}}`
	if err := os.WriteFile(store.Path(), []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := store.ForRepo(repo); got["A"] != "1" {
		t.Fatalf("ForRepo(v0) = %v, want A=1", got)
	}
}

func TestProjectEnv_V1RoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewProjectEnvStore(dir)
	repo := t.TempDir()
	if err := store.Set(repo, map[string]string{"A": "1"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); !containsAll(got, `"version"`, `"env"`) {
		t.Fatalf("saved file missing envelope: %s", got)
	}
	if got := NewProjectEnvStore(dir).ForRepo(repo); got["A"] != "1" {
		t.Fatalf("ForRepo(v1) = %v, want A=1", got)
	}
}

func TestProjectEnv_UnknownVersionDegradesToEmpty(t *testing.T) {
	dir := t.TempDir()
	store := NewProjectEnvStore(dir)
	repo := t.TempDir()
	fixture := `{"version":99,"env":{"/x":{"A":"1"}}}`
	if err := os.WriteFile(store.Path(), []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	// The store's degraded contract: a bad file yields an empty map, never
	// a panic or a permissive parse.
	if got := store.ForRepo(repo); len(got) != 0 {
		t.Fatalf("ForRepo(version:99) = %v, want empty (fail closed)", got)
	}
}

func TestWorkspaceStore_V0FileReadsUnchanged(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ws := NewWorkspace("ws", "feat", "main", t.TempDir(), t.TempDir())
	// Write a v0 fixture directly (no version key).
	id := ws.ID()
	dir := filepath.Dir(store.workspacePath(id))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture := `{"name":"ws","branch":"feat","repo":"` + ws.Repo + `","root":"` + ws.Root + `","shelved":true}`
	if err := os.WriteFile(store.workspacePath(id), []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load(v0) error = %v", err)
	}
	if !loaded.Shelved || loaded.Name != "ws" {
		t.Fatalf("Load(v0) = %+v", loaded)
	}
}

func TestWorkspaceStore_V1RoundTrip(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ws := NewWorkspace("ws", "feat", "main", t.TempDir(), t.TempDir())
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(store.workspacePath(ws.ID()))
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(string(raw), `"version"`) {
		t.Fatalf("saved workspace.json missing version stamp: %s", raw)
	}
	loaded, err := store.Load(ws.ID())
	if err != nil {
		t.Fatalf("Load(v1) error = %v", err)
	}
	if loaded.Name != "ws" {
		t.Fatalf("Load(v1) = %+v", loaded)
	}
}

func TestWorkspaceStore_UnknownVersionFailsClosed(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ws := NewWorkspace("ws", "feat", "main", t.TempDir(), t.TempDir())
	id := ws.ID()
	dir := filepath.Dir(store.workspacePath(id))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture := `{"version":99,"name":"ws","repo":"` + ws.Repo + `","root":"` + ws.Root + `"}`
	if err := os.WriteFile(store.workspacePath(id), []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(id); err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Load(version:99) must fail closed, got %v", err)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
