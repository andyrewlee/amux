package vterm

import "testing"

func TestAltScreenEraseCapturesScrollback(t *testing.T) {
	vt := New(10, 3)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h")) // enter alt screen

	// Write content then erase display
	vt.Write([]byte("hello\r\nworld"))
	vt.Write([]byte("\x1b[2J")) // erase display

	if len(vt.Scrollback) < 2 {
		t.Fatalf("expected at least 2 scrollback lines, got %d", len(vt.Scrollback))
	}

	// First captured line should contain "hello"
	if vt.Scrollback[0][0].Rune != 'h' {
		t.Errorf("expected 'h', got %c", vt.Scrollback[0][0].Rune)
	}
	// Second captured line should contain "world"
	if vt.Scrollback[1][0].Rune != 'w' {
		t.Errorf("expected 'w', got %c", vt.Scrollback[1][0].Rune)
	}
}

func TestAltScreenEraseDedup(t *testing.T) {
	vt := New(10, 3)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))

	// Write content, erase, rewrite identical content, erase again
	vt.Write([]byte("hello\r\nworld"))
	vt.Write([]byte("\x1b[2J"))
	before := len(vt.Scrollback)

	// Redraw identical content
	vt.Write([]byte("\x1b[Hhello\r\nworld"))
	vt.Write([]byte("\x1b[2J"))
	after := len(vt.Scrollback)

	if after != before {
		t.Errorf("expected dedup to prevent duplicate capture: before=%d, after=%d", before, after)
	}
}

func TestAltScreenEraseDedupedHistoryIsPreserved(t *testing.T) {
	vt := New(5, 2)
	vt.AllowAltScreenScrollback = true
	line := MakeBlankLine(5)
	copy(line, []Cell{
		{Rune: 'f', Width: 1},
		{Rune: 'r', Width: 1},
		{Rune: 'a', Width: 1},
		{Rune: 'm', Width: 1},
		{Rune: 'e', Width: 1},
	})
	vt.Scrollback = append(vt.Scrollback, line)

	vt.Write([]byte("\x1b[?1049h"))
	vt.Write([]byte("frame"))
	vt.Write([]byte("\x1b[2J"))

	if vt.Scrollback[0][0].Rune != 'f' {
		t.Fatalf("expected existing history to remain, got %q", vt.Scrollback[0][0].Rune)
	}
	if vt.altScreenCaptureLen != 1 {
		t.Fatalf("expected deduped frame reserved for resize, got %d", vt.altScreenCaptureLen)
	}
	if vt.altScreenCaptureTracked {
		t.Fatalf("expected deduped frame not tracked as removable capture")
	}

	vt.Write([]byte("other"))
	vt.Write([]byte("\x1b[2J"))

	if len(vt.Scrollback) != 2 {
		t.Fatalf("expected existing history + new capture, got %d rows", len(vt.Scrollback))
	}
	if got := vt.Scrollback[0][0].Rune; got != 'f' {
		t.Fatalf("expected preexisting row to remain intact, got %q", got)
	}
}

func TestAltScreenEraseBlankNotCaptured(t *testing.T) {
	vt := New(10, 3)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))

	// Erase a blank screen — nothing should be captured
	vt.Write([]byte("\x1b[2J"))
	if len(vt.Scrollback) != 0 {
		t.Errorf("expected no scrollback for blank screen, got %d", len(vt.Scrollback))
	}
}

func TestAltScreenEraseNoCaptureWithoutFlag(t *testing.T) {
	vt := New(10, 3)
	// AllowAltScreenScrollback is false (default)
	vt.Write([]byte("\x1b[?1049h"))

	vt.Write([]byte("hello\r\nworld"))
	vt.Write([]byte("\x1b[2J"))

	if len(vt.Scrollback) != 0 {
		t.Errorf("expected no scrollback without AllowAltScreenScrollback, got %d", len(vt.Scrollback))
	}
}

func TestNormalScreenEraseNoCapture(t *testing.T) {
	vt := New(10, 3)
	vt.AllowAltScreenScrollback = true
	// NOT in alt screen

	vt.Write([]byte("hello\r\nworld"))
	vt.Write([]byte("\x1b[2J"))

	if len(vt.Scrollback) != 0 {
		t.Errorf("expected no scrollback capture on normal screen erase, got %d", len(vt.Scrollback))
	}
}

func TestAltScreenEraseRepaintReplacesPriorCapture(t *testing.T) {
	vt := New(10, 3)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))

	// Frame 1
	vt.Write([]byte("frame1"))
	vt.Write([]byte("\x1b[2J"))
	after1 := len(vt.Scrollback)

	// Frame 2 (different content)
	vt.Write([]byte("\x1b[Hframe2"))
	vt.Write([]byte("\x1b[2J"))
	after2 := len(vt.Scrollback)

	if after1 != 1 {
		t.Fatalf("expected first capture to add one row, got %d", after1)
	}
	if after2 != after1 {
		t.Fatalf("expected repaint capture to replace prior frame: after1=%d, after2=%d", after1, after2)
	}
	if got := vt.Scrollback[len(vt.Scrollback)-1][5].Rune; got != '2' {
		t.Fatalf("expected latest captured frame to be retained, got %q", got)
	}
}

