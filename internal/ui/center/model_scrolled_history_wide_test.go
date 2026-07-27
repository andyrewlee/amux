package center

import (
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/ui/compositor"
	"github.com/andyrewlee/amux/internal/vterm"
)

// wideHistoryLine builds a scrollback line holding a wide glyph followed by
// narrow text, as a streaming agent that prints emoji would produce.
func wideHistoryLine(width int) []vterm.Cell {
	line := vterm.MakeBlankLine(width)
	line[0] = vterm.Cell{Rune: '✅', Width: 2}
	line[1] = vterm.Cell{Width: 0}
	line[2] = vterm.Cell{Rune: 'o', Width: 1}
	line[3] = vterm.Cell{Rune: 'k', Width: 1}
	return line
}

// A chat tab scrolled into history renders a scrollback-only window
// (applyScrolledChatHistoryViewLocked). This is the exact path a user is on
// while scrolling up through agent output, so pin that a wide glyph in that
// window keeps both its glyph and its column alignment through the real
// snapshot -> layer -> canvas pipeline.
func TestScrolledChatHistoryPreservesWideGlyphColumns(t *testing.T) {
	m, tab := setupScrolledChatHistoryModel()
	term := tab.Terminal
	term.Scrollback[0] = wideHistoryLine(term.Width)

	term.ScrollView(1)
	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}

	row := layer.Snap.Screen[0]
	if row[0].Rune != '✅' || row[0].Width != 2 {
		t.Fatalf("wide glyph lost in scrolled history snapshot: %+v", row[0])
	}
	if row[1].Width != 0 {
		t.Fatalf("continuation cell lost in scrolled history snapshot: %+v", row[1])
	}

	// Through the canvas renderer the row must still occupy exactly Width
	// columns, with the glyph intact and the narrow text after it in place.
	out := compositor.RenderSnapshotWithCanvas(
		nil, layer.Snap, layer.Snap.Width, layer.Snap.Height,
		vterm.Color{Type: vterm.ColorDefault}, vterm.Color{Type: vterm.ColorDefault},
	)
	first := ansi.Strip(out)
	if idx := indexByte(first, '\n'); idx >= 0 {
		first = first[:idx]
	}
	if got, want := ansi.StringWidth(first), term.Width; got != want {
		t.Fatalf("scrolled history row width = %d, want %d (%q)", got, want, first)
	}
	if want := "✅ok"; !hasPrefix(first, want) {
		t.Fatalf("scrolled history row = %q, want it to start with %q", first, want)
	}
}

// The same scrolled-history window, but the history line holds a wide base
// whose continuation was overwritten (a half-erased glyph). It must render as a
// blank occupying one column, not as two columns that push the rest right.
func TestScrolledChatHistoryBlanksWidowedWideBase(t *testing.T) {
	m, tab := setupScrolledChatHistoryModel()
	term := tab.Terminal

	line := vterm.MakeBlankLine(term.Width)
	line[0] = vterm.Cell{Rune: '✅', Width: 2}
	line[1] = vterm.Cell{Rune: 'o', Width: 1} // continuation overwritten
	line[2] = vterm.Cell{Rune: 'k', Width: 1}
	term.Scrollback[0] = line

	term.ScrollView(1)
	layer := m.TerminalLayer()
	if layer == nil || layer.Snap == nil {
		t.Fatal("expected terminal layer snapshot")
	}

	out := compositor.RenderSnapshotWithCanvas(
		nil, layer.Snap, layer.Snap.Width, layer.Snap.Height,
		vterm.Color{Type: vterm.ColorDefault}, vterm.Color{Type: vterm.ColorDefault},
	)
	first := ansi.Strip(out)
	if idx := indexByte(first, '\n'); idx >= 0 {
		first = first[:idx]
	}
	if got, want := ansi.StringWidth(first), term.Width; got != want {
		t.Fatalf("row width = %d, want %d — widowed base drew two columns (%q)", got, want, first)
	}
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
