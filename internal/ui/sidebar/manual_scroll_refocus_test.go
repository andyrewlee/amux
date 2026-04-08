package sidebar

import (
	"fmt"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
)

func TestChangesFocusIsIdempotentWhileAlreadyFocused(t *testing.T) {
	m := setupChangesScrollModel()
	m.cursor = 1
	m.scrollOffset = 6

	if changesCursorVisible(m) {
		t.Fatalf("expected cursor to start hidden, cursor=%d scrollOffset=%d visibleHeight=%d",
			m.cursor, m.scrollOffset, m.visibleHeight())
	}

	m.Focus()

	if m.scrollOffset != 6 {
		t.Fatalf("scrollOffset = %d, want 6", m.scrollOffset)
	}
	if changesCursorVisible(m) {
		t.Fatalf("expected redundant Focus to preserve hidden cursor, cursor=%d scrollOffset=%d visibleHeight=%d",
			m.cursor, m.scrollOffset, m.visibleHeight())
	}

	m.Blur()
	m.Focus()

	if !changesCursorVisible(m) {
		t.Fatalf("expected Focus after blur to reanchor cursor, cursor=%d scrollOffset=%d visibleHeight=%d",
			m.cursor, m.scrollOffset, m.visibleHeight())
	}
}

func TestTabbedInactiveChangesPreserveManualScrollOffsetUntilRefocus(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*TabbedSidebar)
	}{
		{
			name: "resize",
			apply: func(sidebar *TabbedSidebar) {
				sidebar.SetSize(80, 8)
			},
		},
		{
			name: "show keymap hints",
			apply: func(sidebar *TabbedSidebar) {
				sidebar.SetShowKeymapHints(true)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sidebar := setupTabbedChangesScrollModel()
			changes := sidebar.Changes()
			changes.cursor = 1
			changes.scrollOffset = 6

			sidebar.SetActiveTab(TabProject)
			tt.apply(sidebar)

			if changes.scrollOffset != 6 {
				t.Fatalf("scrollOffset = %d, want 6", changes.scrollOffset)
			}
			if changesCursorVisible(changes) {
				t.Fatalf("expected cursor to remain hidden while inactive, cursor=%d scrollOffset=%d visibleHeight=%d",
					changes.cursor, changes.scrollOffset, changes.visibleHeight())
			}

			sidebar.SetActiveTab(TabChanges)

			if !changesCursorVisible(changes) {
				t.Fatalf("expected cursor to be visible after refocus, cursor=%d scrollOffset=%d visibleHeight=%d",
					changes.cursor, changes.scrollOffset, changes.visibleHeight())
			}
		})
	}
}

func TestTabbedInactiveChangesSkipCursorRepairOnGitStatusRefresh(t *testing.T) {
	sidebar := setupTabbedChangesScrollModel()
	changes := sidebar.Changes()
	changes.cursor = 7
	changes.scrollOffset = 6

	if !changesCursorVisible(changes) {
		t.Fatalf("expected cursor to start visible, cursor=%d scrollOffset=%d visibleHeight=%d",
			changes.cursor, changes.scrollOffset, changes.visibleHeight())
	}
	if got := topVisibleChangePath(changes); got != "file-05.txt" {
		t.Fatalf("top visible change = %q, want %q", got, "file-05.txt")
	}

	unstaged := make([]git.Change, 0, 20)
	for i := 0; i < 20; i++ {
		unstaged = append(unstaged, git.Change{
			Path: fmt.Sprintf("file-%02d.txt", i),
			Kind: git.ChangeModified,
		})
	}

	sidebar.SetActiveTab(TabProject)
	sidebar.SetGitStatus(&git.StatusResult{
		Clean: false,
		Staged: []git.Change{
			{Path: "staged.txt", Kind: git.ChangeModified, Staged: true},
		},
		Unstaged: unstaged,
	})

	if got := topVisibleChangePath(changes); got != "file-05.txt" {
		t.Fatalf("top visible change after background refresh = %q, want %q", got, "file-05.txt")
	}
	if changesCursorVisible(changes) {
		t.Fatalf("expected cursor to remain hidden while inactive after refresh, cursor=%d scrollOffset=%d visibleHeight=%d",
			changes.cursor, changes.scrollOffset, changes.visibleHeight())
	}

	sidebar.SetActiveTab(TabChanges)

	if !changesCursorVisible(changes) {
		t.Fatalf("expected cursor to be visible after refocus, cursor=%d scrollOffset=%d visibleHeight=%d",
			changes.cursor, changes.scrollOffset, changes.visibleHeight())
	}
}

