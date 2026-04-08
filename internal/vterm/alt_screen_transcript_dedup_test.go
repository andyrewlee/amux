package vterm

import (
	"fmt"
	"testing"
)

func TestAltScreenRepeatedAboveFoldChangesDoNotDuplicateIdenticalRedraw(t *testing.T) {
	vt := New(10, 3)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))

	frames := []string{
		"AAA\r\nBBB\r\nCCC\r\nDDD\r\nEEE",
		"XXX\r\nYYY\r\nCCC\r\nDDD\r\nEEE",
		"PPP\r\nQQQ\r\nCCC\r\nDDD\r\nEEE",
		"PPP\r\nQQQ\r\nCCC\r\nDDD\r\nEEE",
	}

	vt.Write([]byte(frames[0]))
	vt.Write([]byte("\x1b[2J"))
	for _, frame := range frames[1:] {
		vt.Write([]byte("\x1b[H"))
		vt.Write([]byte(frame))
		vt.Write([]byte("\x1b[2J"))
	}

	want := []string{"AAA", "BBB", "XXX", "YYY", "PPP", "QQQ", "CCC", "DDD", "EEE"}
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

func TestAltScreenShiftRedrawDoesNotDuplicateRowsAlreadyScrolledOffDuringDraw(t *testing.T) {
	vt := New(12, 3)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))

	frames := []string{
		"status1\r\na\r\nb\r\nc\r\nd",
		"status2\r\nb\r\nc\r\nd\r\ne",
		"status3\r\nc\r\nd\r\ne\r\nf",
		"status4\r\nd\r\ne\r\nf\r\ng",
	}

	vt.Write([]byte(frames[0]))
	vt.Write([]byte("\x1b[2J"))
	for _, frame := range frames[1:] {
		vt.Write([]byte("\x1b[H"))
		vt.Write([]byte(frame))
		vt.Write([]byte("\x1b[2J"))
	}

	want := []string{"status1", "a", "status2", "b", "status3", "c", "status4", "d", "e", "f", "g"}
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

func TestAltScreenLongTranscriptShiftDoesNotDuplicateOrDropLines(t *testing.T) {
	vt := New(16, 3)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))

	frame := func(i int) string {
		return fmt.Sprintf(
			"status%02d\r\nline%02d\r\nline%02d\r\nline%02d\r\nline%02d",
			i, i, i+1, i+2, i+3,
		)
	}

	vt.Write([]byte(frame(0)))
	vt.Write([]byte("\x1b[2J"))
	for i := 1; i < 40; i++ {
		vt.Write([]byte("\x1b[H"))
		vt.Write([]byte(frame(i)))
		vt.Write([]byte("\x1b[2J"))
	}

	want := make([]string, 0, 83)
	for i := 0; i < 40; i++ {
		want = append(want, fmt.Sprintf("status%02d", i), fmt.Sprintf("line%02d", i))
	}
	want = append(want, "line40", "line41", "line42")

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
