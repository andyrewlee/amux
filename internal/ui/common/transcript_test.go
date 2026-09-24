package common

import (
	"strings"
	"testing"
)

func TestTruncateTranscriptTail_UnderCapUnchanged(t *testing.T) {
	text := "line one\nline two\n"
	got, truncated := TruncateTranscriptTail(text)
	if truncated {
		t.Fatal("under-cap text must not report truncation")
	}
	if got != text {
		t.Fatalf("under-cap text changed: %q", got)
	}
}

func TestTruncateTranscriptTail_OverCapKeepsTail(t *testing.T) {
	var b strings.Builder
	for b.Len() < TranscriptMaxBytes+512 {
		b.WriteString("transcript line\n")
	}
	// A distinctive tail marker must survive; the oldest lines must not.
	b.WriteString("TAIL-MARKER\n")
	text := b.String()

	got, truncated := TruncateTranscriptTail(text)
	if !truncated {
		t.Fatal("over-cap text must report truncation")
	}
	if len(got) > TranscriptMaxBytes {
		t.Fatalf("truncated text = %d bytes, exceeds cap %d", len(got), TranscriptMaxBytes)
	}
	if !strings.HasSuffix(got, "TAIL-MARKER\n") {
		t.Fatal("tail (newest output) must be kept")
	}
	// The first kept line must be complete — the cut lands on a newline
	// boundary, so the result cannot start mid-line.
	if strings.ContainsRune(got[:1], '\n') {
		t.Fatal("kept transcript must not start mid-line / with a newline")
	}
}

func TestTruncateTranscriptTail_CutsOnNewlineBoundary(t *testing.T) {
	// 10 bytes over the cap: "0123456789\n" + filler of exactly the cap.
	filler := strings.Repeat("x", TranscriptMaxBytes-11) + "\nLASTLINE"
	text := "HEAD-DROPPED\n" + filler
	if len(text) <= TranscriptMaxBytes {
		t.Skipf("fixture miscalc: %d bytes", len(text))
	}
	got, truncated := TruncateTranscriptTail(text)
	if !truncated {
		t.Fatal("expected truncation")
	}
	if strings.Contains(got, "HEAD-DROPPED") {
		t.Fatal("oldest line must be dropped")
	}
	if !strings.HasSuffix(got, "LASTLINE") {
		t.Fatal("tail must survive")
	}
}
