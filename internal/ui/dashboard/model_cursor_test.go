package dashboard

import "testing"

func TestMoveCursorRowByRow(t *testing.T) {
	// Create a model with a known row structure including spacers
	m := New()
	// Manually set up rows with spacers to test row-by-row walking
	m.rows = []Row{
		{Type: RowHome},      // 0: selectable
		{Type: RowProject},   // 1: selectable
		{Type: RowWorkspace}, // 2: selectable
		{Type: RowSpacer},    // 3: NOT selectable
		{Type: RowProject},   // 4: selectable
		{Type: RowWorkspace}, // 5: selectable
		{Type: RowSpacer},    // 6: NOT selectable
		{Type: RowProject},   // 7: selectable
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
		{Type: RowHome},      // 0
		{Type: RowProject},   // 1
		{Type: RowWorkspace}, // 2
		{Type: RowWorkspace}, // 3
		{Type: RowSpacer},    // 4
		{Type: RowProject},   // 5
		{Type: RowWorkspace}, // 6
		{Type: RowWorkspace}, // 7
		{Type: RowSpacer},    // 8
		{Type: RowProject},   // 9
		{Type: RowWorkspace}, // 10
		{Type: RowWorkspace}, // 11
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
