package data

import "testing"

func seedScriptsWorkspace(t *testing.T) (*WorkspaceStore, WorkspaceID) {
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
	}
	if err := store.Save(ws); err != nil {
		t.Fatalf("seed Save() error = %v", err)
	}
	return store, ws.ID()
}

// TestWorkspaceStoreSetScripts_PersistsAndReloads mirrors
// TestWorkspaceStoreSetEnv_PersistsAndReloads: SetScripts updates the stored
// Scripts/ScriptMode and a fresh Load reflects them.
func TestWorkspaceStoreSetScripts_PersistsAndReloads(t *testing.T) {
	store, id := seedScriptsWorkspace(t)

	scripts := ScriptsConfig{Setup: "make deps", Run: "npm run dev", Archive: "make clean"}
	if err := store.SetScripts(id, scripts, "concurrent"); err != nil {
		t.Fatalf("SetScripts() error = %v", err)
	}

	reloaded, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if reloaded.Scripts != scripts {
		t.Fatalf("persisted Scripts = %#v, want %#v", reloaded.Scripts, scripts)
	}
	if reloaded.ScriptMode != "concurrent" {
		t.Fatalf("persisted ScriptMode = %q, want concurrent", reloaded.ScriptMode)
	}
}

// TestWorkspaceStoreSetScripts_ModeNormalizes: an unknown mode string falls
// back to nonconcurrent rather than persisting garbage the lifecycle check
// (ws.ScriptMode == "nonconcurrent") would silently misread as concurrent.
func TestWorkspaceStoreSetScripts_ModeNormalizes(t *testing.T) {
	store, id := seedScriptsWorkspace(t)
	if err := store.SetScripts(id, ScriptsConfig{Run: "x"}, "bogus"); err != nil {
		t.Fatalf("SetScripts() error = %v", err)
	}
	reloaded, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if reloaded.ScriptMode != "nonconcurrent" {
		t.Fatalf("ScriptMode = %q, want nonconcurrent", reloaded.ScriptMode)
	}
}

// TestWorkspaceStoreSetScripts_NoChangeIsNoop mirrors SetEnv's no-op guard:
// identical values must not rewrite the file (which would emit a spurious
// watcher event).
func TestWorkspaceStoreSetScripts_NoChangeIsNoop(t *testing.T) {
	store, id := seedScriptsWorkspace(t)
	if err := store.SetScripts(id, ScriptsConfig{}, "nonconcurrent"); err != nil {
		t.Fatalf("SetScripts() error = %v", err)
	}
	reloaded, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if reloaded.Scripts != (ScriptsConfig{}) || reloaded.ScriptMode != "nonconcurrent" {
		t.Fatalf("unexpected mutation: %#v mode=%q", reloaded.Scripts, reloaded.ScriptMode)
	}
}
