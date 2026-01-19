package vterm

import (
	"testing"
)

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
