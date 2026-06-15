package vterm

import (
	"strings"
	"testing"
)

// writeRows writes one string per row into a fresh VTerm of the given size and
// returns it. Each input string is written followed by CRLF so the rows land on
// successive screen lines. Strings longer than width wrap per the terminal's
// own logic, so callers should keep them within width for predictable rows.
func writeRows(width, height int, rows ...string) *VTerm {
	vt := New(width, height)
	for i, r := range rows {
		vt.Write([]byte(r))
		if i < len(rows)-1 {
			vt.Write([]byte("\r\n"))
		}
	}
	return vt
}

// rowTexts maps lineText over a screen buffer so whole-buffer content can be
// asserted in one comparison.
func rowTexts(lines [][]Cell) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = lineText(l)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestVisibleScreenLiveView checks the non-scrolled path: the returned buffer
// has exactly Height rows, each exactly Width cells, and mirrors the live screen
// content.
func TestVisibleScreenLiveView(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		width  int
		height int
		rows   []string
		want   []string
	}{
		{
			name:   "single line",
			width:  5,
			height: 1,
			rows:   []string{"hi"},
			want:   []string{"hi"},
		},
		{
			name:   "multiple lines padded to height",
			width:  6,
			height: 3,
			rows:   []string{"abc", "de"},
			want:   []string{"abc", "de", ""},
		},
		{
			name:   "full-width row",
			width:  4,
			height: 2,
			rows:   []string{"wxyz"},
			want:   []string{"wxyz", ""},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vt := writeRows(tc.width, tc.height, tc.rows...)
			got := vt.VisibleScreen()

			if len(got) != tc.height {
				t.Fatalf("VisibleScreen() returned %d rows, want %d", len(got), tc.height)
			}
			for y, row := range got {
				if len(row) != tc.width {
					t.Fatalf("row %d has %d cells, want width %d", y, len(row), tc.width)
				}
			}
			if texts := rowTexts(got); !equalStrings(texts, tc.want) {
				t.Fatalf("VisibleScreen() content = %q, want %q", texts, tc.want)
			}
		})
	}
}

// TestVisibleScreenReturnsCopy verifies the live-view path returns independent
// lines: mutating the result must not corrupt the underlying screen buffer.
func TestVisibleScreenReturnsCopy(t *testing.T) {
	t.Parallel()
	vt := writeRows(4, 2, "ab", "cd")
	got := vt.VisibleScreen()

	got[0][0] = Cell{Rune: 'Z', Width: 1}
	if vt.Screen[0][0].Rune != 'a' {
		t.Fatalf("mutating VisibleScreen() result altered underlying screen: got %q", vt.Screen[0][0].Rune)
	}
}

// TestVisibleScreenScrolled exercises the ViewOffset > 0 branch that stitches
// scrollback and screen together. With 3 scrollback rows, a 2-row screen, and a
// 2-row viewport scrolled up by 2, the visible window starts inside scrollback.
func TestVisibleScreenScrolled(t *testing.T) {
	t.Parallel()
	vt := New(6, 2)
	vt.Scrollback = [][]Cell{
		lineFromString(6, "old0"),
		lineFromString(6, "old1"),
		lineFromString(6, "old2"),
	}
	vt.Screen = [][]Cell{
		lineFromString(6, "live0"),
		lineFromString(6, "live1"),
	}

	// total = 3 scrollback + 2 screen = 5. height=2. ViewOffset=2 ->
	// startLine = 3 + 2 - 2 - 2 = 1, so rows old1, old2 are visible.
	vt.ViewOffset = 2
	got := vt.VisibleScreen()
	if want := []string{"old1", "old2"}; !equalStrings(rowTexts(got), want) {
		t.Fatalf("scrolled VisibleScreen() = %q, want %q", rowTexts(got), want)
	}

	// ViewOffset=1 -> startLine = 3+2-2-1 = 2, rows old2, live0.
	vt.ViewOffset = 1
	got = vt.VisibleScreen()
	if want := []string{"old2", "live0"}; !equalStrings(rowTexts(got), want) {
		t.Fatalf("scrolled VisibleScreen() = %q, want %q", rowTexts(got), want)
	}
}

