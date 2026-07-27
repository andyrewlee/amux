package vterm

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func orphanLine() []Cell {
	return []Cell{
		{Rune: 'a', Width: 1},
		{Width: 0}, // zero-width cell with no wide glyph before it
		{Rune: 'b', Width: 1},
		{Rune: 'c', Width: 1},
	}
}

func widePairLine() []Cell {
	return []Cell{
		{Rune: '中', Width: 2},
		{Width: 0},
		{Rune: 'b', Width: 1},
		{Rune: 'c', Width: 1},
	}
}

// Renderers emit nothing for a zero-width cell. That is correct for the second
// column of a wide glyph and wrong for an orphan, which must occupy its column
// or the rest of the line renders one column too far left.
func TestRenderBlanksOrphanContinuation(t *testing.T) {
	t.Run("live screen", func(t *testing.T) {
		term := New(4, 1)
		term.Screen[0] = orphanLine()
		term.invalidateRenderCache()

		out := ansi.Strip(term.Render())
		if got, want := ansi.StringWidth(out), 4; got != want {
			t.Fatalf("rendered width = %d, want %d (%q)", got, want, out)
		}
		if want := "a bc"; out != want {
			t.Fatalf("rendered %q, want %q", out, want)
		}
	})

	t.Run("scrollback view", func(t *testing.T) {
		term := New(4, 1)
		term.Scrollback = [][]Cell{orphanLine()}
		term.ScrollView(1)
		if term.ViewOffset == 0 {
			t.Fatalf("setup: expected a scrolled view")
		}

		out := ansi.Strip(term.Render())
		if got, want := ansi.StringWidth(out), 4; got != want {
			t.Fatalf("rendered width = %d, want %d (%q)", got, want, out)
		}
		if want := "a bc"; out != want {
			t.Fatalf("rendered %q, want %q", out, want)
		}
	})
}

// The blank substitution must not touch a legitimate wide pair, which really
// does occupy two columns with one emitted glyph.
func TestRenderKeepsWidePair(t *testing.T) {
	t.Run("live screen", func(t *testing.T) {
		term := New(4, 1)
		term.Screen[0] = widePairLine()
		term.invalidateRenderCache()

		if got, want := ansi.Strip(term.Render()), "中bc"; got != want {
			t.Fatalf("rendered %q, want %q", got, want)
		}
	})

	t.Run("scrollback view", func(t *testing.T) {
		term := New(4, 1)
		term.Scrollback = [][]Cell{widePairLine()}
		term.ScrollView(1)

		if got, want := ansi.Strip(term.Render()), "中bc"; got != want {
			t.Fatalf("rendered %q, want %q", got, want)
		}
	})
}

func TestIsWideContinuation(t *testing.T) {
	pair := widePairLine()
	orphan := orphanLine()

	tests := []struct {
		name string
		row  []Cell
		x    int
		want bool
	}{
		{name: "continuation of a wide glyph", row: pair, x: 1, want: true},
		{name: "the wide base itself", row: pair, x: 0, want: false},
		{name: "narrow cell", row: pair, x: 2, want: false},
		{name: "orphan after a narrow cell", row: orphan, x: 1, want: false},
		{name: "zero-width cell at column 0", row: []Cell{{Width: 0}}, x: 0, want: false},
		{name: "index past the row", row: pair, x: len(pair), want: false},
		{name: "negative index", row: pair, x: -1, want: false},
		{name: "empty row", row: nil, x: 1, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsWideContinuation(tc.row, tc.x); got != tc.want {
				t.Fatalf("IsWideContinuation(%v) = %v, want %v", tc.x, got, tc.want)
			}
		})
	}
}
