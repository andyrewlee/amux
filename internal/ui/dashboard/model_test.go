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
		Worktrees: []data.Worktree{
			{Name: "repo", Branch: "main", Repo: "/repo", Root: "/repo"},
			{Name: "feature", Branch: "feature", Repo: "/repo", Root: "/repo/.amux/worktrees/feature"},
		},
	}
}

func TestDashboardRebuildRowsSkipsMainAndPrimary(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})

	var worktreeRows int
	var projectRows int
	for _, row := range m.rows {
		switch row.Type {
		case RowWorktree:
			worktreeRows++
		case RowProject:
			projectRows++
		}
	}

	if projectRows != 1 {
		t.Fatalf("expected 1 project row, got %d", projectRows)
	}
	if worktreeRows != 1 {
		t.Fatalf("expected only non-main/non-primary worktree rows, got %d", worktreeRows)
	}
}

func TestDashboardDirtyFilter(t *testing.T) {
	m := New()
	project := makeProject()
	m.SetProjects([]data.Project{project})

	root := project.Worktrees[1].Root
	m.statusCache[root] = &git.StatusResult{Clean: true}
	m.filterDirty = true
	m.rebuildRows()

	for _, row := range m.rows {
		if row.Type == RowWorktree {
			t.Fatalf("expected clean worktree to be hidden by dirty filter")
		}
	}

	m.statusCache[root] = &git.StatusResult{Clean: false}
	m.rebuildRows()

	found := false
	for _, row := range m.rows {
		if row.Type == RowWorktree {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected dirty worktree to be visible when filter enabled")
	}
}

func TestDashboardHandleEnterProjectSelectsMain(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})

	// Row order: Home, AddProject, Project...
	m.cursor = 2
	cmd := m.handleEnter()
	if cmd == nil {
		t.Fatalf("expected handleEnter to return a command")
	}

	msg := cmd()
	activated, ok := msg.(messages.WorktreeActivated)
	if !ok {
		t.Fatalf("expected WorktreeActivated, got %T", msg)
	}
	if activated.Worktree == nil || activated.Worktree.Branch != "main" {
		t.Fatalf("expected main worktree activation, got %+v", activated.Worktree)
	}
}

func TestDashboardCursorMovement(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})

	t.Run("move down", func(t *testing.T) {
		m.cursor = 0
		m.moveCursor(1)
		if m.cursor != 1 {
			t.Fatalf("expected cursor at 1, got %d", m.cursor)
		}
	})

	t.Run("move up", func(t *testing.T) {
		m.cursor = 1
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

func TestDashboardHandleEnterHome(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})
	m.cursor = 0 // Home row

	cmd := m.handleEnter()
	if cmd == nil {
		t.Fatalf("expected handleEnter to return a command")
	}

	msg := cmd()
	if _, ok := msg.(messages.ShowWelcome); !ok {
		t.Fatalf("expected ShowWelcome message, got %T", msg)
	}
}

func TestDashboardHandleEnterAddProject(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})
	m.cursor = 1 // AddProject row

	cmd := m.handleEnter()
	if cmd == nil {
		t.Fatalf("expected handleEnter to return a command")
	}

	msg := cmd()
	if _, ok := msg.(messages.ShowAddProjectDialog); !ok {
		t.Fatalf("expected ShowAddProjectDialog message, got %T", msg)
	}
}

func TestDashboardHandleEnterWorktree(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})

	// Find a worktree row
	for i, row := range m.rows {
		if row.Type == RowWorktree {
			m.cursor = i
			break
		}
	}

	cmd := m.handleEnter()
	if cmd == nil {
		t.Fatalf("expected handleEnter to return a command")
	}

	msg := cmd()
	activated, ok := msg.(messages.WorktreeActivated)
	if !ok {
		t.Fatalf("expected WorktreeActivated message, got %T", msg)
	}
	if activated.Worktree == nil {
		t.Fatalf("expected worktree in activation message")
	}
}

func TestDashboardHandleEnterCreate(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})

	// Find a create row
	for i, row := range m.rows {
		if row.Type == RowCreate {
			m.cursor = i
			break
		}
	}

	cmd := m.handleEnter()
	if cmd == nil {
		t.Fatalf("expected handleEnter to return a command")
	}

	msg := cmd()
	if _, ok := msg.(messages.ShowCreateWorktreeDialog); !ok {
		t.Fatalf("expected ShowCreateWorktreeDialog message, got %T", msg)
	}
}

func TestDashboardHandleNew(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})

	// Cursor on project row
	m.cursor = 2
	cmd := m.handleNew()
	if cmd == nil {
		t.Fatalf("expected handleNew to return a command")
	}

	msg := cmd()
	dialog, ok := msg.(messages.ShowCreateWorktreeDialog)
	if !ok {
		t.Fatalf("expected ShowCreateWorktreeDialog message, got %T", msg)
	}
	if dialog.Project == nil {
		t.Fatalf("expected project in dialog message")
	}
}

func TestDashboardHandleDelete(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})

	// Find a worktree row
	for i, row := range m.rows {
		if row.Type == RowWorktree {
			m.cursor = i
			break
		}
	}

	cmd := m.handleDelete()
	if cmd == nil {
		t.Fatalf("expected handleDelete to return a command")
	}

	msg := cmd()
	dialog, ok := msg.(messages.ShowDeleteWorktreeDialog)
	if !ok {
		t.Fatalf("expected ShowDeleteWorktreeDialog message, got %T", msg)
	}
	if dialog.Worktree == nil {
		t.Fatalf("expected worktree in dialog message")
	}
}

func TestDashboardHandleDeleteNonWorktree(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})
	m.cursor = 0 // Home row

	cmd := m.handleDelete()
	if cmd != nil {
		t.Fatalf("expected handleDelete to return nil for non-worktree row")
	}
}

func TestDashboardCreatingWorktreeRow(t *testing.T) {
	m := New()
	project := makeProject()
	m.SetProjects([]data.Project{project})
	m.filterDirty = true

	wt := data.NewWorktree("creating", "creating", "HEAD", project.Path, project.Path+"/.amux/worktrees/creating")
	m.SetWorktreeCreating(wt, true)

	found := false
	for _, row := range m.rows {
		if row.Type == RowWorktree && row.Worktree != nil && row.Worktree.Root == wt.Root {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected creating worktree to be visible in rows")
	}
}

func TestDashboardToggleFilter(t *testing.T) {
	m := New()
	m.SetProjects([]data.Project{makeProject()})

	if m.filterDirty {
		t.Fatalf("expected filterDirty to be false by default")
	}

	m.toggleFilter()
	if !m.filterDirty {
		t.Fatalf("expected filterDirty to be true after toggle")
	}

	m.toggleFilter()
	if m.filterDirty {
		t.Fatalf("expected filterDirty to be false after second toggle")
	}
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

	// Should still have Home and AddProject rows
	if len(m.rows) < 2 {
		t.Fatalf("expected at least 2 rows (Home, AddProject), got %d", len(m.rows))
	}

	if m.rows[0].Type != RowHome {
		t.Fatalf("expected first row to be RowHome")
	}
	if m.rows[1].Type != RowAddProject {
		t.Fatalf("expected second row to be RowAddProject")
	}
}

func TestDashboardRefresh(t *testing.T) {
	m := New()

	cmd := m.refresh()
	if cmd == nil {
		t.Fatalf("expected refresh to return a command")
	}

	msg := cmd()
	if _, ok := msg.(messages.RefreshDashboard); !ok {
		t.Fatalf("expected RefreshDashboard message, got %T", msg)
	}
}
