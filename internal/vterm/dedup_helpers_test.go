package vterm

import (
	"testing"
)

func TestSelectionContains(t *testing.T) {
	// Bounds are assumed already normalized (start precedes end).
	tests := []struct {
		name                       string
		startX, startY, endX, endY int
		x, y                       int
		want                       bool
	}{
		// Single-row selection: inclusive on both column bounds.
		{"single row inside", 2, 1, 5, 1, 3, 1, true},
		{"single row left edge", 2, 1, 5, 1, 2, 1, true},
		{"single row right edge", 2, 1, 5, 1, 5, 1, true},
		{"single row before start", 2, 1, 5, 1, 1, 1, false},
		{"single row after end", 2, 1, 5, 1, 6, 1, false},
		{"single row wrong row above", 2, 1, 5, 1, 3, 0, false},
		{"single row wrong row below", 2, 1, 5, 1, 3, 2, false},

		// First row of multi-row selection: x must be >= startX.
		{"first row at start", 3, 1, 4, 3, 3, 1, true},
		{"first row right of start", 3, 1, 4, 3, 9, 1, true},
		{"first row left of start", 3, 1, 4, 3, 2, 1, false},

		// Middle rows: fully selected regardless of x.
		{"middle row col 0", 3, 1, 4, 3, 0, 2, true},
		{"middle row high col", 3, 1, 4, 3, 99, 2, true},

		// Last row: x must be <= endX.
		{"last row at end", 3, 1, 4, 3, 4, 3, true},
		{"last row left of end", 3, 1, 4, 3, 0, 3, true},
		{"last row right of end", 3, 1, 4, 3, 5, 3, false},

		// Outside the row range entirely.
		{"above range", 3, 1, 4, 3, 3, 0, false},
		{"below range", 3, 1, 4, 3, 3, 4, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SelectionContains(tc.startX, tc.startY, tc.endX, tc.endY, tc.x, tc.y)
			if got != tc.want {
				t.Errorf("SelectionContains(%d,%d,%d,%d, %d,%d) = %v, want %v",
					tc.startX, tc.startY, tc.endX, tc.endY, tc.x, tc.y, got, tc.want)
			}
		})
	}
}

func TestNormalizeSelectionRange(t *testing.T) {
	tests := []struct {
		name                           string
		startX, startY, endX, endY     int
		wantSX, wantSY, wantEX, wantEY int
	}{
		{"already ordered", 1, 0, 4, 2, 1, 0, 4, 2},
		{"reversed rows", 4, 2, 1, 0, 1, 0, 4, 2},
		{"same row reversed cols", 7, 3, 2, 3, 2, 3, 7, 3},
		{"same row ordered cols", 2, 3, 7, 3, 2, 3, 7, 3},
		{"same point", 5, 5, 5, 5, 5, 5, 5, 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sx, sy, ex, ey := NormalizeSelectionRange(tc.startX, tc.startY, tc.endX, tc.endY)
			if sx != tc.wantSX || sy != tc.wantSY || ex != tc.wantEX || ey != tc.wantEY {
				t.Errorf("NormalizeSelectionRange(%d,%d,%d,%d) = (%d,%d,%d,%d), want (%d,%d,%d,%d)",
					tc.startX, tc.startY, tc.endX, tc.endY,
					sx, sy, ex, ey, tc.wantSX, tc.wantSY, tc.wantEX, tc.wantEY)
			}
		})
	}
}

func TestSuppressBlankUnderline(t *testing.T) {
	underlined := Style{Underline: true}
	plain := Style{}

	tests := []struct {
		name          string
		r             rune
		in            Style
		wantUnderline bool
	}{
		{"underline on glyph kept", 'a', underlined, true},
		{"underline on space dropped", ' ', underlined, false},
		{"underline on NUL dropped", 0, underlined, false},
		{"no underline glyph", 'a', plain, false},
		{"no underline space", ' ', plain, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SuppressBlankUnderline(tc.r, tc.in)
			if got.Underline != tc.wantUnderline {
				t.Errorf("SuppressBlankUnderline(%q, %+v).Underline = %v, want %v",
					tc.r, tc.in, got.Underline, tc.wantUnderline)
			}
		})
	}

	// Only the Underline field may change; everything else must pass through.
	in := Style{Underline: true, Bold: true, Fg: Color{Type: ColorIndexed, Value: 3}}
	got := SuppressBlankUnderline(' ', in)
	want := in
	want.Underline = false
	if got != want {
		t.Errorf("SuppressBlankUnderline mutated more than Underline: got %+v, want %+v", got, want)
	}
}

