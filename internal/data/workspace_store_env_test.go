package data

import "testing"

// seedEnvWorkspace saves a single workspace (optionally pre-seeded with Env)
// into a fresh store and returns the store plus the saved workspace's ID,
// mirroring seedRenameWorkspace in rename_design_test.go.
func seedEnvWorkspace(t *testing.T, env map[string]string) (*WorkspaceStore, WorkspaceID) {
	t.Helper()
	store := NewWorkspaceStore(t.TempDir())
	ws := &Workspace{
		Name:       "feature",
		Branch:     "feature-branch",
		Base:       "origin/main",
		Repo:       "/home/user/repo",
		Root:       "/home/user/.amux/workspaces/feature",
		Runtime:    RuntimeLocalWorktree,
		Assistant:  "claude",
		ScriptMode: "nonconcurrent",
		Env:        env,
	}
	if err := store.Save(ws); err != nil {
		t.Fatalf("seed Save() error = %v", err)
	}
	return store, ws.ID()
}

// TestWorkspaceStoreSetEnv_PersistsAndReloads is the wired persist-path test:
// SetEnv updates the stored Env map and a fresh Load reflects it, mirroring
// TestRenameWorkspaceLabelDesign_StoreIntegration's shape for the Name field.
func TestWorkspaceStoreSetEnv_PersistsAndReloads(t *testing.T) {
	store, id := seedEnvWorkspace(t, map[string]string{"OLD": "value"})

	newEnv := map[string]string{"API_KEY": "secret", "NODE_ENV": "production"}
	if err := store.SetEnv(id, newEnv); err != nil {
		t.Fatalf("SetEnv() error = %v", err)
	}

	reloaded, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load() after SetEnv error = %v", err)
	}
	if len(reloaded.Env) != 2 || reloaded.Env["API_KEY"] != "secret" || reloaded.Env["NODE_ENV"] != "production" {
		t.Fatalf("Env after SetEnv = %#v, want %#v", reloaded.Env, newEnv)
	}
	// The old key must be gone: SetEnv replaces the map wholesale (the app
	// layer is responsible for merging edits before calling it).
	if _, ok := reloaded.Env["OLD"]; ok {
		t.Fatalf("expected OLD to be replaced, got %#v", reloaded.Env)
	}
	// ID must be unchanged (Repo/Root/Branch untouched, so no tmux/tag/worktree
	// churn), the same invariant Rename pins.
	if reloaded.ID() != id {
		t.Errorf("ID changed by SetEnv: got %q, want %q", reloaded.ID(), id)
	}
}

// TestWorkspaceStoreSetEnv_EmptyMapClearsEnv confirms SetEnv can clear every
// custom var (removing the last pair via the dialog persists as an empty
// map, not a no-op).
func TestWorkspaceStoreSetEnv_EmptyMapClearsEnv(t *testing.T) {
	store, id := seedEnvWorkspace(t, map[string]string{"FOO": "bar"})

	if err := store.SetEnv(id, map[string]string{}); err != nil {
		t.Fatalf("SetEnv() error = %v", err)
	}

	reloaded, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(reloaded.Env) != 0 {
		t.Fatalf("Env after clearing = %#v, want empty", reloaded.Env)
	}
}

// TestWorkspaceStoreSetEnv_NoOpSameMapDoesNotError mirrors
// TestRenameWorkspace_NoOpSameName: writing back an identical map is a
// harmless no-op.
func TestWorkspaceStoreSetEnv_NoOpSameMapDoesNotError(t *testing.T) {
	store, id := seedEnvWorkspace(t, map[string]string{"FOO": "bar"})

	if err := store.SetEnv(id, map[string]string{"FOO": "bar"}); err != nil {
		t.Fatalf("SetEnv() with identical map should be a no-op, got %v", err)
	}

	reloaded, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if reloaded.Env["FOO"] != "bar" {
		t.Fatalf("Env = %#v, want unchanged", reloaded.Env)
	}
}

// TestWorkspaceStoreSetEnv_UnknownIDErrors confirms SetEnv surfaces the same
// load error Rename would for a workspace ID with no metadata on disk, rather
// than silently creating a new record.
func TestWorkspaceStoreSetEnv_UnknownIDErrors(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	if err := store.SetEnv(WorkspaceID("does-not-exist"), map[string]string{"A": "1"}); err == nil {
		t.Fatal("expected an error for an unknown workspace ID")
	}
}
