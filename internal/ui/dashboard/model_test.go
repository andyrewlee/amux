package dashboard

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
)

func makeProject() data.Project {
	return data.Project{
		Name: "repo",
		Path: "/repo",
		Workspaces: []data.Workspace{
			{Name: "repo", Branch: "main", Repo: "/repo", Root: "/repo"},
			{Name: "feature", Branch: "feature", Repo: "/repo", Root: "/repo/.amux/workspaces/feature"},
		},
	}
}

func TestDashboardRebuildRowsSkipsMainAndPrimary(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})

	var workspaceRows int
	var projectRows int
	for _, row := range m.rows {
		switch row.Type {
		case RowWorkspace:
			workspaceRows++
		case RowProject:
			projectRows++
		}
	}

	if projectRows != 1 {
		t.Fatalf("expected 1 project row, got %d", projectRows)
	}
	if workspaceRows != 1 {
		t.Fatalf("expected only non-main/non-primary workspace rows, got %d", workspaceRows)
	}
}

func TestDashboardCursorMovement(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})

	t.Run("move down", func(t *testing.T) {
		m.cursor = 0
		m.moveCursor(1)
		if m.cursor != 2 {
			t.Fatalf("expected cursor at 2, got %d", m.cursor)
		}
	})

	t.Run("move up", func(t *testing.T) {
		m.cursor = 2
		m.moveCursor(-1)
		if m.cursor != 0 {
			t.Fatalf("expected cursor at 0, got %d", m.cursor)
		}
	})

	t.Run("skip spacer rows", func(t *testing.T) {
		// Find a spacer row and try to land on it
		for i, row := range m.rows {
			if row.Type == RowSpacer && i > 0 {
				m.cursor = i - 1
				m.moveCursor(1)
				// Should skip the spacer
				if m.rows[m.cursor].Type == RowSpacer {
					t.Fatalf("cursor should skip spacer rows")
				}
				break
			}
		}
	})

	t.Run("clamp at top", func(t *testing.T) {
		m.cursor = 0
		m.moveCursor(-10)
		if m.cursor < 0 {
			t.Fatalf("cursor should not go below 0")
		}
	})

	t.Run("clamp at bottom", func(t *testing.T) {
		m.cursor = len(m.rows) - 1
		m.moveCursor(10)
		if m.cursor >= len(m.rows) {
			t.Fatalf("cursor should not exceed rows length")
		}
	})
}

func TestDashboardFocus(t *testing.T) {
	m := New()

	t.Run("initial focus", func(t *testing.T) {
		if !m.Focused() {
			t.Fatalf("expected dashboard to be focused by default")
		}
	})

	t.Run("blur", func(t *testing.T) {
		m.Blur()
		if m.Focused() {
			t.Fatalf("expected dashboard to be blurred after Blur()")
		}
	})

	t.Run("focus", func(t *testing.T) {
		m.Blur()
		m.Focus()
		if !m.Focused() {
			t.Fatalf("expected dashboard to be focused after Focus()")
		}
	})
}

func TestDashboardSelectedRow(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})

	t.Run("valid cursor", func(t *testing.T) {
		m.cursor = 0
		row := m.SelectedRow()
		if row == nil {
			t.Fatalf("expected non-nil row")
		}
		if row.Type != RowHome {
			t.Fatalf("expected RowHome, got %v", row.Type)
		}
	})

	t.Run("cursor at project", func(t *testing.T) {
		m.cursor = 2 // Project row
		row := m.SelectedRow()
		if row == nil {
			t.Fatalf("expected non-nil row")
		}
		if row.Type != RowProject {
			t.Fatalf("expected RowProject, got %v", row.Type)
		}
	})
}

func TestDashboardSetSize(t *testing.T) {
	m := New()
	m.SetSize(100, 50)

	if m.width != 100 {
		t.Fatalf("expected width 100, got %d", m.width)
	}
	if m.height != 50 {
		t.Fatalf("expected height 50, got %d", m.height)
	}
}

func TestDashboardProjects(t *testing.T) {
	m := New()
	projects := []data.Project{makeProject()}
	m.SetProjects(projects)

	got := m.Projects()
	if len(got) != 1 {
		t.Fatalf("expected 1 project, got %d", len(got))
	}
	if got[0].Name != "repo" {
		t.Fatalf("expected project name 'repo', got %s", got[0].Name)
	}
}

