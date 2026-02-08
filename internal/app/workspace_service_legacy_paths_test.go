package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

func TestLoadProjectsPreservesLegacyRelativeRepoWorkspaceMetadata(t *testing.T) {
	skipIfNoGit(t)

	baseDir := t.TempDir()
	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(baseDir); err != nil {
		t.Fatalf("Chdir(%s) error = %v", baseDir, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prevWD)
	})

	repoRel := "repo"
	repoAbs := filepath.Join(baseDir, repoRel)
	if err := os.MkdirAll(repoAbs, 0o755); err != nil {
		t.Fatalf("MkdirAll(repo) error = %v", err)
	}
	runGit(t, repoAbs, "init", "-b", "main")

	registry := data.NewRegistry(filepath.Join(baseDir, "projects.json"))
	if err := registry.Save([]string{repoRel}); err != nil {
		t.Fatalf("Save(registry) error = %v", err)
	}

	store := data.NewWorkspaceStore(filepath.Join(baseDir, "workspaces-metadata"))
	legacyWorkspace := data.NewWorkspace(
		"feature",
		"feature",
		"HEAD",
		repoRel,
		filepath.Join(repoRel, ".amux", "workspaces", "feature"),
	)
	if err := store.Save(legacyWorkspace); err != nil {
		t.Fatalf("Save(workspace metadata) error = %v", err)
	}

	service := newWorkspaceService(registry, store, nil, "")
	msg := service.LoadProjects()()
	loaded, ok := msg.(messages.ProjectsLoaded)
	if !ok {
		t.Fatalf("expected ProjectsLoaded, got %T", msg)
	}
	if len(loaded.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(loaded.Projects))
	}

	foundLegacyWorkspace := false
	for _, ws := range loaded.Projects[0].Workspaces {
		if ws.Name == legacyWorkspace.Name && data.NormalizePath(ws.Root) == data.NormalizePath(legacyWorkspace.Root) {
			foundLegacyWorkspace = true
			break
		}
	}
	if !foundLegacyWorkspace {
		t.Fatalf("expected stored legacy workspace metadata to be loaded")
	}
}
