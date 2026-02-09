package data

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceStore_LoadMetadataForFallsBackToPathMatch(t *testing.T) {
	root := t.TempDir()
	store := NewWorkspaceStore(root)

	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	workspaceRoot := filepath.Join(repo, ".amux", "workspaces", "feature")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspaceRoot) error = %v", err)
	}

	stored := &Workspace{
		Name:      "feature",
		Branch:    "feature",
		Repo:      repo,
		Root:      workspaceRoot,
		Assistant: "codex",
		OpenTabs: []TabInfo{
			{
				Name:        "Agent 1",
				Assistant:   "codex",
				SessionName: "amux-feature-1",
				Status:      "running",
			},
		},
	}
	if err := store.Save(stored); err != nil {
		t.Fatalf("Save(stored) error = %v", err)
	}
	corruptDir := filepath.Join(root, "deadbeefcafebabe")
	if err := os.MkdirAll(corruptDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(corruptDir) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(corruptDir, workspaceFilename), []byte("{invalid"), 0o644); err != nil {
		t.Fatalf("WriteFile(corrupt metadata) error = %v", err)
	}

	// Change to an unrelated directory to prove lookup resolves against the
	// store's metadata root, not the process CWD.
	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("Chdir error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prevWD)
	})

	// Use absolute paths — real callers always provide absolute repo/root.
	query := &Workspace{
		Name:   "feature",
		Branch: "feature",
		Repo:   repo,
		Root:   workspaceRoot,
	}
	found, err := store.LoadMetadataFor(query)
	if err != nil {
		t.Fatalf("LoadMetadataFor(query) error = %v", err)
	}
	if !found {
		t.Fatalf("expected metadata match despite relative/absolute path representation mismatch")
	}
	if query.Assistant != "codex" {
		t.Fatalf("Assistant = %q, want %q", query.Assistant, "codex")
	}
	if len(query.OpenTabs) != 1 || query.OpenTabs[0].SessionName != "amux-feature-1" {
		t.Fatalf("unexpected restored tabs: %+v", query.OpenTabs)
	}
}

func TestWorkspaceStore_ListByRepoCWDIndependent(t *testing.T) {
	storeRoot := t.TempDir()
	store := NewWorkspaceStore(storeRoot)

	// Create a real repo/root directory so EvalSymlinks succeeds.
	base := t.TempDir()
	repo := filepath.Join(base, "myrepo")
	workspaceRoot := filepath.Join(repo, ".amux", "workspaces", "main")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll error = %v", err)
	}

	ws := &Workspace{
		Name:   "main",
		Branch: "main",
		Repo:   repo,
		Root:   workspaceRoot,
	}
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save error = %v", err)
	}

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prevWD)
	})

	// Run ListByRepo from two completely different CWDs.
	dirs := []string{t.TempDir(), t.TempDir()}
	var results [2][]*Workspace
	for i, dir := range dirs {
		if err := os.Chdir(dir); err != nil {
			t.Fatalf("Chdir(%s) error = %v", dir, err)
		}
		list, err := store.ListByRepo(repo)
		if err != nil {
			t.Fatalf("ListByRepo from CWD %s error = %v", dir, err)
		}
		results[i] = list
	}

	if len(results[0]) != 1 {
		t.Fatalf("expected 1 workspace from CWD-1, got %d", len(results[0]))
	}
	if len(results[1]) != 1 {
		t.Fatalf("expected 1 workspace from CWD-2, got %d", len(results[1]))
	}
	if results[0][0].Name != results[1][0].Name {
		t.Fatalf("workspace names differ across CWDs: %q vs %q", results[0][0].Name, results[1][0].Name)
	}
}
