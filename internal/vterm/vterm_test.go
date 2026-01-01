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

func TestWideCharacterWidth(t *testing.T) {
	vt := New(80, 24)

	// Write a wide character (CJK)
	vt.Write([]byte("你")) // Chinese character, width 2

	// Should have advanced cursor by 2
	if vt.CursorX != 2 {
		t.Errorf("Cursor should be at X=2 after wide char, got %d", vt.CursorX)
	}

	// First cell should have the character with Width=2
	if vt.Screen[0][0].Width != 2 {
		t.Errorf("Wide char cell should have Width=2, got %d", vt.Screen[0][0].Width)
	}

	// Second cell should be continuation (Width=0)
	if vt.Screen[0][1].Width != 0 {
		t.Errorf("Continuation cell should have Width=0, got %d", vt.Screen[0][1].Width)
	}
}

func TestWideCharacterAtEndOfLine(t *testing.T) {
	vt := New(10, 5) // Narrow terminal
	vt.CursorX = 9   // Last column

	// Write a wide character at the last column
	vt.Write([]byte("你"))

	// Wide char shouldn't be split across lines
	// It should wrap to the next line
	if vt.Screen[0][9].Rune != ' ' {
		t.Errorf("Last column should have space (padding), got %c", vt.Screen[0][9].Rune)
	}
	if vt.CursorY != 1 {
		t.Errorf("Cursor should have wrapped to line 1, got %d", vt.CursorY)
	}
	if vt.Screen[1][0].Rune != '你' {
		t.Errorf("Wide char should be on next line, got %c", vt.Screen[1][0].Rune)
	}
}

func TestRowToStringSkipsContinuationCells(t *testing.T) {
	vt := New(10, 1)
	vt.Write([]byte("你A"))

	got := rowToString(vt.Screen[0])
	if got != "你A" {
		t.Fatalf("rowToString() = %q, want %q", got, "你A")
	}
}

func TestSearchSkipsContinuationCells(t *testing.T) {
	vt := New(10, 1)
	vt.Write([]byte("你A"))

	matches := vt.Search("你A")
	if len(matches) != 1 || matches[0] != 0 {
		t.Fatalf("Search() = %v, want [0]", matches)
	}
}

func TestGetSelectedTextSkipsContinuationCells(t *testing.T) {
	vt := New(10, 1)
	vt.Write([]byte("你A"))

	got := vt.GetSelectedText(0, 0, 2, 0)
	if got != "你A" {
		t.Fatalf("GetSelectedText() = %q, want %q", got, "你A")
	}
}

func TestNormalCharacterWidth(t *testing.T) {
	vt := New(80, 24)

	// Write a normal ASCII character
	vt.Write([]byte("A"))

	if vt.CursorX != 1 {
		t.Errorf("Cursor should be at X=1 after normal char, got %d", vt.CursorX)
	}
	if vt.Screen[0][0].Width != 1 {
		t.Errorf("Normal char should have Width=1, got %d", vt.Screen[0][0].Width)
	}
}

func assertLineNormalized(t *testing.T, line []Cell) {
	t.Helper()
	for i := 0; i < len(line); i++ {
		switch line[i].Width {
		case 0:
			if i == 0 || line[i-1].Width != 2 {
				t.Fatalf("continuation cell at %d without leading wide cell", i)
			}
		case 2:
			if i+1 >= len(line) || line[i+1].Width != 0 {
				t.Fatalf("wide cell at %d without continuation", i)
			}
		}
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

func TestWideGlyphNormalizationAfterEdits(t *testing.T) {
	vt := New(6, 1)
	vt.Write([]byte("你A"))

	// Insert at continuation cell.
	vt.CursorX = 1
	vt.insertChars(1)
	assertLineNormalized(t, vt.Screen[0])

	// Delete at continuation cell.
	vt.CursorX = 1
	vt.deleteChars(1)
	assertLineNormalized(t, vt.Screen[0])

	// Erase at continuation cell.
	vt.CursorX = 1
	vt.eraseChars(1)
	assertLineNormalized(t, vt.Screen[0])
}
