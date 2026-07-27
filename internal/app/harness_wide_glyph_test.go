package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestCenterPaneKeepsWideGlyphColumns renders a real composed frame containing a
// wide glyph and asserts the glyph survives and the text after it keeps its
// column. The compositor used to write an explicit zero-width continuation cell,
// which made ultraviolet blank the wide base and drop the placeholder from the
// output — every cell after a wide glyph then drew one column to the left.
func TestCenterPaneKeepsWideGlyphColumns(t *testing.T) {
	h, err := NewHarness(HarnessOptions{
		Mode:    HarnessCenter,
		Tabs:    1,
		Width:   120,
		Height:  36,
		HotTabs: 0,
	})
	if err != nil {
		t.Fatalf("harness init: %v", err)
	}
	if len(h.tabs) == 0 || h.tabs[0] == nil {
		t.Fatalf("harness produced no center tabs")
	}

	const marker = "AB✅CD"
	h.tabs[0].WriteToTerminal([]byte("\x1b[2J\x1b[H" + marker))

	content := h.Render().Content
	if content == "" {
		t.Fatalf("rendered frame is empty")
	}

	lines := strings.Split(content, "\n")
	var found string
	for _, line := range lines {
		// Match on the marker's narrow halves so the row is still found when
		// the wide glyph between them is dropped (the bug being guarded).
		if plain := ansi.Strip(line); strings.Contains(plain, "AB") && strings.Contains(plain, "CD") {
			found = plain
			break
		}
	}
	if found == "" {
		t.Fatalf("rendered frame has no line containing the marker")
	}
	if !strings.Contains(found, marker) {
		t.Fatalf("wide glyph lost or line collapsed: got %q, want a line containing %q", found, marker)
	}

	// Every row of the frame is the same display width; a dropped zero-width
	// cell shortens the glyph's row by exactly one column, which is what pulls
	// the rest of the line left on screen.
	want := ansi.StringWidth(ansi.Strip(lines[0]))
	if got := ansi.StringWidth(found); got != want {
		t.Fatalf("glyph row width = %d, want %d (other rows) — line: %q", got, want, found)
	}
}
