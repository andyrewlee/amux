package vterm

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A wide glyph draws two columns. If it loses its continuation cell it still
// draws two while the buffer owns one, so the line renders one column too wide
// and everything after it shifts right — the mirror of the orphan bug.
func TestRenderBlanksWidowedWideBase(t *testing.T) {
	t.Run("continuation pushed out of view by resize", func(t *testing.T) {
		// resizeRows keeps rows wider than the viewport, so after shrinking the
		// wide base sits in the last visible column with its continuation just
		// outside it.
		v := New(8, 1)
		v.Write([]byte("abc中de"))
		v.Resize(4, 1)

		out := ansi.Strip(v.Render())
		if got, want := ansi.StringWidth(out), v.Width; got != want {
			t.Fatalf("rendered width = %d, want %d (%q)", got, want, out)
		}
	})

	t.Run("scrollback line wider than the viewport", func(t *testing.T) {
		v := New(4, 2)
		v.PrependScrollbackWithSize([]byte("abc中de\nqqqq"), 8, 2)
		v.ScrollViewTo(2)
		if v.ViewOffset == 0 {
			t.Fatalf("setup: expected a scrolled view")
		}

		out := ansi.Strip(v.Render())
		for i, line := range splitLines(out) {
			if got, want := ansi.StringWidth(line), v.Width; got != want {
				t.Fatalf("scrolled line %d width = %d, want %d (%q)", i, got, want, line)
			}
		}
	})

	t.Run("continuation overwritten mid-line", func(t *testing.T) {
		// A wide glyph at the last two columns, then another wide glyph starting
		// in the final column: the second one wraps, and the space it leaves
		// behind lands on the first one's continuation cell.
		v := New(12, 2)
		v.Write([]byte("\x1b[1;11H中"))
		v.Write([]byte("\x1b[1;12H中"))

		if v.Screen[0][10].Width == 2 && v.Screen[0][11].Width != 0 {
			t.Fatalf("putChar left a widowed wide base at column 10: %+v", v.Screen[0][10])
		}
		for i, line := range splitLines(ansi.Strip(v.Render())) {
			if got, want := ansi.StringWidth(line), v.Width; got != want {
				t.Fatalf("line %d width = %d, want %d (%q)", i, got, want, line)
			}
		}
	})
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func TestHasWideContinuation(t *testing.T) {
	pair := []Cell{{Rune: '中', Width: 2}, {Width: 0}, {Rune: 'a', Width: 1}}
	widowed := []Cell{{Rune: '中', Width: 2}, {Rune: 'a', Width: 1}, {Rune: 'b', Width: 1}}

	tests := []struct {
		name         string
		row          []Cell
		x            int
		visibleWidth int
		want         bool
	}{
		{name: "intact pair", row: pair, x: 0, visibleWidth: 3, want: true},
		{name: "continuation overwritten", row: widowed, x: 0, visibleWidth: 3, want: false},
		{name: "continuation outside the viewport", row: pair, x: 0, visibleWidth: 1, want: false},
		{name: "base in the last visible column", row: pair, x: 2, visibleWidth: 3, want: false},
		{name: "index past the row", row: pair, x: len(pair), visibleWidth: 9, want: false},
		{name: "negative index", row: pair, x: -1, visibleWidth: 3, want: false},
		{name: "empty row", row: nil, x: 0, visibleWidth: 3, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasWideContinuation(tc.row, tc.x, tc.visibleWidth); got != tc.want {
				t.Fatalf("HasWideContinuation(x=%d, w=%d) = %v, want %v",
					tc.x, tc.visibleWidth, got, tc.want)
			}
		})
	}
}
