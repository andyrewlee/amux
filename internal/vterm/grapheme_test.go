package vterm

import (
	"strings"
	"testing"
)

// TestGraphemeClusters verifies that combining marks are stored in
// Cell.GraphemeCluster, that the base Rune and Width are unaffected, and that
// selection copy returns the full cluster.
func TestGraphemeClusters(t *testing.T) {
	t.Parallel()

	// Expected cluster strings built numerically to avoid source-encoding
	// ambiguity.
	eAcute := string([]rune{'e', 0x0301})          // e + combining acute
	aDouble := string([]rune{'a', 0x0301, 0x0302}) // a + acute + circumflex

	tests := []struct {
		name        string
		input       []rune
		wantRune    rune
		wantWidth   int
		wantCluster string // "" means "GraphemeCluster must be empty"
		wantCursorX int    // cursor X after writing input
	}{
		{
			name:        "one combining mark stores cluster",
			input:       []rune{'e', 0x0301},
			wantRune:    'e',
			wantWidth:   1,
			wantCluster: eAcute,
			wantCursorX: 1,
		},
		{
			name:        "two combining marks accumulate in cluster",
			input:       []rune{'a', 0x0301, 0x0302},
			wantRune:    'a',
			wantWidth:   1,
			wantCluster: aDouble,
			wantCursorX: 1,
		},
		{
			name:        "no combining mark leaves GraphemeCluster empty",
			input:       []rune{'z'},
			wantRune:    'z',
			wantWidth:   1,
			wantCluster: "",
			wantCursorX: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			vt := New(20, 5)
			for _, r := range tc.input {
				vt.putChar(r)
			}

			cell := vt.Screen[0][0]
			if cell.Rune != tc.wantRune {
				t.Errorf("cell.Rune = %q, want %q", cell.Rune, tc.wantRune)
			}
			if cell.Width != tc.wantWidth {
				t.Errorf("cell.Width = %d, want %d", cell.Width, tc.wantWidth)
			}
			if cell.GraphemeCluster != tc.wantCluster {
				t.Errorf("cell.GraphemeCluster = %q, want %q", cell.GraphemeCluster, tc.wantCluster)
			}
			if vt.CursorX != tc.wantCursorX {
				t.Errorf("CursorX = %d, want %d", vt.CursorX, tc.wantCursorX)
			}
		})
	}
}

// TestGraphemeSelectionCopy verifies that GetTextRange returns the full cluster
// rather than just the base rune.
func TestGraphemeSelectionCopy(t *testing.T) {
	t.Parallel()

	eAcute := string([]rune{'e', 0x0301})

	vt := New(20, 5)
	// Write 'e' followed by combining acute accent.
	vt.putChar('e')
	vt.putChar(0x0301)

	// Screen row 0 is at absolute line index 0 (no scrollback).
	got := vt.GetTextRange(0, 0, 0, 0)
	if got != eAcute {
		t.Errorf("GetTextRange = %q, want %q", got, eAcute)
	}
}

func TestGraphemeRenderUsesCluster(t *testing.T) {
	t.Parallel()

	eAcute := string([]rune{'e', 0x0301})
	vt := New(20, 5)
	vt.putChar('e')
	vt.putChar(0x0301)

	if got := vt.Render(); !strings.Contains(got, eAcute) {
		t.Fatalf("Render() = %q, want rendered grapheme cluster %q", got, eAcute)
	}
}

func TestGraphemeRenderUsesClusterInScrollback(t *testing.T) {
	t.Parallel()

	eAcute := string([]rune{'e', 0x0301})
	vt := New(20, 2)
	vt.Scrollback = [][]Cell{{
		{
			Rune:            'e',
			Width:           1,
			GraphemeCluster: eAcute,
		},
	}}
	vt.ViewOffset = 1

	if got := vt.Render(); !strings.Contains(got, eAcute) {
		t.Fatalf("Render() scrolled = %q, want rendered grapheme cluster %q", got, eAcute)
	}
}

func TestLinesEqualComparesGraphemeCluster(t *testing.T) {
	t.Parallel()

	plain := DefaultCell()
	plain.Rune = 'e'
	plain.Width = 1

	combined := plain
	combined.GraphemeCluster = string([]rune{'e', 0x0301})

	if linesEqual([]Cell{plain}, []Cell{combined}) {
		t.Fatal("linesEqual treated plain and combined grapheme cells as equal")
	}
}

// TestGraphemeCombiningAtScreenStart verifies that a combining mark with no
// valid previous cell (at column 0 with no prior row) does not panic and does
// not create a cluster.
func TestGraphemeCombiningAtScreenStart(t *testing.T) {
	t.Parallel()

	vt := New(20, 5)

	// Cursor starts at (0,0). Sending a combining mark first should not panic.
	// CursorX-1 == -1 and CursorY == 0, so prevY would not decrement (prevY > 0 is false).
	// The guard (prevX >= 0) prevents any write.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("putChar panicked with combining mark at screen start: %v", r)
		}
	}()

	vt.putChar(0x0301) // combining acute at position (0,0) — no prior cell

	// Cell at (0,0) must still be the zero/blank cell; no cluster should be set.
	cell := vt.Screen[0][0]
	if cell.GraphemeCluster != "" {
		t.Errorf("GraphemeCluster = %q after combining-at-start, want empty", cell.GraphemeCluster)
	}
}
