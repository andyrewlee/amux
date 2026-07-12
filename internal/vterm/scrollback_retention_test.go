package vterm

import (
	"fmt"
	"testing"
)

// TestScrollbackTrimBoundsRetention pins the memory characteristics of the
// trim strategy: trimScrollback re-slices (advancing the slice header), so the
// dead prefix stays reachable until append outgrows the backing array and
// reallocates. With Go's ~1.25x large-slice growth the steady-state backing
// array stays under 1.5x MaxScrollback rows — bounded and self-correcting
// (measured 2026-07: oscillates 1.00x-1.30x; ~13 MB transient peak per
// continuously-streaming 80-col terminal, not worth a compaction pass). If
// this test fails, the trim/append interplay changed and retention should be
// re-measured.
func TestScrollbackTrimBoundsRetention(t *testing.T) {
	t.Parallel()

	vt := New(80, 24)
	total := 3 * MaxScrollback
	for i := 0; i < total; i++ {
		vt.Write([]byte(fmt.Sprintf("line-%d\r\n", i)))
	}

	if len(vt.Scrollback) != MaxScrollback {
		t.Fatalf("scrollback len = %d, want MaxScrollback (%d)", len(vt.Scrollback), MaxScrollback)
	}
	if c := cap(vt.Scrollback); c > MaxScrollback+MaxScrollback/2 {
		t.Fatalf("scrollback backing array cap = %d, want <= 1.5x MaxScrollback (%d)", c, MaxScrollback+MaxScrollback/2)
	}

	// The retained lines must be exactly the most recent MaxScrollback lines.
	// The screen holds the newest rows; scrollback ends just before them.
	wantOldest := fmt.Sprintf("line-%d", total-MaxScrollback-(vt.Height-1))
	if got := lineText(vt.Scrollback[0]); got != wantOldest {
		t.Fatalf("oldest retained scrollback line = %q, want %q", got, wantOldest)
	}
	wantNewest := fmt.Sprintf("line-%d", total-1-(vt.Height-1))
	if got := lineText(vt.Scrollback[len(vt.Scrollback)-1]); got != wantNewest {
		t.Fatalf("newest scrollback line = %q, want %q", got, wantNewest)
	}
}
