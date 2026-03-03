package app

import "testing"

func TestFastASCIIWidthPrintableASCII(t *testing.T) {
	if got, ok := fastASCIIWidth("abcXYZ123 !"); !ok || got != 11 {
		t.Fatalf("expected printable ASCII fast path width=11 ok=true, got width=%d ok=%v", got, ok)
	}
}

func TestFastASCIIWidthRejectsControlBytes(t *testing.T) {
	tests := []string{
		"a\tb",
		"a\rb",
		"a\x7fb",
		"a\x1bb",
	}
	for _, tc := range tests {
		if _, ok := fastASCIIWidth(tc); ok {
			t.Fatalf("expected fast path fallback for %q", tc)
		}
	}
}

func TestClampLinesControlBytesDoNotForceRawByteTruncation(t *testing.T) {
	got := clampLines("a\tb", 2, 1)
	if got != "a\tb" {
		t.Fatalf("expected control-byte line to avoid raw-byte truncation, got %q", got)
	}
}
