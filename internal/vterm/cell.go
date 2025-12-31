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
}

// DefaultCell returns a blank cell
func DefaultCell() Cell {
	return Cell{Rune: ' ', Width: 1}
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
