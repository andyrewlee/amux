package vterm

import (
	"strconv"
	"strings"
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

func TestRenderSuppressesUnderlineOnBlankCells(t *testing.T) {
	vt := New(5, 1)
	vt.CurrentStyle.Underline = true
	vt.Write([]byte("     "))

	out := vt.Render()
	if containsSGRParam(out, 4) {
		t.Fatalf("expected no underline SGR for blank cells, got %q", out)
	}
}

func TestStyleToDeltaANSIPreservesBoldWhenTurningOffDim(t *testing.T) {
	prev := Style{Bold: true, Dim: true}
	next := Style{Bold: true}
	out := StyleToDeltaANSI(prev, next)
	if !strings.Contains(out, "22") || !strings.Contains(out, "1") {
		t.Fatalf("expected delta to disable dim and preserve bold, got %q", out)
	}
}

func TestVersionBumpsOnCursorMoveAndHide(t *testing.T) {
	vt := New(10, 5)
	v0 := vt.Version()
	vt.Write([]byte("\x1b[C")) // CUF - cursor forward
	if vt.Version() == v0 {
		t.Fatalf("expected version to bump on cursor move")
	}

	v1 := vt.Version()
	vt.Write([]byte("\x1b[?25l")) // hide cursor
	if vt.Version() == v1 {
		t.Fatalf("expected version to bump on cursor hide")
	}
}

func TestRenderKeepsUnderlineForText(t *testing.T) {
	vt := New(2, 1)
	vt.CurrentStyle.Underline = true
	vt.Write([]byte("A "))

	out := vt.Render()
	if !containsSGRParam(out, 4) {
		t.Fatalf("expected underline SGR for text, got %q", out)
	}
}

func containsSGRParam(s string, target int) bool {
	targetStr := strconv.Itoa(target)
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b || i+1 >= len(s) || s[i+1] != '[' {
			continue
		}
		j := i + 2
		for j < len(s) && s[j] != 'm' {
			j++
		}
		if j >= len(s) {
			break
		}
		params := strings.Split(s[i+2:j], ";")
		for _, param := range params {
			if param == targetStr {
				return true
			}
		}
		i = j
	}
	return false
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

func TestOverwriteWideCharWithNarrowChar(t *testing.T) {
	vt := New(10, 1)

	// Write a wide character
	vt.Write([]byte("你"))
	// Verify initial state: wide char at 0, continuation at 1
	if vt.Screen[0][0].Width != 2 {
		t.Fatalf("Expected wide char at position 0, got width %d", vt.Screen[0][0].Width)
	}
	if vt.Screen[0][1].Width != 0 {
		t.Fatalf("Expected continuation cell at position 1, got width %d", vt.Screen[0][1].Width)
	}

	// Move cursor back to position 0 and write narrow char
	vt.CursorX = 0
	vt.Write([]byte("A"))

	// The narrow char should replace the wide char
	if vt.Screen[0][0].Rune != 'A' {
		t.Errorf("Expected 'A' at position 0, got %c", vt.Screen[0][0].Rune)
	}
	if vt.Screen[0][0].Width != 1 {
		t.Errorf("Expected width 1 at position 0, got %d", vt.Screen[0][0].Width)
	}

	// The continuation cell should be cleared (replaced with default)
	if vt.Screen[0][1].Width != 1 {
		t.Errorf("Expected continuation cell to be cleared (width 1), got width %d", vt.Screen[0][1].Width)
	}
	if vt.Screen[0][1].Rune != ' ' {
		t.Errorf("Expected space at cleared continuation cell, got %c", vt.Screen[0][1].Rune)
	}

	assertLineNormalized(t, vt.Screen[0])
}

func TestOverwriteContinuationCellWithNarrowChar(t *testing.T) {
	vt := New(10, 1)

	// Write a wide character
	vt.Write([]byte("你"))

	// Move cursor to position 1 (continuation cell) and write narrow char
	vt.CursorX = 1
	vt.Write([]byte("B"))

	// The wide char's first cell should be cleared
	if vt.Screen[0][0].Rune != ' ' {
		t.Errorf("Expected space at cleared wide char cell, got %c", vt.Screen[0][0].Rune)
	}
	if vt.Screen[0][0].Width != 1 {
		t.Errorf("Expected width 1 at cleared wide char cell, got %d", vt.Screen[0][0].Width)
	}

	// The narrow char should be at position 1
	if vt.Screen[0][1].Rune != 'B' {
		t.Errorf("Expected 'B' at position 1, got %c", vt.Screen[0][1].Rune)
	}
	if vt.Screen[0][1].Width != 1 {
		t.Errorf("Expected width 1 at position 1, got %d", vt.Screen[0][1].Width)
	}

	assertLineNormalized(t, vt.Screen[0])
}

func TestOverwriteNarrowCharsWithWideChar(t *testing.T) {
	vt := New(10, 1)

	// Write two narrow characters
	vt.Write([]byte("AB"))

	// Move cursor back to position 0 and write wide char
	vt.CursorX = 0
	vt.Write([]byte("你"))

	// Wide char should be at position 0
	if vt.Screen[0][0].Rune != '你' {
		t.Errorf("Expected wide char at position 0, got %c", vt.Screen[0][0].Rune)
	}
	if vt.Screen[0][0].Width != 2 {
		t.Errorf("Expected width 2 at position 0, got %d", vt.Screen[0][0].Width)
	}

	// Continuation at position 1
	if vt.Screen[0][1].Width != 0 {
		t.Errorf("Expected continuation cell (width 0) at position 1, got %d", vt.Screen[0][1].Width)
	}

	assertLineNormalized(t, vt.Screen[0])
}

func TestOverwriteWideCharWithWideChar(t *testing.T) {
	vt := New(10, 1)

	// Write first wide character
	vt.Write([]byte("你"))

	// Move cursor back to position 0 and write another wide char
	vt.CursorX = 0
	vt.Write([]byte("好"))

	// Second wide char should replace first
	if vt.Screen[0][0].Rune != '好' {
		t.Errorf("Expected '好' at position 0, got %c", vt.Screen[0][0].Rune)
	}
	if vt.Screen[0][0].Width != 2 {
		t.Errorf("Expected width 2 at position 0, got %d", vt.Screen[0][0].Width)
	}

	// Continuation at position 1
	if vt.Screen[0][1].Width != 0 {
		t.Errorf("Expected continuation cell at position 1, got width %d", vt.Screen[0][1].Width)
	}

	assertLineNormalized(t, vt.Screen[0])
}

func TestWideCharOverwritesAdjacentWideChar(t *testing.T) {
	vt := New(10, 1)

	// Write two adjacent wide characters: "你好" at positions 0-1 and 2-3
	vt.Write([]byte("你好"))

	// Move cursor to position 1 (continuation of first wide char) and write wide char
	// This should clear the first wide char AND overwrite into the second wide char's first cell
	vt.CursorX = 1
	vt.Write([]byte("世"))

	// First cell should be cleared (was wide char, now its continuation is overwritten)
	if vt.Screen[0][0].Rune != ' ' {
		t.Errorf("Expected space at position 0, got %c", vt.Screen[0][0].Rune)
	}
	if vt.Screen[0][0].Width != 1 {
		t.Errorf("Expected width 1 at position 0, got %d", vt.Screen[0][0].Width)
	}

	// New wide char should be at position 1-2
	if vt.Screen[0][1].Rune != '世' {
		t.Errorf("Expected '世' at position 1, got %c", vt.Screen[0][1].Rune)
	}
	if vt.Screen[0][1].Width != 2 {
		t.Errorf("Expected width 2 at position 1, got %d", vt.Screen[0][1].Width)
	}
	if vt.Screen[0][2].Width != 0 {
		t.Errorf("Expected continuation at position 2, got width %d", vt.Screen[0][2].Width)
	}

	// The second original wide char's continuation (position 3) should be cleared
	if vt.Screen[0][3].Rune != ' ' {
		t.Errorf("Expected space at position 3 (cleared continuation), got %c", vt.Screen[0][3].Rune)
	}
	if vt.Screen[0][3].Width != 1 {
		t.Errorf("Expected width 1 at position 3, got %d", vt.Screen[0][3].Width)
	}

	assertLineNormalized(t, vt.Screen[0])
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

func TestWideCharIncrementalUpdate(t *testing.T) {
	// Test that wide characters in incremental updates don't leave orphans
	vt := New(20, 1)

	// Write "你好世界" (4 wide chars = 8 cells)
	vt.Write([]byte("你好世界"))

	// Move cursor to position 2 and overwrite with narrow chars
	// This overwrites the continuation cell of "你" and the start of "好"
	vt.CursorX = 2
	vt.Write([]byte("AB"))

	// Expected state:
	// - Position 0: "你" should be cleared (its continuation at 1 is overwritten)
	// - Position 1: space (cleared)... wait, no - position 2 is the start of "好"
	// Let me recalculate:
	// Position 0: 你 (width 2)
	// Position 1: continuation
	// Position 2: 好 (width 2)
	// Position 3: continuation
	// Position 4: 世 (width 2)
	// Position 5: continuation
	// Position 6: 界 (width 2)
	// Position 7: continuation

	// When we write "AB" at position 2, we overwrite "好" and its continuation
	// "你" should remain intact

	// Position 0: 你 should remain
	if vt.Screen[0][0].Rune != '你' {
		t.Errorf("Expected '你' at position 0, got %c", vt.Screen[0][0].Rune)
	}
	// Position 1: continuation
	if vt.Screen[0][1].Width != 0 {
		t.Errorf("Expected continuation at position 1, got width %d", vt.Screen[0][1].Width)
	}
	// Position 2: A
	if vt.Screen[0][2].Rune != 'A' {
		t.Errorf("Expected 'A' at position 2, got %c", vt.Screen[0][2].Rune)
	}
	// Position 3: B
	if vt.Screen[0][3].Rune != 'B' {
		t.Errorf("Expected 'B' at position 3, got %c", vt.Screen[0][3].Rune)
	}
	// Position 4: 世 should remain
	if vt.Screen[0][4].Rune != '世' {
		t.Errorf("Expected '世' at position 4, got %c", vt.Screen[0][4].Rune)
	}

	assertLineNormalized(t, vt.Screen[0])
}
