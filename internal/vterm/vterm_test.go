package vterm

import (
	"testing"
)

func TestScrollUpClamping(t *testing.T) {
	vt := New(80, 24)
	// Set scroll region to lines 5-15 (10 lines)
	vt.ScrollTop = 5
	vt.ScrollBottom = 15

	// Fill region with content
	for i := 5; i < 15; i++ {
		vt.Screen[i][0] = Cell{Rune: rune('A' + i - 5), Width: 1}
	}

	// Scroll by more than region height (should clamp to 10)
	vt.scrollUp(100)

	// All lines in region should now be blank (space character)
	for i := 5; i < 15; i++ {
		if vt.Screen[i][0].Rune != ' ' {
			t.Errorf("Line %d should be blank after excessive scroll, got %c", i, vt.Screen[i][0].Rune)
		}
	}
}

func TestScrollDownClamping(t *testing.T) {
	vt := New(80, 24)
	// Set scroll region to lines 5-15 (10 lines)
	vt.ScrollTop = 5
	vt.ScrollBottom = 15

	// Fill region with content
	for i := 5; i < 15; i++ {
		vt.Screen[i][0] = Cell{Rune: rune('A' + i - 5), Width: 1}
	}

	// Scroll down by more than region height (should clamp to 10)
	vt.scrollDown(100)

	// All lines in region should now be blank (space character)
	for i := 5; i < 15; i++ {
		if vt.Screen[i][0].Rune != ' ' {
			t.Errorf("Line %d should be blank after excessive scroll, got %c", i, vt.Screen[i][0].Rune)
		}
	}
}

func TestInsertLinesClamping(t *testing.T) {
	vt := New(80, 24)
	vt.ScrollTop = 0
	vt.ScrollBottom = 24
	vt.CursorY = 20

	// Fill lines with content
	for i := 20; i < 24; i++ {
		vt.Screen[i][0] = Cell{Rune: rune('A' + i - 20), Width: 1}
	}

	// Insert more lines than available space (4 lines from cursor to bottom)
	vt.insertLines(100)

	// Lines from cursor to bottom should be blank (space character)
	for i := 20; i < 24; i++ {
		if vt.Screen[i][0].Rune != ' ' {
			t.Errorf("Line %d should be blank after insert, got %c", i, vt.Screen[i][0].Rune)
		}
	}
}

func TestDeleteLinesClamping(t *testing.T) {
	vt := New(80, 24)
	vt.ScrollTop = 0
	vt.ScrollBottom = 24
	vt.CursorY = 20

	// Fill lines with content
	for i := 20; i < 24; i++ {
		vt.Screen[i][0] = Cell{Rune: rune('A' + i - 20), Width: 1}
	}

	// Delete more lines than available (4 lines from cursor to bottom)
	vt.deleteLines(100)

	// Lines from cursor to bottom should be blank (space character)
	for i := 20; i < 24; i++ {
		if vt.Screen[i][0].Rune != ' ' {
			t.Errorf("Line %d should be blank after delete, got %c", i, vt.Screen[i][0].Rune)
		}
	}
}

func TestResizeMinimumDimensions(t *testing.T) {
	vt := New(80, 24)
	vt.CursorX = 10
	vt.CursorY = 10

	// Resize to zero dimensions (should clamp to 1x1)
	vt.Resize(0, 0)

	if vt.Width != 1 {
		t.Errorf("Width should be clamped to 1, got %d", vt.Width)
	}
	if vt.Height != 1 {
		t.Errorf("Height should be clamped to 1, got %d", vt.Height)
	}
	if vt.CursorX < 0 {
		t.Errorf("CursorX should not be negative, got %d", vt.CursorX)
	}
	if vt.CursorY < 0 {
		t.Errorf("CursorY should not be negative, got %d", vt.CursorY)
	}
}

func TestResizeNegativeDimensions(t *testing.T) {
	vt := New(80, 24)

	// Resize to negative dimensions (should clamp to 1x1)
	vt.Resize(-5, -10)

	if vt.Width != 1 {
		t.Errorf("Width should be clamped to 1, got %d", vt.Width)
	}
	if vt.Height != 1 {
		t.Errorf("Height should be clamped to 1, got %d", vt.Height)
	}
}

