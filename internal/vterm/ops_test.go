package vterm

import "testing"

// fillRow overwrites every cell of row y with the marker rune so that a later
// erase is observable: erased cells revert to a blank ' ' (DefaultCell), while
// untouched cells keep the marker.
func fillRow(v *VTerm, y int, marker rune) {
	for x := 0; x < v.Width && x < len(v.Screen[y]); x++ {
		v.Screen[y][x] = Cell{Rune: marker, Width: 1}
	}
}

// rowRunes returns the raw runes of row y without trimming, so callers can
// assert on exact per-column content (including interior blanks).
func rowRunes(v *VTerm, y int) []rune {
	out := make([]rune, 0, len(v.Screen[y]))
	for _, c := range v.Screen[y] {
		out = append(out, c.Rune)
	}
	return out
}

// TestTab moves the cursor to the next 8-column tab stop, clamping at the right
// edge, and bumps the version only when the cursor actually moves.
func TestTab(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		width    int
		startX   int
		startY   int
		wantX    int
		wantBump bool
	}{
		{name: "from home advances to col 8", width: 20, startX: 0, wantX: 8, wantBump: true},
		{name: "mid first stop rounds up to 8", width: 20, startX: 3, wantX: 8, wantBump: true},
		{name: "one before stop rounds up to 8", width: 20, startX: 7, wantX: 8, wantBump: true},
		{name: "exactly on a stop jumps to next", width: 20, startX: 8, wantX: 16, wantBump: true},
		{name: "second interior stop", width: 30, startX: 9, wantX: 16, wantBump: true},
		{
			name:     "stop within width is not clamped",
			width:    10,
			startX:   5,
			wantX:    8, // ((5/8)+1)*8 = 8, and 8 < width(10)
			wantBump: true,
		},
		{
			name:     "next stop past width clamps to width-1",
			width:    6,
			startX:   2,
			wantX:    5, // stop would be 8 >= width(6) -> width-1
			wantBump: true,
		},
		{
			name:     "already at last column when no further stop",
			width:    6,
			startX:   5,
			wantX:    5, // stop 8 clamps to 5, equal to start -> no move
			wantBump: false,
		},
		{
			name:     "width exactly a multiple keeps last column reachable",
			width:    8,
			startX:   3,
			wantX:    7, // stop 8 >= width(8) -> width-1 = 7
			wantBump: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vt := New(tc.width, 5)
			vt.CursorX = tc.startX
			vt.CursorY = tc.startY
			startVersion := vt.Version()

			vt.tab()

			if vt.CursorX != tc.wantX {
				t.Fatalf("tab from x=%d (width %d): got x=%d, want x=%d",
					tc.startX, tc.width, vt.CursorX, tc.wantX)
			}
			if vt.CursorY != tc.startY {
				t.Fatalf("tab moved cursor row from %d to %d; tab must not change Y",
					tc.startY, vt.CursorY)
			}
			if got := vt.Version() != startVersion; got != tc.wantBump {
				t.Fatalf("tab version bump = %v (from %d to %d), want bump = %v",
					got, startVersion, vt.Version(), tc.wantBump)
			}
			// Resulting column must always be a valid index inside the row.
			if vt.CursorX < 0 || vt.CursorX >= vt.Width {
				t.Fatalf("tab produced out-of-range x=%d for width=%d", vt.CursorX, vt.Width)
			}
		})
	}
}

// TestTabAdvancesAcrossSuccessiveStops confirms repeated tabs land on the 8,
// 16, 24 stops in sequence and then pin to the right edge.
func TestTabAdvancesAcrossSuccessiveStops(t *testing.T) {
	t.Parallel()
	vt := New(28, 3)
	want := []int{8, 16, 24, 27, 27}
	for i, w := range want {
		vt.tab()
		if vt.CursorX != w {
			t.Fatalf("tab #%d: got x=%d, want x=%d", i+1, vt.CursorX, w)
		}
	}
}

// TestBackspace moves the cursor one column left, never below zero, and bumps
// the version only when it actually moves.
func TestBackspace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		startX   int
		startY   int
		wantX    int
		wantBump bool
	}{
		{name: "interior moves left", startX: 5, startY: 2, wantX: 4, wantBump: true},
		{name: "from col 1 to col 0", startX: 1, startY: 0, wantX: 0, wantBump: true},
		{name: "at left edge stays put", startX: 0, startY: 3, wantX: 0, wantBump: false},
		{name: "right edge moves left", startX: 9, startY: 1, wantX: 8, wantBump: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vt := New(10, 5)
			vt.CursorX = tc.startX
			vt.CursorY = tc.startY
			startVersion := vt.Version()

			vt.backspace()

			if vt.CursorX != tc.wantX {
				t.Fatalf("backspace from x=%d: got x=%d, want x=%d", tc.startX, vt.CursorX, tc.wantX)
			}
			if vt.CursorY != tc.startY {
				t.Fatalf("backspace changed row from %d to %d; it must only move X",
					tc.startY, vt.CursorY)
			}
			if got := vt.Version() != startVersion; got != tc.wantBump {
				t.Fatalf("backspace version bump = %v (from %d to %d), want bump = %v",
					got, startVersion, vt.Version(), tc.wantBump)
			}
		})
	}
}

