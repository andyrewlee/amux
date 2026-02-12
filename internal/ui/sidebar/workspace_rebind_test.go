package sidebar

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func TestChangesSetWorkspaceSameIDPreservesState(t *testing.T) {
	model := New()
	ws1 := data.NewWorkspace("feature", "feature", "main", "/tmp/repo", "/tmp/workspaces/repo/feature")
	model.SetWorkspace(ws1)
	model.cursor = 4
	model.scrollOffset = 2
	model.filterQuery = "abc"
	model.filterInput.SetValue("abc")

	ws2 := data.NewWorkspace("feature", "updated-branch", "main", "/tmp/repo", "/tmp/workspaces/repo/feature")
	model.SetWorkspace(ws2)

	if model.workspace != ws2 {
		t.Fatal("expected workspace pointer to be rebound")
	}
	if model.cursor != 4 {
		t.Fatalf("cursor = %d, want 4", model.cursor)
	}
	if model.scrollOffset != 2 {
		t.Fatalf("scrollOffset = %d, want 2", model.scrollOffset)
	}
	if model.filterQuery != "abc" {
		t.Fatalf("filterQuery = %q, want %q", model.filterQuery, "abc")
	}
	if model.filterInput.Value() != "abc" {
		t.Fatalf("filterInput = %q, want %q", model.filterInput.Value(), "abc")
	}
}

func TestChangesSetWorkspaceDifferentIDResetsState(t *testing.T) {
	model := New()
	ws1 := data.NewWorkspace("feature", "feature", "main", "/tmp/repo", "/tmp/workspaces/repo/feature")
	ws2 := data.NewWorkspace("other", "other", "main", "/tmp/repo", "/tmp/workspaces/repo/other")

	model.SetWorkspace(ws1)
	model.cursor = 4
	model.scrollOffset = 2
	model.filterQuery = "abc"
	model.filterInput.SetValue("abc")

	model.SetWorkspace(ws2)

	if model.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", model.cursor)
	}
	if model.scrollOffset != 0 {
		t.Fatalf("scrollOffset = %d, want 0", model.scrollOffset)
	}
	if model.filterQuery != "" {
		t.Fatalf("filterQuery = %q, want empty", model.filterQuery)
	}
	if model.filterInput.Value() != "" {
		t.Fatalf("filterInput = %q, want empty", model.filterInput.Value())
	}
}

func TestProjectTreeSetWorkspaceSameIDPreservesState(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo", "feature")
	ws1 := data.NewWorkspace("feature", "feature", "main", filepath.Join(base, "repo"), root)
	ws2 := data.NewWorkspace("feature", "updated", "main", filepath.Join(base, "repo"), root)

	tree := NewProjectTree()
	tree.SetWorkspace(ws1)
	tree.cursor = 3
	tree.scrollOffset = 1

	tree.SetWorkspace(ws2)

	if tree.workspace != ws2 {
		t.Fatal("expected workspace pointer to be rebound")
	}
	if tree.cursor != 3 {
		t.Fatalf("cursor = %d, want 3", tree.cursor)
	}
	if tree.scrollOffset != 1 {
		t.Fatalf("scrollOffset = %d, want 1", tree.scrollOffset)
	}
}