func TestViewOffsetClampedAfterTrim(t *testing.T) {
	vt := New(80, 24)

	// Add scrollback lines
	for i := 0; i < 100; i++ {
		line := MakeBlankLine(80)
		line[0] = Cell{Rune: rune('0' + i%10), Width: 1}
		vt.Scrollback = append(vt.Scrollback, line)
	}

	// Set ViewOffset to end of scrollback
	vt.ViewOffset = 100

	// Trim scrollback to 50 lines
	vt.Scrollback = vt.Scrollback[50:]
	vt.trimScrollback()

	// ViewOffset should be clamped
	if vt.ViewOffset > len(vt.Scrollback) {
		t.Errorf("ViewOffset should be clamped to %d, got %d", len(vt.Scrollback), vt.ViewOffset)
	}
}

func TestCSISubParameterParsing(t *testing.T) {
	vt := New(80, 24)

	// Test colon-separated 256-color foreground: ESC[38:5:196m
	vt.Write([]byte("\x1b[38:5:196m"))
	if vt.CurrentStyle.Fg.Type != ColorIndexed {
		t.Errorf("Expected ColorIndexed, got %v", vt.CurrentStyle.Fg.Type)
	}
	if vt.CurrentStyle.Fg.Value != 196 {
		t.Errorf("Expected color 196, got %d", vt.CurrentStyle.Fg.Value)
	}

	// Reset
	vt.Write([]byte("\x1b[0m"))

	// Test colon-separated truecolor: ESC[38:2:255:128:0m
	vt.Write([]byte("\x1b[38:2:255:128:0m"))
	if vt.CurrentStyle.Fg.Type != ColorRGB {
		t.Errorf("Expected ColorRGB, got %v", vt.CurrentStyle.Fg.Type)
	}
	expected := uint32(255)<<16 | uint32(128)<<8 | uint32(0)
	if vt.CurrentStyle.Fg.Value != expected {
		t.Errorf("Expected RGB value %d, got %d", expected, vt.CurrentStyle.Fg.Value)
	}
}

func TestCSISemicolonParameterParsing(t *testing.T) {
	vt := New(80, 24)

	// Test semicolon-separated 256-color (existing format, should still work)
	vt.Write([]byte("\x1b[38;5;196m"))
	if vt.CurrentStyle.Fg.Type != ColorIndexed {
		t.Errorf("Expected ColorIndexed, got %v", vt.CurrentStyle.Fg.Type)
	}
	if vt.CurrentStyle.Fg.Value != 196 {
		t.Errorf("Expected color 196, got %d", vt.CurrentStyle.Fg.Value)
	}
}

func TestVersionBumpsOnCursorMoveAndAltScreenCursorHide(t *testing.T) {
	vt := New(10, 5)
	v0 := vt.Version()
	vt.Write([]byte("\x1b[C")) // CUF - cursor forward
	if vt.Version() == v0 {
		t.Fatalf("expected version to bump on cursor move")
	}

	vt.Write([]byte("\x1b[?1049h")) // enter alt screen
	v1 := vt.Version()
	vt.Write([]byte("\x1b[?25l")) // hide cursor
	if vt.Version() == v1 {
		t.Fatalf("expected version to bump on cursor hide in alt screen")
	}
}

func TestCursorHideOutsideAltScreenBumpsVersion(t *testing.T) {
	vt := New(10, 5)
	v0 := vt.Version()

	vt.Write([]byte("\x1b[?25l")) // hide cursor
	if vt.Version() == v0 {
		t.Fatalf("expected version to bump on cursor hide outside alt screen")
	}
}

func TestScrollbackViewOffsetAnchorsOnScrollUp(t *testing.T) {
	vt := New(5, 3)
	// Seed scrollback so ViewOffset is meaningful.
	for i := 0; i < 3; i++ {
		line := MakeBlankLine(5)
		line[0] = Cell{Rune: rune('0' + i), Width: 1}
		vt.Scrollback = append(vt.Scrollback, line)
	}
	vt.ViewOffset = 2

	// Scroll up by one line, which appends to scrollback.
	vt.scrollUp(1)

	if vt.ViewOffset != 3 {
		t.Fatalf("expected ViewOffset to advance to 3, got %d", vt.ViewOffset)
	}
}

