package data

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkspaceStore_HasLegacyWorkspacesMissingRepo(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	legacyDir := filepath.Join(root, "legacy-missing-repo")
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	legacyData := `{"name":"old-ws","branch":"old-ws"}`
	if err := os.WriteFile(filepath.Join(legacyDir, "workspace.json"), []byte(legacyData), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	hasLegacy, err := store.HasLegacyWorkspaces("/home/user/repo")
	if err != nil {
		t.Fatalf("HasLegacyWorkspaces() error = %v", err)
	}
	if !hasLegacy {
		t.Fatal("expected legacy workspaces to be detected when repo is missing")
	}
}

func TestWorkspaceStore_UpsertFromDiscoveryPreservesArchived(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	repo := "/home/user/repo"
	rootPath := "/worktrees/feature"

	archived := &Workspace{
		Name:       "feature",
		Branch:     "feature",
		Repo:       repo,
		Root:       rootPath,
		Archived:   true,
		ArchivedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := store.Save(archived); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	discovered := &Workspace{
		Name:   "feature",
		Branch: "feature",
		Repo:   repo,
		Root:   rootPath,
	}
	if err := store.UpsertFromDiscoveryPreserveArchived(discovered); err != nil {
		t.Fatalf("UpsertFromDiscoveryPreserveArchived() error = %v", err)
	}

	loaded, err := store.Load(archived.ID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !loaded.Archived {
		t.Fatal("expected archived workspace to remain archived")
	}
	if loaded.ArchivedAt.IsZero() {
		t.Fatal("expected archived timestamp to remain set")
	}
}