func TestChangesSetWorkspaceCanonicalMatchDifferentIDPreservesState(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	base := t.TempDir()
	absRepo := filepath.Join(base, "repo")
	absRoot := filepath.Join(base, "workspaces", "repo", "feature")
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", absRoot, err)
	}
	relRepo, err := filepath.Rel(wd, absRepo)
	if err != nil {
		t.Fatalf("Rel(repo): %v", err)
	}
	relRoot, err := filepath.Rel(wd, absRoot)
	if err != nil {
		t.Fatalf("Rel(root): %v", err)
	}

	model := New()
	ws1 := data.NewWorkspace("feature", "feature", "main", relRepo, relRoot)
	ws2 := data.NewWorkspace("feature", "feature", "main", absRepo, absRoot)

	model.SetWorkspace(ws1)
	model.cursor = 4
	model.scrollOffset = 2
	model.filterQuery = "abc"
	model.filterInput.SetValue("abc")

	model.SetWorkspace(ws2)

	if model.workspace != ws2 {
		t.Fatal("expected workspace pointer to be rebound")
	}
	if model.cursor != 4 {
		t.Fatalf("cursor = %d, want 4", model.cursor)
	}
	if model.scrollOffset != 2 {
		t.Fatalf("scrollOffset = %d, want 2", model.scrollOffset)
	}
	if model.filterQuery != "abc" {
		t.Fatalf("filterQuery = %q, want %q", model.filterQuery, "abc")
	}
	if model.filterInput.Value() != "abc" {
		t.Fatalf("filterInput = %q, want %q", model.filterInput.Value(), "abc")
	}
}

func TestProjectTreeSetWorkspaceCanonicalMatchDifferentIDPreservesState(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	base := t.TempDir()
	absRepo := filepath.Join(base, "repo")
	absRoot := filepath.Join(base, "workspaces", "repo", "feature")
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", absRoot, err)
	}
	relRepo, err := filepath.Rel(wd, absRepo)
	if err != nil {
		t.Fatalf("Rel(repo): %v", err)
	}
	relRoot, err := filepath.Rel(wd, absRoot)
	if err != nil {
		t.Fatalf("Rel(root): %v", err)
	}
	if err := os.WriteFile(filepath.Join(absRoot, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("WriteFile(a.txt): %v", err)
	}
	if err := os.WriteFile(filepath.Join(absRoot, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatalf("WriteFile(b.txt): %v", err)
	}

	tree := NewProjectTree()
	ws1 := data.NewWorkspace("feature", "feature", "main", relRepo, relRoot)
	ws2 := data.NewWorkspace("feature", "feature", "main", absRepo, absRoot)
	tree.SetWorkspace(ws1)
	if len(tree.flatNodes) < 2 {
		t.Fatalf("expected >= 2 nodes after initial load, got %d", len(tree.flatNodes))
	}
	firstBefore := tree.flatNodes[0].Path
	tree.cursor = 1
	tree.scrollOffset = 1

	tree.SetWorkspace(ws2)

	if tree.workspace != ws2 {
		t.Fatal("expected workspace pointer to be rebound")
	}
	if tree.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", tree.cursor)
	}
	if tree.scrollOffset != 1 {
		t.Fatalf("scrollOffset = %d, want 1", tree.scrollOffset)
	}
	if len(tree.flatNodes) < 2 {
		t.Fatalf("expected >= 2 nodes after rebind, got %d", len(tree.flatNodes))
	}
	firstAfter := tree.flatNodes[0].Path
	if firstAfter == firstBefore {
		t.Fatalf("expected node path to be rebased, still %q", firstAfter)
	}
	if !filepath.IsAbs(firstAfter) {
		t.Fatalf("expected rebased node path to be absolute, got %q", firstAfter)
	}
	if !strings.HasPrefix(firstAfter, absRoot+string(filepath.Separator)) {
		t.Fatalf("expected rebased node path under %q, got %q", absRoot, firstAfter)
	}

	tree.cursor = 0
	cmd := tree.handleEnter()
	if cmd == nil {
		t.Fatal("expected file-open command for selected file")
	}
	opened := cmd()
	msg, ok := opened.(OpenFileInEditor)
	if !ok {
		t.Fatalf("expected OpenFileInEditor, got %T", opened)
	}
	if !filepath.IsAbs(msg.Path) {
		t.Fatalf("expected absolute file path in OpenFileInEditor, got %q", msg.Path)
	}
	if !strings.HasPrefix(msg.Path, absRoot+string(filepath.Separator)) {
		t.Fatalf("expected OpenFileInEditor path under %q, got %q", absRoot, msg.Path)
	}
}
