package vterm

import (
	"strings"
	"testing"
)

// rowText extracts visible text from a screen row, skipping continuation
// cells (Width==0) and replacing NUL runes with space. Trailing spaces
// are trimmed so tests can use compact string literals.
func rowText(v *VTerm, y int) string {
	var b strings.Builder
	for _, c := range v.VisibleScreen()[y] {
		if c.Width == 0 {
			continue
		}
		r := c.Rune
		if r == 0 {
			r = ' '
		}
		b.WriteRune(r)
	}
	return strings.TrimRight(b.String(), " ")
}

// TestLineEditInsertLinesMidRegion verifies insertLines pushes content down
// within the scroll region and fills vacated lines with blanks.
func TestLineEditInsertLinesMidRegion(t *testing.T) {
	t.Parallel()

	// 10-wide, 5-tall terminal.
	vt := New(10, 5)
	// Write four content rows via the terminal.
	vt.Write([]byte("A\r\nB\r\nC\r\nD"))
	// \x1b[2;1H = cursor to row 2, col 1 (1-indexed) → (row=1, col=0).
	vt.Write([]byte("\x1b[2;1H"))
	// Insert 2 lines.
	vt.Write([]byte("\x1b[2L"))

	// Row 0 must still be A.
	if got := rowText(vt, 0); got != "A" {
		t.Errorf("row 0 = %q, want %q", got, "A")
	}
	// Rows 1 and 2 must be blank (inserted).
	if got := rowText(vt, 1); got != "" {
		t.Errorf("row 1 = %q, want empty", got)
	}
	if got := rowText(vt, 2); got != "" {
		t.Errorf("row 2 = %q, want empty", got)
	}
	// B shifts to row 3.
	if got := rowText(vt, 3); got != "B" {
		t.Errorf("row 3 = %q, want %q", got, "B")
	}
	// C shifts to row 4; D is pushed off the bottom of the scroll region.
	if got := rowText(vt, 4); got != "C" {
		t.Errorf("row 4 = %q, want %q", got, "C")
	}
}

// TestLineEditInsertLinesOutsideRegionIsNoOp verifies that insertLines is a
// no-op when the cursor is above the scroll region.
func TestLineEditInsertLinesOutsideRegionIsNoOp(t *testing.T) {
	t.Parallel()

	vt := New(10, 5)
	// Write content on all rows.
	vt.Write([]byte("A\r\nB\r\nC\r\nD\r\nE"))
	// Set scroll region to rows 2..4 (1-indexed) → ScrollTop=1, ScrollBottom=4.
	vt.Write([]byte("\x1b[2;4r"))
	// Home cursor (inside the region after setScrollRegion always homes cursor).
	// Move cursor explicitly to row 0 (above scroll region top=1).
	vt.CursorY = 0

	// Capture row contents before the insert attempt.
	before := make([]string, 5)
	for i := 0; i < 5; i++ {
		before[i] = rowText(vt, i)
	}

	// \x1b[1L = insert 1 line — must be a no-op because cursor is outside region.
	vt.Write([]byte("\x1b[1L"))

	for i := 0; i < 5; i++ {
		if got := rowText(vt, i); got != before[i] {
			t.Errorf("row %d changed after no-op insert: got %q, want %q", i, got, before[i])
		}
	}
}

// TestLineEditInsertLinesClamps verifies that an oversized n is clamped to the
// remaining space in the scroll region and does not panic.
func TestLineEditInsertLinesClamps(t *testing.T) {
	t.Parallel()

	vt := New(10, 5)
	vt.Write([]byte("A\r\nB\r\nC\r\nD\r\nE"))
	// Cursor to last row of scroll region (row 4, 0-indexed = row 5, 1-indexed).
	vt.Write([]byte("\x1b[5;1H"))

	// Insert 99 lines — should clamp to 1 (ScrollBottom-CursorY = 5-4 = 1).
	vt.Write([]byte("\x1b[99L"))

	// Row 4 must now be blank (the one inserted line).
	if got := rowText(vt, 4); got != "" {
		t.Errorf("row 4 = %q, want empty after clamped insertLines", got)
	}
}

