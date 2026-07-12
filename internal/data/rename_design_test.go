package data

import (
	"os"
	"testing"
)

// seedRenameWorkspace saves a single workspace into a fresh store and returns
// the store plus the saved workspace's ID, so the rename tests below start from
// a persisted record.
func seedRenameWorkspace(t *testing.T, name string) (*WorkspaceStore, WorkspaceID) {
	t.Helper()
	store := NewWorkspaceStore(t.TempDir())
	ws := &Workspace{
		Name:       name,
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

// TestRenameWorkspaceLabelDesign_StoreIntegration is the wired Tier-1 test: a
// label rename updates Name while ID() and the metadata file path are unchanged.
// This pins the safety property that makes rename zero-churn — the ID derives
// from Repo/Root only, so relabeling never relocates the record or touches any
// ID-keyed tmux session / tag / worktree.
func TestRenameWorkspaceLabelDesign_StoreIntegration(t *testing.T) {
	store, id := seedRenameWorkspace(t, "old-name")
	path := store.workspacePath(id)

	if err := store.Rename(id, "new-name"); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}

	reloaded, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load() after rename error = %v", err)
	}
	if reloaded.Name != "new-name" {
		t.Errorf("Name = %q, want %q", reloaded.Name, "new-name")
	}
	// The crux: renaming the label must NOT churn the ID, so the record stays at
	// the same id-derived path.
	if reloaded.ID() != id {
		t.Errorf("ID changed by rename: got %q, want %q", reloaded.ID(), id)
	}
	if got := store.workspacePath(id); got != path {
		t.Errorf("metadata path moved: got %q, want %q", got, path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("metadata file should still exist at original path: %v", err)
	}
	// The file did not move: exactly one workspace directory remains.
	entries, err := os.ReadDir(store.root)
	if err != nil {
		t.Fatalf("ReadDir(store.root) error = %v", err)
	}
	dirs := 0
	for _, e := range entries {
		if e.IsDir() {
			dirs++
		}
	}
	if dirs != 1 {
		t.Errorf("expected exactly 1 workspace dir after rename, got %d", dirs)
	}
}

// TestRenameWorkspace_RejectsInvalidName asserts every rule enforced by
// validation.ValidateWorkspaceName rejects the corresponding label through the
// store, and that a rejected rename leaves the stored record untouched.
func TestRenameWorkspace_RejectsInvalidName(t *testing.T) {
	invalid := map[string]string{
		"empty":            "",
		"consecutive dots": "a..b",
		"reserved HEAD":    "HEAD",
		"dot-lock suffix":  "work.lock",
		"leading dash":     "-nope",
	}
	for label, name := range invalid {
		t.Run(label, func(t *testing.T) {
			store, id := seedRenameWorkspace(t, "keep-me")
			if err := store.Rename(id, name); err == nil {
				t.Fatalf("Rename(%q) should be rejected", name)
			}
			reloaded, err := store.Load(id)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if reloaded.Name != "keep-me" {
				t.Errorf("Name = %q, want unchanged %q", reloaded.Name, "keep-me")
			}
		})
	}
}

// TestRenameWorkspace_NoOpSameName asserts renaming to the current name is a
// no-op that does not error.
func TestRenameWorkspace_NoOpSameName(t *testing.T) {
	store, id := seedRenameWorkspace(t, "same")
	if err := store.Rename(id, "same"); err != nil {
		t.Fatalf("Rename() to same name should be a no-op, got %v", err)
	}
	reloaded, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if reloaded.Name != "same" {
		t.Errorf("Name = %q, want %q", reloaded.Name, "same")
	}
}
