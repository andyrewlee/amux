package center

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func TestNextAssistantName(t *testing.T) {
	tabs := []*Tab{
		{Assistant: "codex", Name: "codex"},
		{Assistant: "codex", Name: "codex 1"},
	}

	if got := nextAssistantName("codex", tabs); got != "codex 2" {
		t.Fatalf("expected codex 2, got %q", got)
	}

	if got := nextAssistantName("claude", tabs); got != "claude" {
		t.Fatalf("expected claude, got %q", got)
	}

	tabs = []*Tab{
		{Assistant: "codex", Name: "codex 1"},
	}
	if got := nextAssistantName("codex", tabs); got != "codex" {
		t.Fatalf("expected codex when base available, got %q", got)
	}
}

func TestMonitorTabsIncludesAllTabs(t *testing.T) {
	wtA := &data.Workspace{Name: "alpha", Repo: "/repoA", Root: "/repoA/alpha"}
	wtB := &data.Workspace{Name: "beta", Repo: "/repoB", Root: "/repoB/beta"}

	model := &Model{
		tabsByWorkspace: map[string][]*Tab{
			string(wtA.ID()): {
				{ID: "tab-1", Workspace: wtA, Assistant: "codex", Name: "codex"},
				{ID: "tab-2", Workspace: wtA, Assistant: "codex", Name: "codex 1"},
			},
			string(wtB.ID()): {
				{ID: "tab-3", Workspace: wtB, Assistant: "claude", Name: "claude"},
			},
		},
	}

	tabs := model.MonitorTabs()
	if len(tabs) != 3 {
		t.Fatalf("expected 3 tabs, got %d", len(tabs))
	}

	if tabs[0].ID != "tab-1" || tabs[1].ID != "tab-2" || tabs[2].ID != "tab-3" {
		t.Fatalf("unexpected monitor tab order: %+v", tabs)
	}
}

func TestCleanupWorkspace(t *testing.T) {
	wtA := &data.Workspace{Name: "alpha", Repo: "/repoA", Root: "/repoA/alpha"}
	wtB := &data.Workspace{Name: "beta", Repo: "/repoB", Root: "/repoB/beta"}

	model := &Model{
		tabsByWorkspace: map[string][]*Tab{
			string(wtA.ID()): {
				{ID: "tab-1", Workspace: wtA, Assistant: "codex", Name: "codex"},
			},
			string(wtB.ID()): {
				{ID: "tab-2", Workspace: wtB, Assistant: "claude", Name: "claude"},
			},
		},
		activeTabByWorkspace: map[string]int{
			string(wtA.ID()): 0,
			string(wtB.ID()): 0,
		},
	}

	model.CleanupWorkspace(wtA)

	if _, exists := model.tabsByWorkspace[string(wtA.ID())]; exists {
		t.Fatalf("expected wtA tabs to be deleted")
	}
	if _, exists := model.activeTabByWorkspace[string(wtA.ID())]; exists {
		t.Fatalf("expected wtA active tab index to be deleted")
	}

	if len(model.tabsByWorkspace[string(wtB.ID())]) != 1 {
		t.Fatalf("expected wtB tabs to remain unchanged")
	}

	model.CleanupWorkspace(nil)
}
