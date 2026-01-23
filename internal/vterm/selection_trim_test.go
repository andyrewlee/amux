package vterm

import "testing"

func TestTrimScrollbackShiftsSelection(t *testing.T) {
	vt := New(2, 1)
	vt.Scrollback = make([][]Cell, MaxScrollback+2)
	vt.Screen[0] = MakeBlankLine(2)

	vt.SetSelection(1, MaxScrollback, 1, MaxScrollback+1, true, false)
	vt.trimScrollback()

	if !vt.SelActive() {
		t.Fatalf("expected selection to remain active after trim")
	}
	if got := vt.SelStartLine(); got != MaxScrollback-2 {
		t.Fatalf("expected start line to shift to %d, got %d", MaxScrollback-2, got)
	}
	if got := vt.SelEndLine(); got != MaxScrollback-1 {
		t.Fatalf("expected end line to shift to %d, got %d", MaxScrollback-1, got)
	}
}

func TestTrimScrollbackClearsFullyTrimmedSelection(t *testing.T) {
	vt := New(2, 1)
	vt.Scrollback = make([][]Cell, MaxScrollback+2)
	vt.Screen[0] = MakeBlankLine(2)

	vt.SetSelection(1, 0, 1, 1, true, false)
	vt.trimScrollback()

	if vt.SelActive() {
		t.Fatalf("expected selection to be cleared when fully trimmed")
	}
}
