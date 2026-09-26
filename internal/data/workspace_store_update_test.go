package data

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestWorkspaceStore_Update_CallbackSeesFreshRecord pins the core guarantee:
// the callback mutates the record as committed on disk inside the lock, not
// whatever snapshot the caller happens to hold.
func TestWorkspaceStore_Update_CallbackSeesFreshRecord(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ws := &Workspace{Name: "ws", Branch: "b", Repo: "/r", Root: "/rt"}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	// A writer lands a rename before the second writer's Update runs.
	if err := store.Rename(ws.ID(), "renamed-on-disk"); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	var sawName string
	err := store.Update(ws.ID(), func(fresh *Workspace) (bool, error) {
		sawName = fresh.Name
		return false, nil
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if sawName != "renamed-on-disk" {
		t.Fatalf("callback saw Name=%q, want the committed rename", sawName)
	}
}

// TestWorkspaceStore_Update_NoChangeWritesNothing verifies a false changed
// result performs no disk write — the file bytes must be identical.
func TestWorkspaceStore_Update_NoChangeWritesNothing(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ws := &Workspace{Name: "ws", Branch: "b", Repo: "/r", Root: "/rt"}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	before, err := os.ReadFile(filepath.Join(store.root, string(ws.ID()), "workspace.json"))
	if err != nil {
		t.Fatalf("read before: %v", err)
	}
	called := false
	if err := store.Update(ws.ID(), func(fresh *Workspace) (bool, error) {
		called = true
		return false, nil
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !called {
		t.Fatal("Update did not invoke the callback")
	}
	after, err := os.ReadFile(filepath.Join(store.root, string(ws.ID()), "workspace.json"))
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("changed=false Update still rewrote the record")
	}
}

// TestWorkspaceStore_Update_CallbackErrorAborts verifies a callback error
// propagates and leaves the committed record untouched.
func TestWorkspaceStore_Update_CallbackErrorAborts(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ws := &Workspace{Name: "ws", Branch: "b", Repo: "/r", Root: "/rt"}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	sentinel := errors.New("callback failed")
	err := store.Update(ws.ID(), func(fresh *Workspace) (bool, error) {
		fresh.Name = "must-not-persist"
		return true, sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Update() error = %v, want the callback error", err)
	}
	loaded, err := store.Load(ws.ID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Name != "ws" {
		t.Fatalf("failed callback still persisted Name=%q", loaded.Name)
	}
}

// TestWorkspaceStore_Update_RejectsIdentityMutation verifies a callback that
// changes Repo/Root aborts the write rather than silently remapping the
// record.
func TestWorkspaceStore_Update_RejectsIdentityMutation(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ws := &Workspace{Name: "ws", Branch: "b", Repo: "/r", Root: "/rt"}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	err := store.Update(ws.ID(), func(fresh *Workspace) (bool, error) {
		fresh.Repo = "/elsewhere"
		return true, nil
	})
	if err == nil {
		t.Fatal("Update with Repo mutation must fail")
	}
	loaded, lerr := store.Load(ws.ID())
	if lerr != nil {
		t.Fatalf("Load() error = %v", lerr)
	}
	if loaded.Repo != "/r" {
		t.Fatalf("identity mutation persisted Repo=%q", loaded.Repo)
	}
}

// TestWorkspaceStore_Update_RejectsSchemaBump verifies a callback cannot bump
// the schema to a version this binary cannot read.
func TestWorkspaceStore_Update_RejectsSchemaBump(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ws := &Workspace{Name: "ws", Branch: "b", Repo: "/r", Root: "/rt"}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	err := store.Update(ws.ID(), func(fresh *Workspace) (bool, error) {
		fresh.Version = workspaceFileVersion + 1
		return true, nil
	})
	if err == nil {
		t.Fatal("Update bumping the schema version must fail")
	}
}

// TestWorkspaceStore_Update_MissingRecord verifies Update never creates a
// record — a workspace with no metadata gets fs.ErrNotExist, which callers
// (e.g. the tab-persistence create fallback) branch on.
func TestWorkspaceStore_Update_MissingRecord(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ws := &Workspace{Name: "ws", Branch: "b", Repo: "/r", Root: "/rt"}
	err := store.Update(ws.ID(), func(fresh *Workspace) (bool, error) {
		fresh.Name = "created"
		return true, nil
	})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Update on a missing record = %v, want fs.ErrNotExist", err)
	}
	if _, lerr := store.Load(ws.ID()); !errors.Is(lerr, fs.ErrNotExist) {
		t.Fatal("Update resurrected a record that never existed")
	}
}

// TestWorkspaceStore_Update_ConcurrentFieldWritesPreserveEachOther is the
// regression the primitive exists for: two writers mutating different fields
// in parallel must both land — a load+Save pair loses one of them. Run under
// -race via the package suite.
func TestWorkspaceStore_Update_ConcurrentFieldWritesPreserveEachOther(t *testing.T) {
	const rounds = 40
	for round := 0; round < rounds; round++ {
		store := NewWorkspaceStore(t.TempDir())
		ws := &Workspace{Name: "ws", Branch: "b", Repo: "/r", Root: "/rt"}
		if err := store.Save(ws); err != nil {
			t.Fatalf("round %d: Save() error = %v", round, err)
		}
		id := ws.ID()

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := store.Rename(id, "renamed"); err != nil {
				t.Errorf("round %d: Rename() error = %v", round, err)
			}
		}()
		go func() {
			defer wg.Done()
			if err := store.SetEnv(id, map[string]string{"K": "v"}); err != nil {
				t.Errorf("round %d: SetEnv() error = %v", round, err)
			}
		}()
		wg.Wait()

		loaded, err := store.Load(id)
		if err != nil {
			t.Fatalf("round %d: Load() error = %v", round, err)
		}
		if loaded.Name != "renamed" || loaded.Env["K"] != "v" {
			t.Fatalf("round %d: concurrent field writes clobbered each other (Name=%q Env=%v)", round, loaded.Name, loaded.Env)
		}
	}
}

// TestWorkspaceStore_FieldSetters_PreserveUnrelatedFields verifies the
// migrated setters change only their own field: a stale caller snapshot can
// no longer resurrect old Env/Scripts/tab state.
func TestWorkspaceStore_FieldSetters_PreserveUnrelatedFields(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ws := &Workspace{
		Name:           "ws",
		Branch:         "b",
		Repo:           "/r",
		Root:           "/rt",
		Env:            map[string]string{"KEEP": "me"},
		OpenTabs:       []TabInfo{{Name: "tab-a"}},
		ActiveTabIndex: 0,
		Scripts:        ScriptsConfig{Setup: "echo setup"},
	}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	id := ws.ID()

	if err := store.Rename(id, "renamed"); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	if err := store.SetEnv(id, map[string]string{"NEW": "env"}); err != nil {
		t.Fatalf("SetEnv() error = %v", err)
	}
	if err := store.SetScripts(id, ScriptsConfig{Setup: "echo other"}, "concurrent"); err != nil {
		t.Fatalf("SetScripts() error = %v", err)
	}

	loaded, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Name != "renamed" {
		t.Fatalf("Name=%q, want renamed", loaded.Name)
	}
	if loaded.Env["NEW"] != "env" || loaded.Env["KEEP"] != "" {
		t.Fatalf("Env=%v, want {NEW: env} only", loaded.Env)
	}
	if loaded.Scripts.Setup != "echo other" || loaded.ScriptMode != "concurrent" {
		t.Fatalf("Scripts=%+v Mode=%q, want the new scripts and normalized mode", loaded.Scripts, loaded.ScriptMode)
	}
	if len(loaded.OpenTabs) != 1 || loaded.OpenTabs[0].Name != "tab-a" {
		t.Fatalf("field setters clobbered OpenTabs=%v", loaded.OpenTabs)
	}
}

// TestWorkspaceStore_SetEnv_ClonesCallerMap verifies the store does not
// retain the caller's map — mutating it after the call must not leak into
// the persisted record.
func TestWorkspaceStore_SetEnv_ClonesCallerMap(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ws := &Workspace{Name: "ws", Branch: "b", Repo: "/r", Root: "/rt"}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	env := map[string]string{"A": "1"}
	if err := store.SetEnv(ws.ID(), env); err != nil {
		t.Fatalf("SetEnv() error = %v", err)
	}
	env["A"] = "mutated"
	env["B"] = "2"
	loaded, err := store.Load(ws.ID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Env["A"] != "1" || len(loaded.Env) != 1 {
		t.Fatalf("caller map mutation leaked into the record: %v", loaded.Env)
	}
}

// TestWorkspaceStore_SetScripts_NormalizesModeAndSkipsNoop verifies the mode
// normalization and that an unchanged write performs no file update.
func TestWorkspaceStore_SetScripts_NormalizesModeAndSkipsNoop(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ws := &Workspace{Name: "ws", Branch: "b", Repo: "/r", Root: "/rt", Scripts: ScriptsConfig{Setup: "x"}}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	id := ws.ID()
	if err := store.SetScripts(id, ws.Scripts, "bogus-mode"); err != nil {
		t.Fatalf("SetScripts() error = %v", err)
	}
	loaded, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.ScriptMode != "nonconcurrent" {
		t.Fatalf("ScriptMode=%q, want normalized nonconcurrent", loaded.ScriptMode)
	}
	before, _ := os.ReadFile(filepath.Join(store.root, string(id), "workspace.json"))
	// An identical write must be a no-op on disk.
	if err := store.SetScripts(id, ws.Scripts, "nonconcurrent"); err != nil {
		t.Fatalf("SetScripts() error = %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(store.root, string(id), "workspace.json"))
	if string(before) != string(after) {
		t.Fatal("unchanged SetScripts rewrote the record")
	}
}
