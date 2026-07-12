package vterm

import "testing"

// TestScrollUpDoesNotAliasScreenRow verifies scrollUp hands row ownership to
// scrollback cleanly: after a scroll, mutating the scrollback row must not
// affect the live screen (every vacated Screen slot got a fresh row).
func TestScrollUpDoesNotAliasScreenRow(t *testing.T) {
	t.Parallel()

	vt := New(8, 3)
	// Fill all three rows, then one more LF scrolls "AA" into scrollback.
	vt.Write([]byte("AA\r\nBB\r\nCC\r\n"))

	if len(vt.Scrollback) != 1 {
		t.Fatalf("scrollback len = %d, want 1", len(vt.Scrollback))
	}
	sbRow := vt.Scrollback[len(vt.Scrollback)-1]
	if got := lineText(sbRow); got != "AA" {
		t.Fatalf("scrollback row = %q, want %q", got, "AA")
	}

	// Mutate the scrollback row in place.
	for i := range sbRow {
		sbRow[i] = Cell{Rune: 'Z', Width: 1}
	}

	// The live screen must be unaffected: no Screen slot may still alias the
	// row that moved to scrollback.
	for y := 0; y < vt.Height; y++ {
		for x, c := range vt.Screen[y] {
			if c.Rune == 'Z' {
				t.Fatalf("Screen[%d][%d] aliases the scrolled-off row", y, x)
			}
		}
	}
}
