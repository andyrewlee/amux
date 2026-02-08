package data

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceStore_SaveRejectsNilWorkspace(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	if err := store.Save(nil); err == nil {
		t.Fatalf("expected Save to reject nil workspace")
	}
}

func TestWorkspaceStore_SaveRejectsWorkspaceWithMissingIdentity(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	if err := store.Save(&Workspace{Name: "missing-repo", Root: "/root"}); err == nil {
		t.Fatalf("expected Save to reject workspace with empty repo")
	}
	if err := store.Save(&Workspace{Name: "missing-root", Repo: "/repo"}); err == nil {
		t.Fatalf("expected Save to reject workspace with empty root")
	}
}

func TestWorkspaceStore_LoadMetadataForRejectsNilWorkspace(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	found, err := store.LoadMetadataFor(nil)
	if err == nil {
		t.Fatalf("expected LoadMetadataFor to reject nil workspace")
	}
	if found {
		t.Fatalf("expected found=false when workspace is nil")
	}
}

func TestWorkspaceStore_LockFileIsOutsideWorkspaceDirectory(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)
	id := WorkspaceID("abc123")

	lockPath := filepath.Clean(store.workspaceLockPath(id))
	workspaceDir := filepath.Clean(filepath.Join(root, string(id)))
	legacyLockPath := filepath.Clean(filepath.Join(workspaceDir, ".lock"))

	if filepath.Clean(filepath.Dir(lockPath)) != filepath.Clean(root) {
		t.Fatalf("expected lock file parent to be metadata root, got %s", filepath.Dir(lockPath))
	}
	if lockPath == legacyLockPath {
		t.Fatalf("lock path should not be inside workspace directory: %s", lockPath)
	}
}

func TestWorkspaceStore_ListByRepo_ErrorsWhenTargetRepoMetadataCorrupt(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	repoA := filepath.Join(t.TempDir(), "repo-a")
	repoB := filepath.Join(t.TempDir(), "repo-b")

	// Valid workspace for a different repo should not hide target-repo corruption.
	validB := &Workspace{Name: "ws-b", Repo: repoB, Root: filepath.Join(root, "workspaces", "ws-b")}
	if err := store.Save(validB); err != nil {
		t.Fatalf("Save(validB) error = %v", err)
	}

	// Write a corrupt metadata file that still includes a target repo hint.
	targetRoot := filepath.Join(root, "workspaces", "ws-a")
	targetRepoJSON, _ := json.Marshal(repoA)
	targetRootJSON, _ := json.Marshal(targetRoot)
	corruptData := fmt.Sprintf(`{"name":"ws-a","repo":%s,"root":%s,`, targetRepoJSON, targetRootJSON)
	corruptID := Workspace{Repo: repoA, Root: targetRoot}.ID()
	corruptDir := filepath.Join(root, string(corruptID))
	if err := os.MkdirAll(corruptDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(corruptDir) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(corruptDir, workspaceFilename), []byte(corruptData), 0o644); err != nil {
		t.Fatalf("WriteFile(corrupt) error = %v", err)
	}

	_, err := store.ListByRepo(repoA)
	if err == nil {
		t.Fatalf("expected ListByRepo(repoA) to report corruption for target repo")
	}
}