// TestLineEditDeleteLinesMidRegion verifies deleteLines pulls content up and
// fills vacated lines at the bottom with blanks.
func TestLineEditDeleteLinesMidRegion(t *testing.T) {
	t.Parallel()

	vt := New(10, 5)
	vt.Write([]byte("A\r\nB\r\nC\r\nD"))
	// Cursor to row 1.
	vt.Write([]byte("\x1b[2;1H"))
	// Delete 2 lines.
	vt.Write([]byte("\x1b[2M"))

	// Row 0 unchanged.
	if got := rowText(vt, 0); got != "A" {
		t.Errorf("row 0 = %q, want %q", got, "A")
	}
	// Deleting 2 lines at cursor (row 1) removes B and C.
	// D (row 3) and then blank (row 4) pull up to fill the gap.
	if got := rowText(vt, 1); got != "D" {
		t.Errorf("row 1 = %q, want %q (D pulls up)", got, "D")
	}
	// Row 2 was blank (row 4 shifted up).
	if got := rowText(vt, 2); got != "" {
		t.Errorf("row 2 = %q, want empty", got)
	}
	// Bottom two rows become blank (vacated by the shift).
	if got := rowText(vt, 3); got != "" {
		t.Errorf("row 3 = %q, want empty", got)
	}
	if got := rowText(vt, 4); got != "" {
		t.Errorf("row 4 = %q, want empty", got)
	}
}

// TestLineEditInsertChars verifies insertChars shifts content right and
// inserts blank cells at the cursor column, truncating at the line width.
func TestLineEditInsertChars(t *testing.T) {
	t.Parallel()

	// 8-wide terminal so there's room to observe shift + truncation.
	vt := New(8, 3)
	vt.Write([]byte("ABCDE"))
	// Cursor to row 1, col 3 (1-indexed) → (row=0, col=2).
	vt.Write([]byte("\x1b[1;3H"))
	// Insert 2 chars.
	vt.Write([]byte("\x1b[2@"))

	// Expected: A B _ _ C D E (where _ is blank), truncated to width 8.
	// "AB  CDE" — 7 chars, row has trailing blank.
	got := rowText(vt, 0)
	want := "AB  CDE"
	if got != want {
		t.Errorf("row 0 after insertChars = %q, want %q", got, want)
	}
}

// TestLineEditDeleteChars verifies deleteChars shifts content left and fills
// the vacated end of the line with blank cells.
func TestLineEditDeleteChars(t *testing.T) {
	t.Parallel()

	vt := New(8, 3)
	vt.Write([]byte("ABCDE"))
	// Cursor to col 2 (1-indexed) on row 1 → (row=0, col=1).
	vt.Write([]byte("\x1b[1;2H"))
	// Delete 2 chars.
	vt.Write([]byte("\x1b[2P"))

	// ABCDE → delete B,C → A D E  (D,E shift left).
	got := rowText(vt, 0)
	want := "ADE"
	if got != want {
		t.Errorf("row 0 after deleteChars = %q, want %q", got, want)
	}
}

// TestLineEditEraseChars verifies eraseChars blanks cells in place without
// shifting the rest of the line.
func TestLineEditEraseChars(t *testing.T) {
	t.Parallel()

	vt := New(8, 3)
	vt.Write([]byte("ABCDE"))
	// Cursor to col 2 (1-indexed) on row 1 → (row=0, col=1).
	vt.Write([]byte("\x1b[1;2H"))
	// Erase 2 chars (no shift).
	vt.Write([]byte("\x1b[2X"))

	// A stays, B and C become blank, D and E stay.
	// Screen row: A _ _ D E → "A  DE"
	got := rowText(vt, 0)
	want := "A  DE"
	if got != want {
		t.Errorf("row 0 after eraseChars = %q, want %q", got, want)
	}
}

// TestLineEditWideCharNormalizationAfterDelete verifies that after deleteChars
// removes the base cell of a wide character, no orphan Width==0 continuation
// cell remains: every Width==0 cell must be immediately preceded by a Width==2
// cell.
func TestLineEditWideCharNormalizationAfterDelete(t *testing.T) {
	t.Parallel()

	// Write a wide char at the start of the line followed by ASCII.
	vt := New(10, 3)
	// "世" is a 2-column wide character.
	vt.Write([]byte("世AB"))
	// cursor is now at col 4 (世=2, A=1, B=1).
	// Move cursor to col 1 (the wide char base is at col 0).
	vt.Write([]byte("\x1b[1;1H"))
	// Delete 1 char — removes the wide char base cell.
	vt.Write([]byte("\x1b[1P"))

	// Walk the row and verify no orphan continuation cell exists.
	row := vt.VisibleScreen()[0]
	for i, c := range row {
		if c.Width == 0 {
			if i == 0 || row[i-1].Width != 2 {
				t.Errorf("orphan continuation cell at col %d (previous width=%d)",
					i, func() int {
						if i == 0 {
							return -1
						}
						return row[i-1].Width
					}())
			}
		}
	}
}