// TestBackspaceLeavesCellsIntact verifies backspace is purely a cursor move: it
// does not erase the cell it lands on or the one it left.
func TestBackspaceLeavesCellsIntact(t *testing.T) {
	t.Parallel()
	vt := New(6, 2)
	fillRow(vt, 0, 'X')
	vt.CursorX, vt.CursorY = 3, 0

	vt.backspace()

	if vt.CursorX != 2 {
		t.Fatalf("backspace: got x=%d, want 2", vt.CursorX)
	}
	if got := lineText(vt.Screen[0]); got != "XXXXXX" {
		t.Fatalf("backspace mutated row content: got %q, want %q", got, "XXXXXX")
	}
}

// TestEraseLine exercises all three erase modes plus the out-of-bounds guard,
// asserting exactly which columns revert to blank and which keep their marker.
func TestEraseLine(t *testing.T) {
	t.Parallel()

	const width = 8
	tests := []struct {
		name    string
		mode    int
		cursorX int
		want    string // per-column expected runes, '.' = erased blank, 'X' = kept
	}{
		{name: "mode 0 cursor to end at col 3", mode: 0, cursorX: 3, want: "XXX....."},
		{name: "mode 0 from home erases all", mode: 0, cursorX: 0, want: "........"},
		{name: "mode 0 at last column erases one", mode: 0, cursorX: 7, want: "XXXXXXX."},
		{name: "mode 1 start to cursor at col 3", mode: 1, cursorX: 3, want: "....XXXX"},
		{name: "mode 1 from home erases first only", mode: 1, cursorX: 0, want: ".XXXXXXX"},
		{name: "mode 1 at last column erases all", mode: 1, cursorX: 7, want: "........"},
		{name: "mode 2 erases entire line", mode: 2, cursorX: 4, want: "........"},
		{name: "unknown mode is a no-op", mode: 9, cursorX: 4, want: "XXXXXXXX"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vt := New(width, 4)
			const row = 1
			fillRow(vt, row, 'X')
			vt.CursorX, vt.CursorY = tc.cursorX, row
			startVersion := vt.Version()

			vt.eraseLine(tc.mode)

			got := rowRunes(vt, row)
			for x, ch := range tc.want {
				wantRune := 'X'
				if ch == '.' {
					wantRune = ' '
				}
				if got[x] != wantRune {
					t.Fatalf("eraseLine(mode=%d, x=%d) col %d = %q, want %q (full row %q)",
						tc.mode, tc.cursorX, x, string(got[x]), string(wantRune), string(got))
				}
			}
			// Erasing marks the line dirty (and bumps version) whenever the
			// cursor row is in range, including the no-op mode which still
			// calls markDirtyLine at the end of eraseLine.
			if vt.Version() == startVersion {
				t.Fatalf("eraseLine did not bump version for in-range row")
			}
			// Other rows must be untouched.
			if got := lineText(vt.Screen[0]); got != "" {
				t.Fatalf("eraseLine touched row 0: got %q, want empty", got)
			}
		})
	}
}

// TestEraseLineErasedCellsAreDefault confirms erased cells are exactly
// DefaultCell (blank space, width 1, zero style), not merely zeroed runes.
func TestEraseLineErasedCellsAreDefault(t *testing.T) {
	t.Parallel()
	vt := New(5, 2)
	for x := 0; x < vt.Width; x++ {
		vt.Screen[0][x] = Cell{Rune: 'Z', Width: 1, Style: sampleStyle()}
	}
	vt.CursorX, vt.CursorY = 0, 0

	vt.eraseLine(0) // cursor to end -> whole row

	for x := 0; x < vt.Width; x++ {
		if vt.Screen[0][x] != DefaultCell() {
			t.Fatalf("erased cell %d = %+v, want DefaultCell %+v",
				x, vt.Screen[0][x], DefaultCell())
		}
	}
}

// TestEraseLineCursorOutOfRange verifies the early return when CursorY is past
// the end of the screen: nothing is touched and no version bump occurs.
func TestEraseLineCursorOutOfRange(t *testing.T) {
	t.Parallel()
	vt := New(6, 3)
	fillRow(vt, 0, 'X')
	fillRow(vt, 1, 'X')
	fillRow(vt, 2, 'X')
	vt.CursorY = vt.Height // one past the last valid row
	vt.CursorX = 0
	startVersion := vt.Version()

	vt.eraseLine(2)

	for y := 0; y < vt.Height; y++ {
		if got := lineText(vt.Screen[y]); got != "XXXXXX" {
			t.Fatalf("out-of-range eraseLine touched row %d: got %q", y, got)
		}
	}
	if vt.Version() != startVersion {
		t.Fatalf("out-of-range eraseLine bumped version from %d to %d",
			startVersion, vt.Version())
	}
}

