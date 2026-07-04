package vterm

import (
	"strings"
	"testing"
)

func TestParserOversizedOSCSequenceDiscardedUntilTerminator(t *testing.T) {
	t.Parallel()

	vt := New(20, 2)
	vt.Write([]byte("\x1b]0;initial\x07"))
	vt.Write([]byte("\x1b]0;" + strings.Repeat("x", maxOSCSequenceBytes+1) + "hidden\x07OK"))

	if got := vt.Title(); got != "initial" {
		t.Fatalf("Title() after oversized OSC = %q, want %q", got, "initial")
	}
	if got := lineText(vt.Screen[0]); got != "OK" {
		t.Fatalf("oversized OSC payload leaked to screen: row 0 = %q, want %q", got, "OK")
	}
}

func TestParserOversizedOSCMalformedEscapeDoesNotDispatch(t *testing.T) {
	t.Parallel()

	vt := New(20, 2)
	vt.Write([]byte("\x1b]0;initial\x07"))
	vt.Write([]byte("\x1b]0;" + strings.Repeat("x", maxOSCSequenceBytes+1) + "\x1b[0mOK"))

	if got := vt.Title(); got != "initial" {
		t.Fatalf("Title() after oversized malformed OSC = %q, want %q", got, "initial")
	}
	if got := lineText(vt.Screen[0]); got != "OK" {
		t.Fatalf("oversized malformed OSC leaked to screen: row 0 = %q, want %q", got, "OK")
	}
}

func TestParserOversizedCSISequenceDiscardedUntilFinal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		csi  string
	}{
		{
			name: "single oversized param",
			csi:  "\x1b[" + strings.Repeat("1", maxCSIParamBytes+1) + "m",
		},
		{
			name: "too many params",
			csi:  "\x1b[" + strings.Repeat(";", maxCSIParams+1) + "m",
		},
		{
			name: "too many subparams",
			csi:  "\x1b[" + strings.Repeat("1:", maxCSIParams+1) + "m",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			vt := New(20, 2)
			vt.Write([]byte("A" + tc.csi + "B"))

			if got := lineText(vt.Screen[0]); got != "AB" {
				t.Fatalf("oversized CSI payload leaked to screen: row 0 = %q, want %q", got, "AB")
			}
		})
	}
}
