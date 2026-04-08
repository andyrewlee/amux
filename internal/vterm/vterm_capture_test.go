package vterm

import "testing"

func TestCaptureLines_PreservesExplicitBlankTailRows(t *testing.T) {
	data := []byte("first\n\n")
	tmp := parseCaptureWithSize(data, 8, 4)
	lines := captureLines(data, tmp)

	if len(lines) != 2 {
		t.Fatalf("expected explicit blank tail row to be preserved, got %d rows", len(lines))
	}
	if got := lines[0][0].Rune; got != 'f' {
		t.Fatalf("expected first row to contain captured text, got %q", got)
	}
	if !isBlankLine(lines[1]) {
		t.Fatal("expected second row to remain an explicit blank history row")
	}
}

func TestCaptureLines_DropsImplicitUnusedScreenRows(t *testing.T) {
	data := []byte("first\nsecond\n")
	tmp := parseCaptureWithSize(data, 8, 4)
	lines := captureLines(data, tmp)

	if len(lines) != 2 {
		t.Fatalf("expected only explicit capture rows, got %d rows", len(lines))
	}
	if got := lines[1][0].Rune; got != 's' {
		t.Fatalf("expected second explicit row to contain captured text, got %q", got)
	}
}
