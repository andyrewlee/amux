package vterm

import "testing"

func TestAltScreenEraseTranscriptShiftPreservesScrolledOffTop(t *testing.T) {
	vt := New(10, 3)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))

	vt.Write([]byte("line1\r\nline2\r\nline3"))
	vt.Write([]byte("\x1b[2J"))

	vt.Write([]byte("\x1b[H"))
	vt.Write([]byte("line2\r\nline3\r\nline4"))
	vt.Write([]byte("\x1b[2J"))

	want := []string{"line1", "line2", "line3", "line4"}
	if len(vt.Scrollback) != len(want) {
		dumpScrollback(t, vt)
		t.Fatalf("scrollback length = %d, want %d", len(vt.Scrollback), len(want))
	}
	for i, w := range want {
		if got := lineText(vt.Scrollback[i]); got != w {
			dumpScrollback(t, vt)
			t.Fatalf("scrollback[%d] = %q, want %q", i, got, w)
		}
	}
}