func TestDashboardEmptyState(t *testing.T) {
	m := New()
	// Set empty projects to trigger rebuildRows
	m.SetProjects([]data.Project{})

	// Should still have Home row
	if len(m.rows) < 1 {
		t.Fatalf("expected at least 1 row (Home), got %d", len(m.rows))
	}

	if m.rows[0].Type != RowHome {
		t.Fatalf("expected first row to be RowHome")
	}
}

func TestDashboardRefresh(t *testing.T) {
	m := New()

	cmd := m.refresh()
	if cmd == nil {
		t.Fatalf("expected refresh to return a command")
	}

	msg := cmd()
	if _, ok := msg.(messages.RescanWorkspaces); !ok {
		t.Fatalf("expected RescanWorkspaces message, got %T", msg)
	}
}

func TestDashboardInvalidateStatus(t *testing.T) {
	m := New()
	root := "/test/workspace"

	t.Run("invalidates cached status", func(t *testing.T) {
		// Pre-populate the cache with a dirty status
		m.statusCache[root] = &git.StatusResult{
			Unstaged: []git.Change{{Path: "test.go", Kind: git.ChangeModified}},
		}

		// Invalidate
		m.InvalidateStatus(root)

		// Verify cache is cleared
		if _, ok := m.statusCache[root]; ok {
			t.Fatal("expected statusCache entry to be deleted")
		}
	})

	t.Run("handles non-existent root", func(t *testing.T) {
		// Should not panic when invalidating a root that isn't cached
		m.InvalidateStatus("/non/existent/path")
	})

	t.Run("multiple invalidations", func(t *testing.T) {
		root1 := "/workspace1"
		root2 := "/workspace2"

		// Cache both
		m.statusCache[root1] = &git.StatusResult{Clean: true}
		m.statusCache[root2] = &git.StatusResult{Clean: true}

		// Invalidate first
		m.InvalidateStatus(root1)

		// First should be invalidated, second should remain
		if _, ok := m.statusCache[root1]; ok {
			t.Fatal("expected root1 to be invalidated")
		}
		if _, ok := m.statusCache[root2]; !ok {
			t.Fatal("expected root2 to remain cached")
		}
	})
}

func TestSpinnerOnlyForCreateDelete(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})

	t.Run("spinner not active initially", func(t *testing.T) {
		if m.spinnerActive {
			t.Fatal("spinner should not be active initially")
		}
	})

	t.Run("spinner activates on workspace creation", func(t *testing.T) {
		ws := &data.Workspace{
			Name:   "new-ws",
			Branch: "feature",
			Repo:   "/repo",
			Root:   "/repo/.amux/workspaces/new-ws",
		}
		cmd := m.SetWorkspaceCreating(ws, true)
		if cmd == nil {
			t.Fatal("expected command to start spinner")
		}
		if !m.spinnerActive {
			t.Fatal("spinner should be active during creation")
		}
		if len(m.creatingWorkspaces) != 1 {
			t.Fatalf("expected 1 creating workspace, got %d", len(m.creatingWorkspaces))
		}
	})

	t.Run("spinner deactivates when creation completes", func(t *testing.T) {
		ws := &data.Workspace{
			Name:   "new-ws",
			Branch: "feature",
			Repo:   "/repo",
			Root:   "/repo/.amux/workspaces/new-ws",
		}
		m.SetWorkspaceCreating(ws, false)
		if len(m.creatingWorkspaces) != 0 {
			t.Fatalf("expected 0 creating workspaces, got %d", len(m.creatingWorkspaces))
		}
	})

	t.Run("spinner activates on workspace deletion", func(t *testing.T) {
		m.spinnerActive = false
		cmd := m.SetWorkspaceDeleting("/some/root", true)
		if cmd == nil {
			t.Fatal("expected command to start spinner")
		}
		if !m.spinnerActive {
			t.Fatal("spinner should be active during deletion")
		}
		if len(m.deletingWorkspaces) != 1 {
			t.Fatalf("expected 1 deleting workspace, got %d", len(m.deletingWorkspaces))
		}
	})

	t.Run("spinner deactivates when deletion completes", func(t *testing.T) {
		m.SetWorkspaceDeleting("/some/root", false)
		if len(m.deletingWorkspaces) != 0 {
			t.Fatalf("expected 0 deleting workspaces, got %d", len(m.deletingWorkspaces))
		}
	})

	t.Run("git status refresh does not activate spinner", func(t *testing.T) {
		m.spinnerActive = false
		m.InvalidateStatus("/repo")
		// Spinner should remain inactive - no loading status tracking
		cmd := m.startSpinnerIfNeeded()
		if cmd != nil {
			t.Fatal("spinner should not start for git status refresh")
		}
	})
}
