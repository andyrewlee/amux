package vterm

import "testing"

func TestNormalScreenClearCapturesChatRedrawFrame(t *testing.T) {
	t.Parallel()
	vt := New(12, 4)
	vt.CaptureNormalScreenOnClear = true

	vt.Write([]byte("\x1b[?2026hfirst\r\nsecond\x1b[2J\x1b[3J\x1b[?2026l"))

	if len(vt.Scrollback) < 2 {
		t.Fatalf("expected normal-screen redraw to be captured, got %d rows", len(vt.Scrollback))
	}
	if got := lineText(vt.Scrollback[0]); got != "first" {
		t.Fatalf("expected first captured row, got %q", got)
	}
	if got := lineText(vt.Scrollback[1]); got != "second" {
		t.Fatalf("expected second captured row, got %q", got)
	}
}

func TestNormalScreenHomeEraseToEndCapturesChatRedrawFrame(t *testing.T) {
	t.Parallel()
	vt := New(12, 4)
	vt.CaptureNormalScreenOnClear = true

	vt.Write([]byte("first\r\nsecond\x1b[H\x1b[Jnew"))

	if len(vt.Scrollback) < 2 {
		t.Fatalf("expected home erase redraw to be captured, got %d rows", len(vt.Scrollback))
	}
	if got := lineText(vt.Scrollback[0]); got != "first" {
		t.Fatalf("expected first captured row, got %q", got)
	}
	if got := lineText(vt.Scrollback[1]); got != "second" {
		t.Fatalf("expected second captured row, got %q", got)
	}
}

func TestNormalScreenHomeEraseToEndPreservesFrameThroughImmediateClear3(t *testing.T) {
	t.Parallel()
	vt := New(12, 4)
	vt.CaptureNormalScreenOnClear = true

	vt.Write([]byte("\x1b[?2026hfirst\r\nsecond\x1b[H\x1b[J\x1b[3Jnew\x1b[?2026l"))

	if len(vt.Scrollback) < 2 {
		t.Fatalf("expected home erase redraw to survive immediate 3J, got %d rows", len(vt.Scrollback))
	}
	if got := lineText(vt.Scrollback[0]); got != "first" {
		t.Fatalf("expected first captured row, got %q", got)
	}
	if got := lineText(vt.Scrollback[1]); got != "second" {
		t.Fatalf("expected second captured row, got %q", got)
	}
}

func TestNormalScreenClearStillClearsScrollbackByDefault(t *testing.T) {
	t.Parallel()
	vt := New(12, 4)
	vt.Write([]byte("first\r\nsecond\x1b[2J\x1b[3J"))

	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected normal-screen clear to preserve default 3J behavior, got %d rows", len(vt.Scrollback))
	}
}

func TestNormalScreenClear3ClearsChatScrollbackWhenStandalone(t *testing.T) {
	t.Parallel()
	vt := New(12, 4)
	vt.CaptureNormalScreenOnClear = true
	vt.Scrollback = append(vt.Scrollback, textLine("saved", vt.Width))

	vt.Write([]byte("\x1b[3J"))

	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected standalone 3J to clear chat scrollback, got %d rows", len(vt.Scrollback))
	}
}

func TestNormalScreenClear3ClearsChatScrollbackAfterInterveningOutput(t *testing.T) {
	t.Parallel()
	vt := New(12, 4)
	vt.CaptureNormalScreenOnClear = true

	vt.Write([]byte("old\x1b[2Jnew\x1b[3J"))

	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected delayed 3J to clear captured chat scrollback, got %d rows", len(vt.Scrollback))
	}
}

func TestNormalScreenImmediateClear3ClearsChatScrollbackAfterUnsynchronizedClear2Capture(t *testing.T) {
	t.Parallel()
	vt := New(12, 4)
	vt.CaptureNormalScreenOnClear = true

	vt.Write([]byte("old\x1b[2J\x1b[3J"))

	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected unsynchronized 2J then 3J to clear captured chat scrollback, got %d rows", len(vt.Scrollback))
	}
}

func TestNormalScreenImmediateClear3ClearsChatScrollbackAfterUnsynchronizedHomeEraseCapture(t *testing.T) {
	t.Parallel()
	vt := New(12, 4)
	vt.CaptureNormalScreenOnClear = true

	vt.Write([]byte("old\x1b[H\x1b[J\x1b[3J"))

	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected unsynchronized home erase then 3J to clear captured chat scrollback, got %d rows", len(vt.Scrollback))
	}
}

func TestNormalScreenImmediateClear3ClearsChatScrollbackWhenClear2CapturesNothing(t *testing.T) {
	t.Parallel()
	vt := New(12, 4)
	vt.CaptureNormalScreenOnClear = true
	vt.Scrollback = append(vt.Scrollback, textLine("saved", vt.Width))

	vt.Write([]byte("\x1b[2J\x1b[3J"))

	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected blank 2J then 3J to clear chat scrollback, got %d rows", len(vt.Scrollback))
	}
}

func TestNormalScreenImmediateClear3ClearsChatScrollbackWhenHomeEraseCapturesNothing(t *testing.T) {
	t.Parallel()
	vt := New(12, 4)
	vt.CaptureNormalScreenOnClear = true
	vt.Scrollback = append(vt.Scrollback, textLine("saved", vt.Width))

	vt.Write([]byte("\x1b[H\x1b[J\x1b[3J"))

	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected blank home erase then 3J to clear chat scrollback, got %d rows", len(vt.Scrollback))
	}
}

func textLine(text string, width int) []Cell {
	line := MakeBlankLine(width)
	for i, r := range text {
		if i >= len(line) {
			break
		}
		line[i] = Cell{Rune: r, Width: 1}
	}
	return line
}
