package data

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRebindRootUpdatesRecordInPlace(t *testing.T) {
	dir := t.TempDir()
	store := NewWorkspaceStore(dir)
	repo := filepath.Join(dir, "repo")
	oldRoot := filepath.Join(dir, "workspaces", "checkfu", "feature")
	newRoot := filepath.Join(dir, "workspaces", "checkfu-old", "feature")

	ws := NewWorkspace("feature", "feature", "main", repo, oldRoot)
	if err := store.Save(ws); err != nil {
		t.Fatalf("save: %v", err)
	}
	key := ws.MetadataID()

	// A concurrent field write must survive the rebind: RebindRoot is a
	// locked read-modify-write, not a caller-snapshot rewrite.
	if err := store.SetEnv(key, map[string]string{"TOKEN": "keep"}); err != nil {
		t.Fatalf("set env: %v", err)
	}

	if err := store.RebindRoot(key, newRoot); err != nil {
		t.Fatalf("RebindRoot() error = %v", err)
	}

	loaded, err := store.Load(key)
	if err != nil {
		t.Fatalf("load after rebind: %v", err)
	}
	if loaded.Root != newRoot {
		t.Fatalf("Root = %q, want %q", loaded.Root, newRoot)
	}
	if loaded.Env["TOKEN"] != "keep" {
		t.Fatalf("concurrent field lost across rebind: %v", loaded.Env)
	}

	// The record lives under the SAME key — the stable identity sessions,
	// tags, and port reservations key on.
	if _, err := os.Stat(filepath.Join(dir, string(key), "workspace.json")); err != nil {
		t.Fatalf("record must stay under its original key: %v", err)
	}

	// Discovery-style lookup now finds the record at the new root — via the
	// content fallback scan, since the record still lives under its
	// old-root-derived key (that key IS the stable identity).
	found, foundID, err := store.findStoredWorkspace(repo, newRoot)
	if err != nil || found == nil || foundID != key {
		t.Fatalf("findStoredWorkspace(new root) = (%v, %v, %v), want the record under its key", found, foundID, err)
	}

	// Probing the old root still resolves the same record (stable-key
	// decoupling) — its content must claim the new root, and the store must
	// hold exactly one record: no duplicate minted for the new path.
	oldFound, _, err := store.findStoredWorkspace(repo, oldRoot)
	if err != nil {
		t.Fatalf("findStoredWorkspace(old root) error: %v", err)
	}
	if oldFound == nil || oldFound.Root != newRoot {
		t.Fatalf("old-root probe should resolve the same record claiming the new root, got %+v", oldFound)
	}
	ids, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("store must hold exactly one record after rebind, got %d: %v", len(ids), ids)
	}

	// A post-migration Save (status change, tab update) must update the
	// same record in place — not mint a second one under the new-root key.
	loaded.Name = "renamed-feature"
	if err := store.Save(loaded); err != nil {
		t.Fatalf("save after rebind: %v", err)
	}
	ids, err = store.List()
	if err != nil {
		t.Fatalf("list after save: %v", err)
	}
	if len(ids) != 1 || ids[0] != key {
		t.Fatalf("save after rebind must keep the single record under its key, got %v", ids)
	}
	reloaded, err := store.Load(key)
	if err != nil {
		t.Fatalf("load after save: %v", err)
	}
	if reloaded.Name != "renamed-feature" || reloaded.Root != newRoot {
		t.Fatalf("save after rebind lost fields: name=%q root=%q", reloaded.Name, reloaded.Root)
	}
}

