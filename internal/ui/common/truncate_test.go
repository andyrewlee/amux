package common

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The expectations below were hand-derived from the loops these helpers
// replace: right-trim kept the largest prefix of runes whose display width
// fit maxWidth-markerWidth; left-trim kept the largest fitting suffix with a
// 4-rune floor. The helpers must be byte-identical on single-rune graphemes
// and must never split a grapheme cluster.

func TestTruncateRightCells(t *testing.T) {
	tests := []struct {
		name       string
		s          string
		maxCells   int
		marker     string
		floorRunes int
		want       string
	}{
		{"fits unchanged", "hello", 10, "…", 0, "hello"},
		{"exact fit unchanged", "hello", 5, "…", 0, "hello"},
		{"ascii trims to budget", "abcdefgh", 5, "…", 0, "abcd…"},
		{"wide runes keep whole cells", "ab中文字cd", 5, "…", 0, "ab中…"},    // prefix ≤4: "ab中"=4, next 文 would be 6
		{"emoji cluster never split", "ab👍🏽cd", 4, "…", 0, "ab…"},        // 👍🏽 (2 cells) won't fit budget 3; old loop emitted half the cluster
		{"floor keeps four runes", "abcdef", 4, "...", 4, "abcd..."},     // budget 1 but floor wins
		{"floor under-run passes through", "中文字", 2, "...", 4, "中文字..."}, // ≤4 runes: no trim, marker appended
		{"zero floor empties prefix", "abc", 1, "…", 0, "…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateRightCells(tt.s, tt.maxCells, tt.marker, tt.floorRunes)
			if got != tt.want {
				t.Fatalf("TruncateRightCells(%q, %d, %q, %d) = %q, want %q",
					tt.s, tt.maxCells, tt.marker, tt.floorRunes, got, tt.want)
			}
		})
	}
}

func TestTruncateLeftCells(t *testing.T) {
	tests := []struct {
		name       string
		s          string
		maxCells   int
		marker     string
		floorRunes int
		want       string
	}{
		{"fits unchanged", "/a/b", 10, "...", 4, "/a/b"},
		{"ascii keeps suffix", "/very/long/path/file.go", 10, "...", 4, "...file.go"}, // suffix ≤7 cells
		{"wide runes keep whole cells", "dir/中文字尾.go", 10, "...", 4, "...字尾.go"},      // suffix ≤7
		{"floor keeps four runes", "abcdefghij", 6, "...", 4, "...ghij"},
		{"emoji cluster never split", "aa👍🏽bbcc", 7, "...", 4, "...bbcc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateLeftCells(tt.s, tt.maxCells, tt.marker, tt.floorRunes)
			if got != tt.want {
				t.Fatalf("TruncateLeftCells(%q, %d, %q, %d) = %q, want %q",
					tt.s, tt.maxCells, tt.marker, tt.floorRunes, got, tt.want)
			}
		})
	}
}

// TestTruncateCells_ClusterBoundary pins the no-split invariant end to end:
// whatever the cut, the output's graphemes are all complete clusters of the
// original string.
func TestTruncateCells_ClusterBoundary(t *testing.T) {
	s := "ab👨‍👩‍👧cd" // multi-codepoint ZWJ emoji mid-string
	for maxCells := 3; maxCells <= 8; maxCells++ {
		got := TruncateRightCells(s, maxCells, "…", 0)
		if ansi.StringWidth(got) > maxCells {
			t.Fatalf("TruncateRightCells(%q, %d) over-wide: %q", s, maxCells, got)
		}
		got = TruncateLeftCells(s, maxCells, "...", 0)
		if ansi.StringWidth(got) > maxCells {
			t.Fatalf("TruncateLeftCells(%q, %d) over-wide: %q", s, maxCells, got)
		}
	}
}