// TestEraseLineMode2ReplacesWholeLine confirms mode 2 swaps in a fresh blank
// line regardless of cursor column.
func TestEraseLineMode2ReplacesWholeLine(t *testing.T) {
	t.Parallel()
	vt := New(7, 2)
	fillRow(vt, 0, 'Q')
	vt.CursorX, vt.CursorY = 5, 0

	vt.eraseLine(2)

	if got := lineText(vt.Screen[0]); got != "" {
		t.Fatalf("mode 2 left content %q, want empty line", got)
	}
	if len(vt.Screen[0]) != vt.Width {
		t.Fatalf("mode 2 produced line of len %d, want width %d", len(vt.Screen[0]), vt.Width)
	}
}

// TestNewlineBelowScrollRegionDoesNotScroll verifies a line feed while the
// cursor sits below a partial scroll region moves the cursor down without
// scrolling the region (DEC/xterm semantics).
func TestNewlineBelowScrollRegionDoesNotScroll(t *testing.T) {
	t.Parallel()

	vt := New(8, 6)
	vt.Write([]byte("TOP"))
	// Region rows 1-3 (DECSTBM is 1-indexed inclusive): ScrollTop=1, ScrollBottom=4.
	vt.Write([]byte("\x1b[2;4r"))
	vt.Write([]byte("\x1b[2;1HR1\x1b[3;1HR2\x1b[4;1HR3"))
	// Below the region: row index 4 >= ScrollBottom.
	vt.Write([]byte("\x1b[5;1H"))
	vt.Write([]byte("S1\r\n"))
	vt.Write([]byte("S2"))

	for y, want := range map[int]string{1: "R1", 2: "R2", 3: "R3"} {
		if got := rowText(vt, y); got != want {
			t.Errorf("region row %d = %q, want %q (region scrolled)", y, got, want)
		}
	}
	if vt.CursorY != 5 {
		t.Errorf("CursorY after LF below region = %d, want 5", vt.CursorY)
	}
	if got := rowText(vt, 5); got != "S2" {
		t.Errorf("row 5 = %q, want %q", got, "S2")
	}
}

// TestNewlineBelowScrollRegionClampsAtLastRow verifies a line feed with the
// cursor already on the last screen row (below the region) stays put and does
// not scroll the region.
func TestNewlineBelowScrollRegionClampsAtLastRow(t *testing.T) {
	t.Parallel()

	vt := New(8, 6)
	vt.Write([]byte("\x1b[2;4r"))
	vt.Write([]byte("\x1b[2;1HR1\x1b[3;1HR2\x1b[4;1HR3"))
	vt.Write([]byte("\x1b[6;1H"))
	vt.Write([]byte("\n"))

	if vt.CursorY != 5 {
		t.Errorf("CursorY after LF on last row = %d, want 5 (clamped)", vt.CursorY)
	}
	for y, want := range map[int]string{1: "R1", 2: "R2", 3: "R3"} {
		if got := rowText(vt, y); got != want {
			t.Errorf("region row %d = %q, want %q (region scrolled)", y, got, want)
		}
	}
}

// TestAutoWrapBelowScrollRegionDoesNotScroll verifies printing past the right
// margin below a partial scroll region wraps to the next row without
// scrolling the region.
func TestAutoWrapBelowScrollRegionDoesNotScroll(t *testing.T) {
	t.Parallel()

	vt := New(8, 6)
	vt.Write([]byte("\x1b[2;4r"))
	vt.Write([]byte("\x1b[2;1HR1\x1b[3;1HR2\x1b[4;1HR3"))
	vt.Write([]byte("\x1b[5;1H"))
	// 9 chars on an 8-wide row: auto-wrap fires after the 8th.
	vt.Write([]byte("ABCDEFGHI"))

	for y, want := range map[int]string{1: "R1", 2: "R2", 3: "R3"} {
		if got := rowText(vt, y); got != want {
			t.Errorf("region row %d = %q, want %q (region scrolled)", y, got, want)
		}
	}
	if got := rowText(vt, 4); got != "ABCDEFGH" {
		t.Errorf("row 4 = %q, want %q", got, "ABCDEFGH")
	}
	if got := rowText(vt, 5); got != "I" {
		t.Errorf("row 5 = %q, want %q (wrapped char)", got, "I")
	}
	if vt.CursorY != 5 {
		t.Errorf("CursorY after wrap below region = %d, want 5", vt.CursorY)
	}
}