func TestRebindRootIdempotentNoOp(t *testing.T) {
	dir := t.TempDir()
	store := NewWorkspaceStore(dir)
	repo := filepath.Join(dir, "repo")
	root := filepath.Join(dir, "workspaces", "checkfu", "feature")

	ws := NewWorkspace("feature", "feature", "main", repo, root)
	if err := store.Save(ws); err != nil {
		t.Fatalf("save: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(dir, string(ws.MetadataID()), "workspace.json"))
	if err != nil {
		t.Fatalf("read record: %v", err)
	}

	if err := store.RebindRoot(ws.MetadataID(), root); err != nil {
		t.Fatalf("rebinding to the current root must be a no-op, got %v", err)
	}
	after, err := os.ReadFile(filepath.Join(dir, string(ws.MetadataID()), "workspace.json"))
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("no-op rebind rewrote the record")
	}
}

func TestRebindRootRefusesBadTargets(t *testing.T) {
	dir := t.TempDir()
	store := NewWorkspaceStore(dir)
	repo := filepath.Join(dir, "repo")
	root := filepath.Join(dir, "workspaces", "checkfu", "feature")
	ws := NewWorkspace("feature", "feature", "main", repo, root)
	if err := store.Save(ws); err != nil {
		t.Fatalf("save: %v", err)
	}
	id := ws.MetadataID()

	cases := []struct {
		name    string
		newRoot string
		wantErr string
	}{
		{"empty root", "", "new root is required"},
		{"relative root", "relative/path", "absolute path"},
		{"primary checkout", repo, "primary checkout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := store.RebindRoot(id, tc.newRoot)
			if err == nil {
				t.Fatalf("expected refusal for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestRebindRootRefusesShadowingAnotherRecord(t *testing.T) {
	dir := t.TempDir()
	store := NewWorkspaceStore(dir)
	repo := filepath.Join(dir, "repo")
	rootA := filepath.Join(dir, "workspaces", "checkfu", "feature-a")
	rootB := filepath.Join(dir, "workspaces", "checkfu-old", "feature-a")

	wsA := NewWorkspace("feature-a", "feature-a", "main", repo, rootA)
	if err := store.Save(wsA); err != nil {
		t.Fatalf("save A: %v", err)
	}
	wsB := NewWorkspace("feature-a", "feature-a", "main", repo, rootB)
	if err := store.Save(wsB); err != nil {
		t.Fatalf("save B: %v", err)
	}

	err := store.RebindRoot(wsA.MetadataID(), rootB)
	if err == nil {
		t.Fatal("expected rebind onto another record's identity to be refused")
	}
	if !strings.Contains(err.Error(), "already recorded") {
		t.Fatalf("error = %v, want it to mention the shadowed record", err)
	}
}

func TestRebindRootRefusesCaseVariantRootCollision(t *testing.T) {
	dir := t.TempDir()
	store := NewWorkspaceStore(dir)
	repo := filepath.Join(dir, "repo")
	// Another record whose root differs from the rebind target only by
	// letter case — one physical directory on the case-insensitive
	// filesystems the workspaces root commonly lives on. The exact-match
	// lookup misses it; the folded check must not.
	otherRoot := filepath.Join(dir, "workspaces", "Checkfu", "feature")
	if err := os.MkdirAll(otherRoot, 0o700); err != nil {
		t.Fatalf("mkdir other root: %v", err)
	}
	ws := NewWorkspace("feature", "feature", "main", repo, otherRoot)
	if err := store.Save(ws); err != nil {
		t.Fatalf("save: %v", err)
	}

	// The rebinding record lives elsewhere today; the rebind would move it
	// onto the case-variant of the other record's root.
	target := NewWorkspace("other", "other", "main", repo, filepath.Join(dir, "workspaces", "elsewhere", "feature"))
	if err := store.Save(target); err != nil {
		t.Fatalf("save target: %v", err)
	}

	newRoot := filepath.Join(dir, "workspaces", "checkfu", "feature")
	if err := os.MkdirAll(newRoot, 0o700); err != nil {
		t.Fatalf("mkdir new root: %v", err)
	}
	err := store.RebindRoot(target.MetadataID(), newRoot)
	// On a case-insensitive filesystem the case-variant spellings are one
	// directory and the rebind must be refused. On a case-sensitive one
	// they are distinct directories, distinct worktrees are allowed to
	// hold them, and refusing would wedge the project rename forever.
	if same, ok := SamePhysicalDir(otherRoot, newRoot); ok && same {
		if err == nil {
			t.Fatal("expected the case-variant root collision to be refused")
		}
		if !strings.Contains(err.Error(), "case-insensitive match") {
			t.Fatalf("error = %v, want it to name the case-insensitive match", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("distinct case-variant directories must rebind cleanly, got %v", err)
	}
}

func TestRebindRootMissingRecordFails(t *testing.T) {
	dir := t.TempDir()
	store := NewWorkspaceStore(dir)
	if err := store.RebindRoot("nonexistent", filepath.Join(dir, "new")); err == nil {
		t.Fatal("expected rebinding a missing record to fail")
	}
}
