package vterm

import "testing"

func TestResetParserStateClearsCarriedCSI(t *testing.T) {
	t.Parallel()
	vt := New(10, 2)

	vt.Write([]byte("\x1b["))
	vt.ResetParserState()
	vt.Write([]byte("Hok"))

	if got := vt.Screen[0][0].Rune; got != 'H' {
		t.Fatalf("screen[0][0] = %q, want %q", got, 'H')
	}
	if got := vt.Screen[0][1].Rune; got != 'o' {
		t.Fatalf("screen[0][1] = %q, want %q", got, 'o')
	}
	if got := vt.Screen[0][2].Rune; got != 'k' {
		t.Fatalf("screen[0][2] = %q, want %q", got, 'k')
	}
}

func TestBellSetsPendingFlag(t *testing.T) {
	t.Parallel()
	v := New(80, 24)

	if v.TakePendingBell() {
		t.Fatal("pending bell set before any write")
	}
	v.Write([]byte("hi\x07"))
	if !v.TakePendingBell() {
		t.Fatal("BEL did not set the pending flag")
	}
	if v.TakePendingBell() {
		t.Fatal("second TakePendingBell should be false (read-and-clear)")
	}
}

func TestBellCoalescesWithinChunk(t *testing.T) {
	t.Parallel()
	v := New(80, 24)
	v.Write([]byte("\x07\x07\x07"))
	if !v.TakePendingBell() {
		t.Fatal("BEL burst did not set the pending flag")
	}
	if v.TakePendingBell() {
		t.Fatal("coalesced bells must report a single edge")
	}
}

func TestBellInsideAnsiStreamFlags(t *testing.T) {
	t.Parallel()
	v := New(80, 24)
	// BEL between escape sequences must still flag — the byte dispatch sees it
	// regardless of surrounding CSI/OSC noise.
	v.Write([]byte("\x1b[31mred\x1b[0m\x07\x1b]0;title\x07"))
	if !v.TakePendingBell() {
		t.Fatal("BEL inside an ANSI stream did not set the pending flag")
	}
}
