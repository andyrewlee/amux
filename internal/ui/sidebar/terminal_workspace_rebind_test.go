package sidebar

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/pty"
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
		ID:        TerminalTabID("term-tab-1"),
		Name:      "Terminal 1",
		Workspace: oldWS,
		State: &TerminalState{
			Running: false,
		},
	}
	m.tabsByWorkspace[oldID] = []*TerminalTab{tab}
	m.activeTabByWorkspace[oldID] = 0
	m.pendingCreation[oldID] = true

	cmd := m.RebindWorkspaceID(oldWS, newWS)
	if cmd != nil {
		t.Fatal("expected no PTY restart cmd for non-running terminal")
	}
	if m.workspace != newWS {
		t.Fatal("expected active workspace pointer to be rebound")
	}
	if _, ok := m.tabsByWorkspace[oldID]; ok {
		t.Fatalf("expected old workspace key %q to be removed", oldID)
	}
	gotTabs := m.tabsByWorkspace[newID]
	if len(gotTabs) != 1 || gotTabs[0] != tab {
		t.Fatalf("expected migrated terminal tab under new workspace key, got %d", len(gotTabs))
	}
	if got := m.activeTabByWorkspace[newID]; got != 0 {
		t.Fatalf("expected active tab index 0, got %d", got)
	}
	if _, ok := m.activeTabByWorkspace[oldID]; ok {
		t.Fatalf("expected old active-tab key %q to be removed", oldID)
	}
	if !m.pendingCreation[newID] {
		t.Fatalf("expected pending creation flag to migrate to %q", newID)
	}
	if m.pendingCreation[oldID] {
		t.Fatalf("expected pending creation flag to be removed from %q", oldID)
	}
}

func TestRebindWorkspaceIDRefreshesWorkspaceWhenIDUnchanged(t *testing.T) {
	oldWS := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo")
	oldWS.Runtime = data.RuntimeCloudSandbox
	newWS := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo")
	newWS.Runtime = data.RuntimeLocalWorktree
	if oldWS.ID() != newWS.ID() {
		t.Fatalf("expected workspace IDs to match: old=%q new=%q", oldWS.ID(), newWS.ID())
	}

	m := NewTerminalModel()
	m.workspace = oldWS
	wsID := string(oldWS.ID())
	m.tabsByWorkspace[wsID] = []*TerminalTab{{
		ID:        TerminalTabID("term-tab-1"),
		Name:      "Terminal 1",
		Workspace: oldWS,
		State: &TerminalState{
			Running: false,
		},
	}}

	cmd := m.RebindWorkspaceID(oldWS, newWS)
	if cmd != nil {
		t.Fatal("expected no command for same-ID terminal workspace refresh")
	}
	if m.workspace != newWS {
		t.Fatal("expected active terminal workspace pointer to refresh when workspace ID is unchanged")
	}
	if gotTabs := m.tabsByWorkspace[wsID]; len(gotTabs) != 1 {
		t.Fatalf("expected terminal tabs to remain under workspace key %q", wsID)
	}
	if gotTabs := m.tabsByWorkspace[wsID]; gotTabs[0].Workspace == nil || data.NormalizeRuntime(gotTabs[0].Workspace.Runtime) != data.RuntimeCloudSandbox {
		t.Fatal("expected same-ID rebind to preserve runtime for existing sandbox-backed terminal tabs")
	}
}

func TestRebindWorkspaceIDKeepsDetachedSandboxTerminalOnSandboxRuntime(t *testing.T) {
	oldWS := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo")
	oldWS.Runtime = data.RuntimeCloudSandbox
	newWS := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo")
	newWS.Runtime = data.RuntimeLocalWorktree

	m := NewTerminalModel()
	m.workspace = oldWS
	wsID := string(oldWS.ID())
	tabID := TerminalTabID("term-tab-1")
	m.tabsByWorkspace[wsID] = []*TerminalTab{{
		ID:        tabID,
		Name:      "Terminal 1",
		Workspace: oldWS,
		State: &TerminalState{
			Running:  false,
			Detached: true,
		},
	}}
	m.activeTabByWorkspace[wsID] = 0

	cmd := m.RebindWorkspaceID(oldWS, newWS)
	if cmd != nil {
		t.Fatal("expected no command for same-ID terminal workspace refresh")
	}

	called := false
	m.SetTerminalFactory(func(got *data.Workspace) (*pty.Terminal, error) {
		called = true
		if got == nil {
			t.Fatal("factory workspace is nil")
		}
		if data.NormalizeRuntime(got.Runtime) != data.RuntimeCloudSandbox {
			t.Fatalf("factory runtime = %q, want %q", got.Runtime, data.RuntimeCloudSandbox)
		}
		if string(got.ID()) != wsID {
			t.Fatalf("factory workspace ID = %q, want %q", got.ID(), wsID)
		}
		return nil, nil
	})

	reattach := m.ReattachActiveTab()
	if reattach == nil {
		t.Fatal("expected reattach command for detached terminal")
	}
	msg := reattach()
	if _, ok := msg.(SidebarTerminalReattachResult); !ok {
		t.Fatalf("expected SidebarTerminalReattachResult, got %T", msg)
	}
	if !called {
		t.Fatal("expected detached sandbox terminal to reattach through the sandbox factory")
	}
}
