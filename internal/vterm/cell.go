package vterm

// Color represents a terminal color
type Color struct {
	Type  ColorType
	Value uint32 // Indexed: 0-255, RGB: 0xRRGGBB
}

type ColorType uint8

const (
	ColorDefault ColorType = iota
	ColorIndexed
	ColorRGB
)

// Style holds text styling attributes
type Style struct {
	Fg        Color
	Bg        Color
	Bold      bool
	Dim       bool
	Italic    bool
	Underline bool
	Blink     bool
	Reverse   bool
	Hidden    bool
	Strike    bool
}

// Cell represents a single character cell
type Cell struct {
	Rune  rune
	Style Style
	Width int // 1 normal, 2 wide, 0 continuation
	// GraphemeCluster, when non-empty, is the full grapheme (base rune plus
	// combining marks) for this cell. Empty means "use Rune". Readers that emit
	// text should prefer it; width/layout logic still uses Rune + Width.
	GraphemeCluster string
}

// DefaultCell returns a blank cell
func DefaultCell() Cell {
	return Cell{Rune: ' ', Width: 1}
}

// IsWideContinuation reports whether row[x] is the second column of a wide
// glyph whose base sits at x-1.
//
// Every renderer emits nothing for a zero-width cell, on the assumption that
// the wide glyph before it already covered both columns. A zero-width cell that
// fails this test is an orphan (a half-erased glyph), and skipping it pulls the
// rest of the line one column to the left — so renderers must draw a blank
// there instead. normalizeLine uses the same rule to keep orphans out of the
// buffer in the first place.
func IsWideContinuation(row []Cell, x int) bool {
	return x > 0 && x < len(row) && row[x].Width == 0 && row[x-1].Width == 2
}

// HasWideContinuation reports whether the wide glyph based at row[x] still owns
// its second column within visibleWidth.
//
// This is the mirror of IsWideContinuation, and renderers need both. A wide
// base draws two columns but is followed by a cell that draws one, so a base
// that has lost its continuation — to a narrow overwrite, or to a resize that
// pushed the second column out of view — makes the line render one column too
// WIDE, drifting the rest of it right. Renderers must substitute a blank for
// such a base. normalizeLine enforces the same rule on the write side, against
// len(line) rather than a viewport width.
func HasWideContinuation(row []Cell, x, visibleWidth int) bool {
	return x >= 0 && x+1 < visibleWidth && x+1 < len(row) && row[x+1].Width == 0
}

// MakeBlankLine creates a blank line
func MakeBlankLine(width int) []Cell {
	line := make([]Cell, width)
	for i := range line {
		line[i] = DefaultCell()
	}
	return line
}

// CopyLine deep copies a line
func CopyLine(src []Cell) []Cell {
	dst := make([]Cell, len(src))
	copy(dst, src)
	return dst
}