func TestScrollbackViewOffsetAnchorsOnResize(t *testing.T) {
	vt := New(5, 3)
	// Seed scrollback so ViewOffset is meaningful.
	for i := 0; i < 2; i++ {
		line := MakeBlankLine(5)
		line[0] = Cell{Rune: rune('0' + i), Width: 1}
		vt.Scrollback = append(vt.Scrollback, line)
	}
	vt.ViewOffset = 1

	// Shrink height by 1; one line moves into scrollback.
	vt.Resize(5, 2)

	if vt.ViewOffset != 2 {
		t.Fatalf("expected ViewOffset to advance to 2 after resize, got %d", vt.ViewOffset)
	}
}

func TestResizeRestoresScrollbackOnGrow(t *testing.T) {
	vt := New(5, 3)
	// Seed scrollback with two lines.
	for i := 0; i < 2; i++ {
		line := MakeBlankLine(5)
		line[0] = Cell{Rune: rune('s' + i), Width: 1}
		vt.Scrollback = append(vt.Scrollback, line)
	}
	// Seed screen lines.
	for i := 0; i < 3; i++ {
		line := MakeBlankLine(5)
		line[0] = Cell{Rune: rune('a' + i), Width: 1}
		vt.Screen[i] = line
	}
	vt.CursorY = 2

	vt.Resize(5, 5)

	if len(vt.Screen) != 5 {
		t.Fatalf("expected screen height 5, got %d", len(vt.Screen))
	}
	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected scrollback empty after restore, got %d", len(vt.Scrollback))
	}
	if vt.Screen[0][0].Rune != 's' || vt.Screen[1][0].Rune != 't' {
		t.Fatalf("expected restored scrollback at top, got %c/%c", vt.Screen[0][0].Rune, vt.Screen[1][0].Rune)
	}
	if vt.Screen[2][0].Rune != 'a' || vt.Screen[3][0].Rune != 'b' || vt.Screen[4][0].Rune != 'c' {
		t.Fatalf("expected original screen lines shifted down, got %c/%c/%c", vt.Screen[2][0].Rune, vt.Screen[3][0].Rune, vt.Screen[4][0].Rune)
	}
	if vt.CursorY != 4 {
		t.Fatalf("expected cursor to shift to 4, got %d", vt.CursorY)
	}
}

func TestIncrementalCursorPositionedWrites(t *testing.T) {
	// Test cursor positioning + partial writes (common in Ink/React TUIs, progress bars, etc.)
	vt := New(20, 3)

	// First render: "Analytics" at row 0
	vt.Write([]byte("\x1b[1;1H")) // Move cursor to row 1, col 1
	vt.Write([]byte("Analytics"))

	// Verify initial state
	expected := "Analytics"
	for i, ch := range expected {
		if vt.Screen[0][i].Rune != ch {
			t.Errorf("Initial: expected %c at position %d, got %c", ch, i, vt.Screen[0][i].Rune)
		}
	}

	// Second render: overwrite with "Dashboard" (longer text)
	vt.Write([]byte("\x1b[1;1H")) // Move cursor to row 1, col 1
	vt.Write([]byte("Dashboard"))

	// Verify "Dashboard" is correctly rendered without artifacts
	expected = "Dashboard"
	for i, ch := range expected {
		if vt.Screen[0][i].Rune != ch {
			t.Errorf("After overwrite: expected %c at position %d, got %c", ch, i, vt.Screen[0][i].Rune)
		}
	}

	// Third render: overwrite with shorter text "Todo"
	vt.Write([]byte("\x1b[1;1H"))
	vt.Write([]byte("Todo"))

	// "Todo" should be there, but old chars "board" might still exist
	// This is expected behavior - the terminal doesn't clear beyond what was written
	if string([]rune{vt.Screen[0][0].Rune, vt.Screen[0][1].Rune, vt.Screen[0][2].Rune, vt.Screen[0][3].Rune}) != "Todo" {
		t.Errorf("Expected 'Todo' at start of line")
	}
}