// TestVisibleScreenScrolledClampsStartLine verifies the startLine<0 guard: an
// over-large ViewOffset clamps to the top of the combined buffer instead of
// indexing negatively.
func TestVisibleScreenScrolledClampsStartLine(t *testing.T) {
	t.Parallel()
	vt := New(6, 3)
	vt.Scrollback = [][]Cell{lineFromString(6, "h0")}
	vt.Screen = [][]Cell{
		lineFromString(6, "s0"),
		lineFromString(6, "s1"),
		lineFromString(6, "s2"),
	}
	// total = 1 + 3 = 4. height=3. A huge ViewOffset drives startLine negative,
	// so it clamps to 0 and the window shows the first 3 combined rows.
	vt.ViewOffset = 99
	got := vt.VisibleScreen()
	if want := []string{"h0", "s0", "s1"}; !equalStrings(rowTexts(got), want) {
		t.Fatalf("over-scrolled VisibleScreen() = %q, want %q", rowTexts(got), want)
	}
}

// TestVisibleScreenUsesSyncSnapshot confirms that during synchronized output the
// frozen snapshot is rendered rather than the live screen, so writes made after
// sync begins are invisible until sync ends.
func TestVisibleScreenUsesSyncSnapshot(t *testing.T) {
	t.Parallel()
	vt := writeRows(6, 1, "before")
	vt.setSynchronizedOutput(true)
	// Overwrite the live screen; the snapshot still holds "before".
	vt.Screen[0] = lineFromString(6, "after")

	if got := rowTexts(vt.VisibleScreen()); !equalStrings(got, []string{"before"}) {
		t.Fatalf("during sync VisibleScreen() = %q, want frozen %q", got, []string{"before"})
	}

	vt.setSynchronizedOutput(false)
	if got := rowTexts(vt.VisibleScreen()); !equalStrings(got, []string{"after"}) {
		t.Fatalf("after sync ends VisibleScreen() = %q, want live %q", got, []string{"after"})
	}
}

// TestVisibleScreenInto checks the allocation-reusing variant matches
// VisibleScreen content for both the live and scrolled paths and reuses the
// backing slices when dst already has the right shape.
func TestVisibleScreenInto(t *testing.T) {
	t.Parallel()

	t.Run("live view matches VisibleScreen", func(t *testing.T) {
		t.Parallel()
		vt := writeRows(6, 3, "abc", "de")
		want := rowTexts(vt.VisibleScreen())
		got := rowTexts(vt.VisibleScreenInto(nil))
		if !equalStrings(got, want) {
			t.Fatalf("VisibleScreenInto(nil) = %q, want %q", got, want)
		}
	})

	t.Run("scrolled view matches VisibleScreen", func(t *testing.T) {
		t.Parallel()
		vt := New(6, 2)
		vt.Scrollback = [][]Cell{
			lineFromString(6, "old0"),
			lineFromString(6, "old1"),
			lineFromString(6, "old2"),
		}
		vt.Screen = [][]Cell{lineFromString(6, "live0"), lineFromString(6, "live1")}
		vt.ViewOffset = 2
		want := rowTexts(vt.VisibleScreen())
		got := rowTexts(vt.VisibleScreenInto(nil))
		if !equalStrings(got, want) {
			t.Fatalf("scrolled VisibleScreenInto(nil) = %q, want %q", got, want)
		}
	})

	t.Run("reuses dst backing rows", func(t *testing.T) {
		t.Parallel()
		vt := writeRows(6, 2, "ab", "cd")
		dst := make([][]Cell, 2)
		dst[0] = MakeBlankLine(6)
		dst[1] = MakeBlankLine(6)
		row0, row1 := dst[0], dst[1]
		out := vt.VisibleScreenInto(dst)
		if len(out) != 2 {
			t.Fatalf("VisibleScreenInto reused dst returned %d rows, want 2", len(out))
		}
		if &out[0][0] != &row0[0] || &out[1][0] != &row1[0] {
			t.Fatalf("VisibleScreenInto did not reuse provided dst backing arrays")
		}
		if texts := rowTexts(out); !equalStrings(texts, []string{"ab", "cd"}) {
			t.Fatalf("reused VisibleScreenInto content = %q, want %q", texts, []string{"ab", "cd"})
		}
	})

	t.Run("reallocates dst with wrong length", func(t *testing.T) {
		t.Parallel()
		vt := writeRows(6, 3, "x")
		dst := make([][]Cell, 1) // wrong length, must be replaced
		out := vt.VisibleScreenInto(dst)
		if len(out) != 3 {
			t.Fatalf("VisibleScreenInto reallocated to %d rows, want height 3", len(out))
		}
	})

	t.Run("short row is reset-padded to width", func(t *testing.T) {
		t.Parallel()
		vt := New(5, 1)
		// A screen row shorter than width must be padded with default cells.
		vt.Screen = [][]Cell{lineFromString(2, "ab")}
		out := vt.VisibleScreenInto(nil)
		if len(out[0]) != 5 {
			t.Fatalf("padded row length = %d, want 5", len(out[0]))
		}
		if got := lineText(out[0]); got != "ab" {
			t.Fatalf("padded row text = %q, want %q", got, "ab")
		}
		for x := 2; x < 5; x++ {
			if out[0][x] != DefaultCell() {
				t.Fatalf("cell %d not reset to default: %+v", x, out[0][x])
			}
		}
	})
}