func TestAltScreenEraseAnchorsViewOffset(t *testing.T) {
	vt := New(10, 3)
	vt.AllowAltScreenScrollback = true
	for i := 0; i < 2; i++ {
		line := MakeBlankLine(10)
		line[0] = Cell{Rune: rune('0' + i), Width: 1}
		vt.Scrollback = append(vt.Scrollback, line)
	}
	vt.ViewOffset = 1

	vt.Write([]byte("\x1b[?1049h"))
	vt.Write([]byte("hello\r\nworld"))
	vt.Write([]byte("\x1b[2J"))

	if vt.ViewOffset != 3 {
		t.Fatalf("expected ViewOffset to advance to 3 after capture, got %d", vt.ViewOffset)
	}
}

func TestAltScreenEraseReplacementPreservesViewOffset(t *testing.T) {
	vt := New(10, 3)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))

	vt.Write([]byte("frame1"))
	vt.Write([]byte("\x1b[2J"))
	vt.ViewOffset = 1

	vt.Write([]byte("\x1b[Hframe2"))
	vt.Write([]byte("\x1b[2J"))

	if vt.ViewOffset != 1 {
		t.Fatalf("expected ViewOffset to remain anchored at 1 during replacement, got %d", vt.ViewOffset)
	}
}

func TestAltScreenEraseTrimsLeadingBlankRows(t *testing.T) {
	vt := New(10, 6)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))

	vt.Write([]byte("\x1b[4;1Hcenter"))
	vt.Write([]byte("\x1b[2J"))

	if len(vt.Scrollback) != 1 {
		t.Fatalf("expected only the visible non-blank frame rows, got %d lines", len(vt.Scrollback))
	}
	if got := vt.Scrollback[0][0].Rune; got != 'c' {
		t.Fatalf("expected captured content row to start with 'c', got %q", got)
	}
}

func TestAltScreenEraseClipsCapturedRowsToVisibleWidth(t *testing.T) {
	vt := New(8, 3)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))
	vt.Write([]byte("abcdefgh"))
	vt.Resize(4, 3)

	vt.Write([]byte("\x1b[2J"))

	if len(vt.Scrollback) != 1 {
		t.Fatalf("expected one captured row, got %d", len(vt.Scrollback))
	}
	if got := len(vt.Scrollback[0]); got != 4 {
		t.Fatalf("expected captured row width 4, got %d", got)
	}
	if got := string([]rune{
		vt.Scrollback[0][0].Rune,
		vt.Scrollback[0][1].Rune,
		vt.Scrollback[0][2].Rune,
		vt.Scrollback[0][3].Rune,
	}); got != "abcd" {
		t.Fatalf("expected visible content \"abcd\", got %q", got)
	}
}

func TestAltScreenEraseDedupedFrameRemainsReservedOnResizeGrow(t *testing.T) {
	vt := New(5, 2)
	vt.AllowAltScreenScrollback = true
	line := MakeBlankLine(5)
	copy(line, []Cell{
		{Rune: 'f', Width: 1},
		{Rune: 'r', Width: 1},
		{Rune: 'a', Width: 1},
		{Rune: 'm', Width: 1},
		{Rune: 'e', Width: 1},
	})
	vt.Scrollback = append(vt.Scrollback, line)

	vt.Write([]byte("\x1b[?1049h"))
	vt.Write([]byte("frame"))
	vt.Write([]byte("\x1b[2J"))

	if vt.altScreenCaptureLen != 1 {
		t.Fatalf("expected deduped frame to stay reserved, got %d", vt.altScreenCaptureLen)
	}

	vt.Resize(5, 3)

	if got := vt.Screen[0][0].Rune; got != ' ' {
		t.Fatalf("expected resized alt screen to stay blank after deduped capture, got %q", got)
	}
}

func TestAltScreenEraseCapturedFrameNotRestoredOnResizeGrow(t *testing.T) {
	vt := New(5, 2)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))
	vt.Write([]byte("frame"))
	vt.Write([]byte("\x1b[2J"))

	vt.Resize(5, 3)

	if got := vt.Screen[0][0].Rune; got != ' ' {
		t.Fatalf("expected resized alt screen to stay blank, got %q", got)
	}
	if len(vt.Scrollback) != 1 {
		t.Fatalf("expected captured frame to remain in scrollback, got %d rows", len(vt.Scrollback))
	}
}

func TestAltScreenEraseDropsWideRuneClippedAtEdge(t *testing.T) {
	vt := New(4, 2)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))
	vt.Write([]byte("ab你"))
	vt.Resize(3, 2)

	vt.Write([]byte("\x1b[2J"))

	if len(vt.Scrollback) != 1 {
		t.Fatalf("expected one captured row, got %d", len(vt.Scrollback))
	}
	if got := vt.Scrollback[0][2]; got.Rune != ' ' || got.Width != 1 {
		t.Fatalf("expected clipped wide rune to be dropped, got rune=%q width=%d", got.Rune, got.Width)
	}
}

func TestAltScreenErasePreservesStyledSpaceRows(t *testing.T) {
	vt := New(4, 3)
	vt.AllowAltScreenScrollback = true
	vt.Write([]byte("\x1b[?1049h"))

	styled := MakeBlankLine(4)
	for i := range styled {
		styled[i] = Cell{
			Rune:  ' ',
			Width: 1,
			Style: Style{Bg: Color{Type: ColorIndexed, Value: 1}},
		}
	}
	vt.Screen[1] = styled

	vt.Write([]byte("\x1b[2J"))

	if len(vt.Scrollback) != 1 {
		t.Fatalf("expected styled-space row to be captured, got %d rows", len(vt.Scrollback))
	}
	if got := vt.Scrollback[0][0].Style.Bg; got.Type != ColorIndexed || got.Value != 1 {
		t.Fatalf("expected captured row to preserve styled background, got %+v", got)
	}
}
