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

// TestScrollUpTopAnchoredRegionFeedsScrollback verifies the default
// full-screen region (ScrollTop == 0) still feeds scrollback exactly as
// before: every line scrolled off the top of the physical screen is
// preserved, in order.
func TestScrollUpTopAnchoredRegionFeedsScrollback(t *testing.T) {
	t.Parallel()

	vt := New(8, 3)
	// 5 lines through a 3-row screen with no trailing newline: 2 lines
	// (L1, L2) scroll off into Scrollback, leaving L3-L5 on screen.
	vt.Write([]byte("L1\r\nL2\r\nL3\r\nL4\r\nL5"))

	if len(vt.Scrollback) != 2 {
		t.Fatalf("scrollback len = %d, want 2", len(vt.Scrollback))
	}
	wantScrollback := []string{"L1", "L2"}
	for i, want := range wantScrollback {
		if got := lineText(vt.Scrollback[i]); got != want {
			t.Errorf("Scrollback[%d] = %q, want %q", i, got, want)
		}
	}
	wantScreen := []string{"L3", "L4", "L5"}
	for y, want := range wantScreen {
		if got := lineText(vt.Screen[y]); got != want {
			t.Errorf("Screen[%d] = %q, want %q", y, got, want)
		}
	}
}

// TestScrollUpNonTopAnchoredRegionSkipsScrollback verifies that scrolling a
// scroll region whose top margin is below row 0 (e.g. a TUI pinning a header
// via CSI 2;4r) does NOT feed scrollback: those lines never reached the top
// of the physical screen, so xterm/DEC semantics discard them. The in-region
// shift and bottom blank-fill must still happen exactly as for a top-anchored
// region.
func TestScrollUpNonTopAnchoredRegionSkipsScrollback(t *testing.T) {
	t.Parallel()

	vt := New(8, 6)
	// Region rows 2-4 (1-indexed): ScrollTop=1, ScrollBottom=4.
	vt.Write([]byte("\x1b[2;4r"))
	vt.Write([]byte("\x1b[2;1HR1\x1b[3;1HR2\x1b[4;1HR3"))

	if len(vt.Scrollback) != 0 {
		t.Fatalf("scrollback len before scroll = %d, want 0", len(vt.Scrollback))
	}

	// Cursor sits on the region's last row (index 3); CR+LF scrolls the
	// region by one line.
	vt.Write([]byte("\r\n"))

	if len(vt.Scrollback) != 0 {
		t.Fatalf("scrollback len after non-top-anchored scroll = %d, want 0 (R1 must be discarded, not saved)", len(vt.Scrollback))
	}

	wantRows := map[int]string{
		0: "",   // above the region: untouched
		1: "R2", // shifted up within the region
		2: "R3", // shifted up within the region
		3: "",   // bottom of region: blank-filled
		4: "",   // below the region: untouched
		5: "",   // below the region: untouched
	}
	for y, want := range wantRows {
		if got := rowText(vt, y); got != want {
			t.Errorf("row %d = %q, want %q", y, got, want)
		}
	}
}