// TestVisibleScreenIntoZeroDims confirms the guard for non-positive dimensions
// returns nil instead of panicking.
func TestVisibleScreenIntoZeroDims(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		width, height int
	}{
		{"zero width", 0, 3},
		{"zero height", 4, 0},
		{"both zero", 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// New clamps to >=1; force the degenerate dims directly.
			vt := New(4, 3)
			vt.Width = tc.width
			vt.Height = tc.height
			if got := vt.VisibleScreenInto(nil); got != nil {
				t.Fatalf("VisibleScreenInto with dims (%d,%d) = %v, want nil", tc.width, tc.height, got)
			}
		})
	}
}

// TestVisibleScreenWithSelectionNoSelection verifies that with no active
// selection the function returns the plain visible screen unchanged.
func TestVisibleScreenWithSelectionNoSelection(t *testing.T) {
	t.Parallel()
	vt := writeRows(6, 2, "abc", "def")
	plain := rowTexts(vt.VisibleScreen())
	got := rowTexts(vt.VisibleScreenWithSelection())
	if !equalStrings(got, plain) {
		t.Fatalf("without selection VisibleScreenWithSelection() = %q, want %q", got, plain)
	}
}

// TestVisibleScreenWithSelectionToggleReverse checks that cells inside the
// selection have their Reverse style flipped while cells outside are untouched.
func TestVisibleScreenWithSelectionToggleReverse(t *testing.T) {
	t.Parallel()
	vt := writeRows(6, 1, "abcdef")
	// Select absolute line 0, columns 1..3 inclusive (no scrollback so absLine
	// equals screenY).
	vt.SetSelection(1, 0, 3, 0, true, false)

	lines := vt.VisibleScreenWithSelection()
	if len(lines) != 1 {
		t.Fatalf("expected 1 row, got %d", len(lines))
	}
	row := lines[0]
	for x := 0; x < len(row); x++ {
		inSel := x >= 1 && x <= 3
		if row[x].Style.Reverse != inSel {
			t.Errorf("cell %d Reverse = %v, want %v", x, row[x].Style.Reverse, inSel)
		}
	}
	// Content is unchanged by selection highlighting.
	if got := lineText(row); got != "abcdef" {
		t.Fatalf("selection altered text: got %q", got)
	}
}