func TestTabbedInactiveChangesSkipCursorRepairOnWorkspaceRebind(t *testing.T) {
	sidebar := setupTabbedChangesScrollModel()
	changes := sidebar.Changes()
	ws1 := data.NewWorkspace("feature", "", "main", "/tmp/repo", "/tmp/workspaces/repo/feature")
	ws2 := data.NewWorkspace("feature", "updated-branch", "main", "/tmp/repo", "/tmp/workspaces/repo/feature")
	sidebar.SetWorkspace(ws1)
	changes.cursor = 13
	changes.scrollOffset = 6

	if !changesCursorVisible(changes) {
		t.Fatalf("expected cursor to start visible, cursor=%d scrollOffset=%d visibleHeight=%d",
			changes.cursor, changes.scrollOffset, changes.visibleHeight())
	}

	sidebar.SetActiveTab(TabProject)
	sidebar.SetWorkspace(ws2)

	if changes.scrollOffset != 6 {
		t.Fatalf("scrollOffset = %d, want 6 while inactive", changes.scrollOffset)
	}
	if changesCursorVisible(changes) {
		t.Fatalf("expected cursor to remain hidden while inactive after workspace rebind, cursor=%d scrollOffset=%d visibleHeight=%d",
			changes.cursor, changes.scrollOffset, changes.visibleHeight())
	}

	sidebar.SetActiveTab(TabChanges)

	if !changesCursorVisible(changes) {
		t.Fatalf("expected cursor to be visible after refocus, cursor=%d scrollOffset=%d visibleHeight=%d",
			changes.cursor, changes.scrollOffset, changes.visibleHeight())
	}
}

func TestProjectTreeFocusIsIdempotentWhileAlreadyFocused(t *testing.T) {
	tree := setupProjectTreeScrollModel(t)
	tree.cursor = 1
	tree.scrollOffset = 6

	if treeCursorVisible(tree) {
		t.Fatalf("expected cursor to start hidden, cursor=%d scrollOffset=%d visibleHeight=%d",
			tree.cursor, tree.scrollOffset, tree.visibleHeight())
	}

	tree.Focus()

	if tree.scrollOffset != 6 {
		t.Fatalf("scrollOffset = %d, want 6", tree.scrollOffset)
	}
	if treeCursorVisible(tree) {
		t.Fatalf("expected redundant Focus to preserve hidden cursor, cursor=%d scrollOffset=%d visibleHeight=%d",
			tree.cursor, tree.scrollOffset, tree.visibleHeight())
	}

	tree.Blur()
	tree.Focus()

	if !treeCursorVisible(tree) {
		t.Fatalf("expected Focus after blur to reanchor cursor, cursor=%d scrollOffset=%d visibleHeight=%d",
			tree.cursor, tree.scrollOffset, tree.visibleHeight())
	}
}

func TestTabbedInactiveProjectPreserveManualScrollOffsetUntilRefocus(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*TabbedSidebar)
	}{
		{
			name: "resize",
			apply: func(sidebar *TabbedSidebar) {
				sidebar.SetSize(80, 8)
			},
		},
		{
			name: "show keymap hints",
			apply: func(sidebar *TabbedSidebar) {
				sidebar.SetShowKeymapHints(true)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sidebar := setupTabbedProjectScrollModel(t)
			tree := sidebar.ProjectTree()
			tree.cursor = 1
			tree.scrollOffset = 6

			sidebar.SetActiveTab(TabChanges)
			tt.apply(sidebar)

			if tree.scrollOffset != 6 {
				t.Fatalf("scrollOffset = %d, want 6", tree.scrollOffset)
			}
			if treeCursorVisible(tree) {
				t.Fatalf("expected cursor to remain hidden while inactive, cursor=%d scrollOffset=%d visibleHeight=%d",
					tree.cursor, tree.scrollOffset, tree.visibleHeight())
			}

			sidebar.SetActiveTab(TabProject)

			if !treeCursorVisible(tree) {
				t.Fatalf("expected cursor to be visible after refocus, cursor=%d scrollOffset=%d visibleHeight=%d",
					tree.cursor, tree.scrollOffset, tree.visibleHeight())
			}
		})
	}
}
