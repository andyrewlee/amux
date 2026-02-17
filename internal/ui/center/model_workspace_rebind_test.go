package center

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func TestRebindWorkspaceIDMigratesTabState(t *testing.T) {
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

	oldWS := data.NewWorkspace("feature", "feature", "main", relRepo, relRoot)
	newWS := data.NewWorkspace("feature", "feature", "main", absRepo, absRoot)
	if oldWS.ID() == newWS.ID() {
		t.Fatalf("expected workspace IDs to differ: old=%q new=%q", oldWS.ID(), newWS.ID())
	}

	m := New(nil)
	m.workspace = oldWS
	oldID := string(oldWS.ID())
	newID := string(newWS.ID())
	tab := &Tab{ID: TabID("tab-1"), Workspace: oldWS}
	m.tabsByWorkspace[oldID] = []*Tab{tab}
	m.activeTabByWorkspace[oldID] = 0

	cmd := m.RebindWorkspaceID(oldWS, newWS)
	if cmd != nil {
		t.Fatal("expected no PTY restart cmd for non-running tab")
	}
	if m.workspace != newWS {
		t.Fatal("expected active workspace pointer to be rebound")
	}
	if _, ok := m.tabsByWorkspace[oldID]; ok {
		t.Fatalf("expected old workspace key %q to be removed", oldID)
	}
	gotTabs := m.tabsByWorkspace[newID]
	if len(gotTabs) != 1 || gotTabs[0] != tab {
		t.Fatalf("expected migrated tab under new workspace key, got %d", len(gotTabs))
	}
	if gotTabs[0].Workspace != newWS {
		t.Fatal("expected migrated tab workspace pointer to be rebound")
	}
	if got := m.activeTabByWorkspace[newID]; got != 0 {
		t.Fatalf("expected active tab index 0, got %d", got)
	}
	if _, ok := m.activeTabByWorkspace[oldID]; ok {
		t.Fatalf("expected old active-tab key %q to be removed", oldID)
	}
}

func TestRebindWorkspaceIDMigratesExplicitEmptyState(t *testing.T) {
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

	oldWS := data.NewWorkspace("feature", "feature", "main", relRepo, relRoot)
	newWS := data.NewWorkspace("feature", "feature", "main", absRepo, absRoot)
	if oldWS.ID() == newWS.ID() {
		t.Fatalf("expected workspace IDs to differ: old=%q new=%q", oldWS.ID(), newWS.ID())
	}

	m := New(nil)
	m.workspace = oldWS
	oldID := string(oldWS.ID())
	newID := string(newWS.ID())
	m.tabsByWorkspace[oldID] = []*Tab{}
	m.activeTabByWorkspace[oldID] = 0

	cmd := m.RebindWorkspaceID(oldWS, newWS)
	if cmd != nil {
		t.Fatal("expected no PTY restart cmd for empty workspace state")
	}
	if m.workspace != newWS {
		t.Fatal("expected active workspace pointer to be rebound")
	}
	if _, ok := m.tabsByWorkspace[oldID]; ok {
		t.Fatalf("expected old workspace key %q to be removed", oldID)
	}
	if tabs, ok := m.tabsByWorkspace[newID]; !ok {
		t.Fatalf("expected migrated empty state under new workspace key %q", newID)
	} else if len(tabs) != 0 {
		t.Fatalf("expected migrated state to remain empty, got %d tabs", len(tabs))
	}
	if got := m.activeTabByWorkspace[newID]; got != 0 {
		t.Fatalf("expected active tab index 0, got %d", got)
	}
	if _, ok := m.activeTabByWorkspace[oldID]; ok {
		t.Fatalf("expected old active-tab key %q to be removed", oldID)
	}
}