func TestRenderableRune(t *testing.T) {
	if got := RenderableRune(0); got != ' ' {
		t.Errorf("RenderableRune(0) = %q, want space", got)
	}
	if got := RenderableRune(' '); got != ' ' {
		t.Errorf("RenderableRune(space) = %q, want space", got)
	}
	if got := RenderableRune('x'); got != 'x' {
		t.Errorf("RenderableRune('x') = %q, want 'x'", got)
	}
}

func TestResizeRows(t *testing.T) {
	// Build a buffer where each row is filled with a distinct rune so we can
	// tell preserved content from blank fill.
	mkRow := func(width int, r rune) []Cell {
		row := make([]Cell, width)
		for i := range row {
			row[i] = Cell{Rune: r, Width: 1}
		}
		return row
	}
	isBlank := func(row []Cell) bool {
		blank := DefaultCell()
		for _, c := range row {
			if c != blank {
				return false
			}
		}
		return true
	}

	t.Run("grow height blank-fills new rows", func(t *testing.T) {
		old := [][]Cell{mkRow(4, 'a'), mkRow(4, 'b')}
		got := resizeRows(old, 4, 4)
		if len(got) != 4 {
			t.Fatalf("len = %d, want 4", len(got))
		}
		if got[0][0].Rune != 'a' || got[1][0].Rune != 'b' {
			t.Errorf("existing rows not preserved: %q %q", got[0][0].Rune, got[1][0].Rune)
		}
		if !isBlank(got[2]) || !isBlank(got[3]) {
			t.Errorf("new rows should be blank")
		}
	})

	t.Run("shrink height drops trailing rows", func(t *testing.T) {
		old := [][]Cell{mkRow(4, 'a'), mkRow(4, 'b'), mkRow(4, 'c')}
		got := resizeRows(old, 4, 2)
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
		if got[0][0].Rune != 'a' || got[1][0].Rune != 'b' {
			t.Errorf("rows not preserved on shrink")
		}
	})

	t.Run("row wider than width is reused as-is", func(t *testing.T) {
		wide := mkRow(8, 'a')
		old := [][]Cell{wide}
		got := resizeRows(old, 4, 1)
		if len(got[0]) != 8 {
			t.Errorf("wide row should be reused unchanged, got width %d", len(got[0]))
		}
		// Must be the same backing slice (reuse, not copy).
		if &got[0][0] != &wide[0] {
			t.Errorf("wide row should be reused (same slice), not copied")
		}
	})

	t.Run("row equal to width is reused as-is", func(t *testing.T) {
		exact := mkRow(4, 'a')
		old := [][]Cell{exact}
		got := resizeRows(old, 4, 1)
		if &got[0][0] != &exact[0] {
			t.Errorf("exact-width row should be reused (same slice)")
		}
	})

	t.Run("narrow row is expanded to width", func(t *testing.T) {
		narrow := mkRow(2, 'a')
		old := [][]Cell{narrow}
		got := resizeRows(old, 5, 1)
		if len(got[0]) != 5 {
			t.Fatalf("expanded row width = %d, want 5", len(got[0]))
		}
		if got[0][0].Rune != 'a' || got[0][1].Rune != 'a' {
			t.Errorf("original content not copied into expanded row")
		}
		blank := DefaultCell()
		for i := 2; i < 5; i++ {
			if got[0][i] != blank {
				t.Errorf("expanded tail cell %d not blank: %+v", i, got[0][i])
			}
		}
		// A fresh slice must have been allocated (not the narrow one).
		if &got[0][0] == &narrow[0] {
			t.Errorf("expanded row should be a fresh slice")
		}
	})

	t.Run("empty source row is blank-filled", func(t *testing.T) {
		old := [][]Cell{{}} // present but zero-length row
		got := resizeRows(old, 3, 1)
		if len(got[0]) != 3 || !isBlank(got[0]) {
			t.Errorf("empty source row should be blank-filled to width")
		}
	})
}
