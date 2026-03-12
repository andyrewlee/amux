package vterm

import "testing"

func TestResetParserStateClearsCarriedCSI(t *testing.T) {
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
