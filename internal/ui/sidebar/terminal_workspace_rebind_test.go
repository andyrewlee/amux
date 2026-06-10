package sidebar

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func TestRebindWorkspaceIDMigratesTerminalState(t *testing.T) {
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

	m := NewTerminalModel()
	m.workspace = oldWS
	oldID := string(oldWS.ID())
	newID := string(newWS.ID())
	tab := &TerminalTab{
		ID:   TerminalTabID("term-tab-1"),
		Name: "Terminal 1",
		State: &TerminalState{
			Running: false,
		},
	}
	m.tabs.ByWorkspace[oldID] = []*TerminalTab{tab}
	m.tabs.ActiveByWorkspace[oldID] = 0
	m.pendingCreation[oldID] = true

	cmd := m.RebindWorkspaceID(oldWS, newWS)
	if cmd != nil {
		t.Fatal("expected no PTY restart cmd for non-running terminal")
	}
	if m.workspace != newWS {
		t.Fatal("expected active workspace pointer to be rebound")
	}
	if _, ok := m.tabs.ByWorkspace[oldID]; ok {
		t.Fatalf("expected old workspace key %q to be removed", oldID)
	}
	gotTabs := m.tabs.ByWorkspace[newID]
	if len(gotTabs) != 1 || gotTabs[0] != tab {
		t.Fatalf("expected migrated terminal tab under new workspace key, got %d", len(gotTabs))
	}
	if got := m.tabs.ActiveByWorkspace[newID]; got != 0 {
		t.Fatalf("expected active tab index 0, got %d", got)
	}
	if _, ok := m.tabs.ActiveByWorkspace[oldID]; ok {
		t.Fatalf("expected old active-tab key %q to be removed", oldID)
	}
	if !m.pendingCreation[newID] {
		t.Fatalf("expected pending creation flag to migrate to %q", newID)
	}
	if m.pendingCreation[oldID] {
		t.Fatalf("expected pending creation flag to be removed from %q", oldID)
	}
}
