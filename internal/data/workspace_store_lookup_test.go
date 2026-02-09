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

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(base); err != nil {
		t.Fatalf("Chdir(%s) error = %v", base, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prevWD)
	})

	query := &Workspace{
		Name:   "feature",
		Branch: "feature",
		Repo:   "./repo",
		Root:   "./repo/.amux/workspaces/feature",
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
