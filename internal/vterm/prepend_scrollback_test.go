package vterm

import (
	"strings"
	"testing"
)

func TestPrependScrollbackEmpty(t *testing.T) {
	vt := New(80, 24)
	vt.PrependScrollback(nil)
	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected empty scrollback, got %d lines", len(vt.Scrollback))
	}
	vt.PrependScrollback([]byte{})
	if len(vt.Scrollback) != 0 {
		t.Fatalf("expected empty scrollback, got %d lines", len(vt.Scrollback))
	}
}

func TestPrependScrollbackPlainText(t *testing.T) {
	vt := New(80, 24)
	vt.PrependScrollback([]byte("hello\nworld\n"))

	if len(vt.Scrollback) == 0 {
		t.Fatal("expected scrollback to have lines")
	}

	// First line should start with 'h'
	if vt.Scrollback[0][0].Rune != 'h' {
		t.Errorf("expected 'h', got %c", vt.Scrollback[0][0].Rune)
	}
}

func TestPrependScrollbackPreservesExisting(t *testing.T) {
	vt := New(80, 24)

	// Add existing scrollback
	existing := MakeBlankLine(80)
	existing[0] = Cell{Rune: 'E', Width: 1}
	vt.Scrollback = append(vt.Scrollback, existing)

	vt.PrependScrollback([]byte("prepended\n"))

	// Should have at least 2 lines: prepended + existing
	if len(vt.Scrollback) < 2 {
		t.Fatalf("expected at least 2 scrollback lines, got %d", len(vt.Scrollback))
	}

	// First line should be from prepended content
	if vt.Scrollback[0][0].Rune != 'p' {
		t.Errorf("first line should start with 'p', got %c", vt.Scrollback[0][0].Rune)
	}

	// Last line should be the original existing line
	last := vt.Scrollback[len(vt.Scrollback)-1]
	if last[0].Rune != 'E' {
		t.Errorf("last line should be existing 'E', got %c", last[0].Rune)
	}
}

func TestPrependScrollbackWithANSI(t *testing.T) {
	vt := New(80, 24)

	// Bold red text: ESC[1;31m
	vt.PrependScrollback([]byte("\x1b[1;31mred bold\x1b[0m\n"))

	if len(vt.Scrollback) == 0 {
		t.Fatal("expected scrollback lines")
	}

	cell := vt.Scrollback[0][0]
	if cell.Rune != 'r' {
		t.Errorf("expected 'r', got %c", cell.Rune)
	}
	if !cell.Style.Bold {
		t.Error("expected bold style")
	}
	if cell.Style.Fg.Type != ColorIndexed || cell.Style.Fg.Value != 1 {
		t.Errorf("expected red foreground, got type=%v value=%d", cell.Style.Fg.Type, cell.Style.Fg.Value)
	}
}

func TestPrependScrollbackTrimsTrailingBlankLines(t *testing.T) {
	vt := New(80, 5)

	// One line of text followed by nothing -- the temp vterm will have
	// that text on screen row 0 and blank rows 1-4. Those trailing blanks
	// should be trimmed.
	vt.PrependScrollback([]byte("only line"))

	if len(vt.Scrollback) != 1 {
		t.Fatalf("expected 1 scrollback line, got %d", len(vt.Scrollback))
	}
	if vt.Scrollback[0][0].Rune != 'o' {
		t.Errorf("expected 'o', got %c", vt.Scrollback[0][0].Rune)
	}
}

func TestPrependScrollbackLargeContent(t *testing.T) {
	vt := New(80, 24)

	// Generate content that exceeds the screen height to produce scrollback
	// in the temporary vterm.
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, strings.Repeat("x", 80))
	}
	content := strings.Join(lines, "\n") + "\n"

	vt.PrependScrollback([]byte(content))

	if len(vt.Scrollback) == 0 {
		t.Fatal("expected scrollback lines from large content")
	}
	// Should have captured all 50 lines (some in scrollback, some on screen)
	if len(vt.Scrollback) < 50 {
		t.Errorf("expected at least 50 scrollback lines, got %d", len(vt.Scrollback))
	}
}

func TestPrependScrollbackRespectsMaxScrollback(t *testing.T) {
	vt := New(80, 24)

	// Fill existing scrollback close to max
	for i := 0; i < MaxScrollback-5; i++ {
		vt.Scrollback = append(vt.Scrollback, MakeBlankLine(80))
	}

	// Prepend 100 more lines
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "line")
	}
	vt.PrependScrollback([]byte(strings.Join(lines, "\n") + "\n"))

	if len(vt.Scrollback) > MaxScrollback {
		t.Errorf("scrollback exceeded max: %d > %d", len(vt.Scrollback), MaxScrollback)
	}
}

func TestPrependScrollbackAllBlankContent(t *testing.T) {
	vt := New(80, 24)
	// Feed whitespace-only content -- should be a no-op
	vt.PrependScrollback([]byte("   \n   \n"))
	// The spaces are non-blank (they're space runes), so this actually
	// produces lines. But pure empty lines (just newlines) produce blank
	// screen rows that get trimmed.
	// Feed truly empty content
	vt2 := New(80, 24)
	vt2.PrependScrollback([]byte("\n\n\n"))
	// All lines are blank newlines -> screen rows are blank -> trimmed
	if len(vt2.Scrollback) != 0 {
		t.Errorf("expected 0 scrollback lines for all-blank content, got %d", len(vt2.Scrollback))
	}
}

func TestIsBlankLine(t *testing.T) {
	blank := MakeBlankLine(10)
	if !isBlankLine(blank) {
		t.Error("MakeBlankLine should produce a blank line")
	}

	nonBlank := MakeBlankLine(10)
	nonBlank[5] = Cell{Rune: 'x', Width: 1}
	if isBlankLine(nonBlank) {
		t.Error("line with 'x' should not be blank")
	}

	// Null rune cells should be considered blank
	nullLine := make([]Cell, 10)
	if !isBlankLine(nullLine) {
		t.Error("line with null runes should be blank")
	}
}
