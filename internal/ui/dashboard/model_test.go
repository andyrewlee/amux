package dashboard

import (
	"testing"

	"github.com/andyrewlee/amux/internal/data"
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
	if _, ok := msg.(messages.RefreshDashboard); !ok {
		t.Fatalf("expected RefreshDashboard message, got %T", msg)
	}
}

func TestMoveCursorRowByRow(t *testing.T) {
	// Create a model with a known row structure including spacers
	m := New()
	// Manually set up rows with spacers to test row-by-row walking
	m.rows = []Row{
		{Type: RowHome},     // 0: selectable
		{Type: RowProject},  // 1: selectable
		{Type: RowWorktree}, // 2: selectable
		{Type: RowSpacer},   // 3: NOT selectable
		{Type: RowProject},  // 4: selectable
		{Type: RowWorktree}, // 5: selectable
		{Type: RowSpacer},   // 6: NOT selectable
		{Type: RowProject},  // 7: selectable
	}

	t.Run("delta=1 moves one selectable row", func(t *testing.T) {
		m.cursor = 0
		m.moveCursor(1)
		if m.cursor != 1 {
			t.Fatalf("expected cursor at 1, got %d", m.cursor)
		}
	})

	t.Run("delta=2 moves two selectable rows", func(t *testing.T) {
		m.cursor = 0
		m.moveCursor(2)
		if m.cursor != 2 {
			t.Fatalf("expected cursor at 2, got %d", m.cursor)
		}
	})

	t.Run("delta=3 skips spacer and lands on row 4", func(t *testing.T) {
		// From row 0, move 3 steps: 0->1->2->4 (skipping spacer at 3)
		m.cursor = 0
		m.moveCursor(3)
		if m.cursor != 4 {
			t.Fatalf("expected cursor at 4 (skipping spacer at 3), got %d", m.cursor)
		}
	})

	t.Run("delta=4 lands on row 5", func(t *testing.T) {
		// From row 0, move 4 steps: 0->1->2->4->5
		m.cursor = 0
		m.moveCursor(4)
		if m.cursor != 5 {
			t.Fatalf("expected cursor at 5, got %d", m.cursor)
		}
	})

	t.Run("delta=5 skips two spacers and lands on row 7", func(t *testing.T) {
		// From row 0, move 5 steps: 0->1->2->4->5->7 (skipping spacers at 3 and 6)
		m.cursor = 0
		m.moveCursor(5)
		if m.cursor != 7 {
			t.Fatalf("expected cursor at 7 (skipping spacers), got %d", m.cursor)
		}
	})

	t.Run("large delta clamps at last selectable", func(t *testing.T) {
		m.cursor = 0
		m.moveCursor(100)
		if m.cursor != 7 {
			t.Fatalf("expected cursor clamped at 7, got %d", m.cursor)
		}
	})

	t.Run("negative delta=-1 moves up one", func(t *testing.T) {
		m.cursor = 5
		m.moveCursor(-1)
		if m.cursor != 4 {
			t.Fatalf("expected cursor at 4, got %d", m.cursor)
		}
	})

	t.Run("negative delta=-2 skips spacer going up", func(t *testing.T) {
		// From row 5, move -2 steps: 5->4->2 (skipping spacer at 3)
		m.cursor = 5
		m.moveCursor(-2)
		if m.cursor != 2 {
			t.Fatalf("expected cursor at 2 (skipping spacer at 3), got %d", m.cursor)
		}
	})

	t.Run("large negative delta clamps at first selectable", func(t *testing.T) {
		m.cursor = 7
		m.moveCursor(-100)
		if m.cursor != 0 {
			t.Fatalf("expected cursor clamped at 0, got %d", m.cursor)
		}
	})
}

func TestMoveCursorFromSpacerPosition(t *testing.T) {
	// Edge case: what if cursor is somehow on a spacer?
	m := New()
	m.rows = []Row{
		{Type: RowHome},    // 0: selectable
		{Type: RowSpacer},  // 1: NOT selectable
		{Type: RowProject}, // 2: selectable
	}

	t.Run("move down from spacer", func(t *testing.T) {
		m.cursor = 1 // On spacer
		m.moveCursor(1)
		if m.cursor != 2 {
			t.Fatalf("expected cursor at 2, got %d", m.cursor)
		}
	})

	t.Run("move up from spacer", func(t *testing.T) {
		m.cursor = 1 // On spacer
		m.moveCursor(-1)
		if m.cursor != 0 {
			t.Fatalf("expected cursor at 0, got %d", m.cursor)
		}
	})
}

func TestVisibleHeightPageMovement(t *testing.T) {
	m := New()
	// Set up a taller dashboard with many rows
	m.rows = []Row{
		{Type: RowHome},     // 0
		{Type: RowProject},  // 1
		{Type: RowWorktree}, // 2
		{Type: RowWorktree}, // 3
		{Type: RowSpacer},   // 4
		{Type: RowProject},  // 5
		{Type: RowWorktree}, // 6
		{Type: RowWorktree}, // 7
		{Type: RowSpacer},   // 8
		{Type: RowProject},  // 9
		{Type: RowWorktree}, // 10
		{Type: RowWorktree}, // 11
	}

	// Set a size that gives us a visible height of ~5
	m.SetSize(80, 15)
	m.showKeymapHints = false // Simplify height calculation

	visibleH := m.visibleHeight()
	if visibleH < 1 {
		t.Fatalf("visible height should be positive, got %d", visibleH)
	}

	// Half-page scroll for context overlap
	halfPage := visibleH / 2
	if halfPage < 1 {
		halfPage = 1
	}

	t.Run("page down moves by half visible height", func(t *testing.T) {
		m.cursor = 0
		m.moveCursor(halfPage)
		// Should have moved halfPage selectable rows (skipping spacers)
		if m.cursor <= 0 {
			t.Fatalf("expected cursor to move forward, got %d", m.cursor)
		}
		// Should not have moved full visible height
		if m.cursor > halfPage+1 { // +1 for possible spacer skip
			t.Fatalf("expected cursor to move by approximately half page, got %d (halfPage=%d)", m.cursor, halfPage)
		}
	})

	t.Run("page up moves by half visible height", func(t *testing.T) {
		m.cursor = 11 // Start at end
		m.moveCursor(-halfPage)
		if m.cursor >= 11 {
			t.Fatalf("expected cursor to move backward, got %d", m.cursor)
		}
	})
}

func TestMoveCursorEmptyRows(t *testing.T) {
	m := New()
	m.rows = []Row{}

	// Should not panic
	m.cursor = 0
	m.moveCursor(1)
	m.moveCursor(-1)

	if m.cursor != 0 {
		t.Fatalf("cursor should remain at 0 for empty rows, got %d", m.cursor)
	}
}

func TestMoveCursorAllSpacers(t *testing.T) {
	m := New()
	m.rows = []Row{
		{Type: RowSpacer},
		{Type: RowSpacer},
		{Type: RowSpacer},
	}

	// From any position, should not move since no selectable rows
	m.cursor = 1
	originalCursor := m.cursor
	m.moveCursor(1)
	if m.cursor != originalCursor {
		t.Fatalf("cursor should not move when no selectable rows exist, got %d", m.cursor)
	}

	m.moveCursor(-1)
	if m.cursor != originalCursor {
		t.Fatalf("cursor should not move when no selectable rows exist, got %d", m.cursor)
	}
}
