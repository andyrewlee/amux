package vterm

import "testing"

func TestParserStateSnapshot_RestoresContinuationAcrossReset(t *testing.T) {
	vt := New(20, 2)
	vt.Write([]byte("\x1b[>"))

	snapshot := vt.SnapshotParserState()
	vt.ResetParserState()
	vt.RestoreParserState(snapshot)
	vt.Write([]byte("1;10;0cvisible"))

	if got := vt.Screen[0][0].Rune; got != 'v' {
		t.Fatalf("expected continued DA response tail to be swallowed, got %q", got)
	}
	if got := vt.Screen[0][1].Rune; got != 'i' {
		t.Fatalf("expected visible text to start immediately after swallowed continuation, got %q", got)
	}
}