// TestVisibleScreenWithSelectionRectangular exercises the rectangular selection
// branch, where a column band is highlighted across multiple rows.
func TestVisibleScreenWithSelectionRectangular(t *testing.T) {
	t.Parallel()
	vt := writeRows(6, 2, "abcdef", "ghijkl")
	// Rectangular columns 2..4 across rows 0..1.
	vt.SetSelection(2, 0, 4, 1, true, true)

	lines := vt.VisibleScreenWithSelection()
	for y, row := range lines {
		for x := 0; x < len(row); x++ {
			inSel := x >= 2 && x <= 4
			if row[x].Style.Reverse != inSel {
				t.Errorf("row %d cell %d Reverse = %v, want %v", y, x, row[x].Style.Reverse, inSel)
			}
		}
	}
}

// TestVisibleScreenWithSelectionDoesNotMutateScreen ensures the highlight pass
// operates on the VisibleScreen copy and leaves the live screen styles intact.
func TestVisibleScreenWithSelectionDoesNotMutateScreen(t *testing.T) {
	t.Parallel()
	vt := writeRows(4, 1, "abcd")
	vt.SetSelection(0, 0, 3, 0, true, false)
	_ = vt.VisibleScreenWithSelection()
	for x := 0; x < vt.Width; x++ {
		if vt.Screen[0][x].Style.Reverse {
			t.Fatalf("selection highlighting leaked into live screen at cell %d", x)
		}
	}
}

// TestRenderScreenFrom checks the synchronized-frame renderer: it emits the row
// content, places newline separators between rows (driven by len(v.Screen)), and
// ends with a style reset.
func TestRenderScreenFrom(t *testing.T) {
	t.Parallel()

	t.Run("single row", func(t *testing.T) {
		t.Parallel()
		vt := writeRows(5, 1, "hi")
		out := vt.renderScreenFrom(vt.Screen)
		if !strings.HasSuffix(out, "\x1b[0m") {
			t.Fatalf("renderScreenFrom output must end with reset, got %q", out)
		}
		if got := stripANSI(out); strings.TrimRight(got, " ") != "hi" {
			t.Fatalf("renderScreenFrom content = %q, want %q", got, "hi")
		}
		if strings.Contains(out, "\n") {
			t.Fatalf("single-row render should contain no newline, got %q", out)
		}
	})

	t.Run("multi row newline separators", func(t *testing.T) {
		t.Parallel()
		vt := writeRows(5, 3, "aa", "bb", "cc")
		out := vt.renderScreenFrom(vt.Screen)
		// Three rows -> two internal newlines (last row gets no trailing \n).
		if n := strings.Count(out, "\n"); n != 2 {
			t.Fatalf("renderScreenFrom newline count = %d, want 2", n)
		}
		plain := stripANSI(out)
		gotRows := make([]string, 0, 3)
		for _, line := range strings.Split(plain, "\n") {
			gotRows = append(gotRows, strings.TrimRight(line, " "))
		}
		if want := []string{"aa", "bb", "cc"}; !equalStrings(gotRows, want) {
			t.Fatalf("renderScreenFrom rows = %q, want %q", gotRows, want)
		}
	})

	t.Run("empty screen", func(t *testing.T) {
		t.Parallel()
		vt := New(4, 2)
		out := vt.renderScreenFrom([][]Cell{})
		// No rows to emit; only the trailing reset.
		if out != "\x1b[0m" {
			t.Fatalf("renderScreenFrom(empty) = %q, want bare reset", out)
		}
	})

	t.Run("renders selection reverse via SGR", func(t *testing.T) {
		t.Parallel()
		vt := writeRows(6, 1, "abcdef")
		vt.SetSelection(0, 0, 5, 0, true, false)
		out := vt.renderScreenFrom(vt.Screen)
		if !ContainsSGRParam(out, 7) {
			t.Fatalf("expected reverse-video SGR (7) for selected row, got %q", out)
		}
	})
}

// lineFromString builds a width-sized cell row whose leading cells hold the
// runes of s (trailing cells stay blank). It mirrors how the renderer treats
// a short row that is padded to width.
func lineFromString(width int, s string) []Cell {
	line := MakeBlankLine(width)
	for i, r := range s {
		if i >= width {
			break
		}
		line[i] = Cell{Rune: r, Width: 1}
	}
	return line
}
