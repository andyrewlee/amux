package vterm

import "testing"

// Erasing part of a line can cut a wide glyph in half. Whichever half survives
// is invalid: a lone continuation cell (Width 0) renders as nothing and pulls
// the rest of the line one column left, and a lone wide cell (Width 2) claims a
// column it no longer owns. eraseLine/eraseDisplay must normalize the line.
func TestEraseSplitsWideGlyphIntoBlanks(t *testing.T) {
	tests := []struct {
		name string
		seq  string
	}{
		// Cursor on the wide base; erase start-to-cursor leaves an orphan
		// continuation at column 1.
		{name: "EL1 leaves orphan continuation", seq: "\x1b[1;1H\x1b[1K"},
		{name: "ED1 leaves orphan continuation", seq: "\x1b[1;1H\x1b[1J"},
		// Cursor on the continuation column; erase cursor-to-end leaves a wide
		// base at column 0 with no continuation.
		{name: "EL0 leaves half a wide base", seq: "\x1b[1;2H\x1b[0K"},
		{name: "ED0 leaves half a wide base", seq: "\x1b[1;2H\x1b[0J"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			term := New(4, 1)
			term.Write([]byte("中ab"))
			if term.Screen[0][0].Width != 2 || term.Screen[0][1].Width != 0 {
				t.Fatalf("setup: expected wide pair, got %+v %+v", term.Screen[0][0], term.Screen[0][1])
			}

			term.Write([]byte(tc.seq))

			for x := 0; x < 2; x++ {
				if got := term.Screen[0][x].Width; got != 1 {
					t.Fatalf("column %d width = %d, want 1 (line: %+v)", x, got, term.Screen[0])
				}
			}
		})
	}
}

// The normalization must not disturb an intact wide glyph elsewhere on the line.
func TestEraseKeepsUntouchedWideGlyph(t *testing.T) {
	term := New(6, 1)
	term.Write([]byte("ab中cd"))
	// Erase the two leading narrow cells only.
	term.Write([]byte("\x1b[1;2H\x1b[1K"))

	if term.Screen[0][2].Rune != '中' || term.Screen[0][2].Width != 2 {
		t.Fatalf("wide glyph was dropped: %+v", term.Screen[0][2])
	}
	if term.Screen[0][3].Width != 0 {
		t.Fatalf("continuation cell was dropped: %+v", term.Screen[0][3])
	}
}
