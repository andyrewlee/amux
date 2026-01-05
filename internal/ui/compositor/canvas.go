package compositor

import (
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/andyrewlee/amux/internal/vterm"
)

// Canvas is a fixed-size buffer of styled cells.
type Canvas struct {
	Width  int
	Height int
	Cells  [][]vterm.Cell

	// renderBuffers keep two frames alive to avoid reallocations while preserving
	// the previous render output for diffing.
	renderBuffers    [2]strings.Builder
	renderBufferNext int
}

// NewCanvas creates a new canvas filled with blank cells.
func NewCanvas(width, height int) *Canvas {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	rows := make([][]vterm.Cell, height)
	for y := range rows {
		rows[y] = vterm.MakeBlankLine(width)
	}

	return &Canvas{
		Width:  width,
		Height: height,
		Cells:  rows,
	}
}

// Resize resets the canvas dimensions when the size changes.
func (c *Canvas) Resize(width, height int) {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	if width == c.Width && height == c.Height {
		return
	}
	rows := make([][]vterm.Cell, height)
	for y := range rows {
		rows[y] = vterm.MakeBlankLine(width)
	}
	c.Width = width
	c.Height = height
	c.Cells = rows
}

// Fill sets the entire canvas to the given style.
func (c *Canvas) Fill(style vterm.Style) {
	for y := 0; y < c.Height; y++ {
		for x := 0; x < c.Width; x++ {
			cell := vterm.DefaultCell()
			cell.Style = style
			c.Cells[y][x] = cell
		}
	}
}

// SetCell sets a cell if within bounds.
func (c *Canvas) SetCell(x, y int, cell vterm.Cell) {
	if x < 0 || y < 0 || x >= c.Width || y >= c.Height {
		return
	}
	c.Cells[y][x] = cell
}

// DrawText draws a string starting at the given position.
func (c *Canvas) DrawText(x, y int, text string, style vterm.Style) {
	if y < 0 || y >= c.Height {
		return
	}

	col := x
	for _, r := range text {
		if col >= c.Width {
			break
		}
		width := runewidth.RuneWidth(r)
		if width <= 0 {
			continue
		}
		if col+width > c.Width {
			break
		}
		cell := vterm.Cell{Rune: r, Width: width, Style: style}
		c.SetCell(col, y, cell)
		if width == 2 {
			c.SetCell(col+1, y, vterm.Cell{Width: 0, Style: style})
		}
		col += width
	}
}

// DrawBorder draws a single or double line border.
func (c *Canvas) DrawBorder(x, y, w, h int, style vterm.Style, focused bool) {
	if w < 2 || h < 2 {
		return
	}

	var tl, tr, bl, br, hline, vline rune
	if focused {
		tl, tr, bl, br = '╔', '╗', '╚', '╝'
		hline, vline = '═', '║'
	} else {
		tl, tr, bl, br = '┌', '┐', '└', '┘'
		hline, vline = '─', '│'
	}

	// Corners
	c.SetCell(x, y, vterm.Cell{Rune: tl, Width: 1, Style: style})
	c.SetCell(x+w-1, y, vterm.Cell{Rune: tr, Width: 1, Style: style})
	c.SetCell(x, y+h-1, vterm.Cell{Rune: bl, Width: 1, Style: style})
	c.SetCell(x+w-1, y+h-1, vterm.Cell{Rune: br, Width: 1, Style: style})

	// Horizontal lines
	for cx := x + 1; cx < x+w-1; cx++ {
		c.SetCell(cx, y, vterm.Cell{Rune: hline, Width: 1, Style: style})
		c.SetCell(cx, y+h-1, vterm.Cell{Rune: hline, Width: 1, Style: style})
	}

	// Vertical lines
	for cy := y + 1; cy < y+h-1; cy++ {
		c.SetCell(x, cy, vterm.Cell{Rune: vline, Width: 1, Style: style})
		c.SetCell(x+w-1, cy, vterm.Cell{Rune: vline, Width: 1, Style: style})
	}
}

// DrawScreen draws a vterm screen into the canvas with clipping.
func (c *Canvas) DrawScreen(x, y, w, h int, screen [][]vterm.Cell, cursorX, cursorY int, showCursor bool, viewOffset int) {
	if w <= 0 || h <= 0 {
		return
	}
	maxY := min(h, len(screen))
	for row := 0; row < maxY; row++ {
		line := screen[row]
		maxX := min(w, len(line))
		for col := 0; col < maxX; col++ {
			cell := line[col]
			if cell.Width == 2 && col+1 >= w {
				cell = vterm.DefaultCell()
			}
			targetX := x + col
			targetY := y + row
			if targetX < 0 || targetY < 0 || targetX >= c.Width || targetY >= c.Height {
				continue
			}
			c.SetCell(targetX, targetY, cell)
		}
	}

	if showCursor && viewOffset == 0 {
		if cursorX >= 0 && cursorY >= 0 && cursorX < w && cursorY < h {
			targetX := x + cursorX
			targetY := y + cursorY
			if targetX >= 0 && targetX < c.Width && targetY >= 0 && targetY < c.Height {
				cell := c.Cells[targetY][targetX]
				cell.Style.Reverse = !cell.Style.Reverse
				c.Cells[targetY][targetX] = cell
			}
		}
	}
}

// Render converts the canvas to an ANSI string.
func (c *Canvas) Render() string {
	b := &c.renderBuffers[c.renderBufferNext]
	c.renderBufferNext = (c.renderBufferNext + 1) % len(c.renderBuffers)
	b.Reset()
	b.Grow(c.Width * c.Height * 2)

	for y := 0; y < c.Height; y++ {
		rowBlank := true
		for x := 0; x < c.Width; x++ {
			cell := c.Cells[y][x]
			if cell.Width == 0 {
				continue
			}
			if cell.Rune != 0 && cell.Rune != ' ' {
				rowBlank = false
				break
			}
		}
		// Reset per line.
		b.WriteString("\x1b[0m")
		var lastStyle vterm.Style
		for x := 0; x < c.Width; x++ {
			cell := c.Cells[y][x]
			if cell.Width == 0 {
				continue
			}
			style := cell.Style
			if rowBlank && style.Underline {
				style.Underline = false
			}
			if style != lastStyle {
				b.WriteString(vterm.StyleToANSI(style))
				lastStyle = style
			}
			r := cell.Rune
			if r == 0 {
				r = ' '
			}
			b.WriteRune(r)
		}
		if y < c.Height-1 {
			b.WriteRune('\n')
		}
	}

	b.WriteString("\x1b[0m")
	return b.String()
}

// RenderTerminal renders a vterm into a canvas and returns the ANSI string.
func RenderTerminal(term *vterm.VTerm, width, height int, showCursor bool, fg, bg vterm.Color) string {
	return RenderTerminalWithCanvas(nil, term, width, height, showCursor, fg, bg)
}

// RenderTerminalWithCanvas renders a vterm into a reusable canvas.
func RenderTerminalWithCanvas(canvas *Canvas, term *vterm.VTerm, width, height int, showCursor bool, fg, bg vterm.Color) string {
	if term == nil || width <= 0 || height <= 0 {
		return ""
	}

	if canvas == nil {
		canvas = NewCanvas(width, height)
	} else {
		canvas.Resize(width, height)
	}
	canvas.Fill(vterm.Style{Fg: fg, Bg: bg})
	screen := term.VisibleScreenWithSelection()
	canvas.DrawScreen(0, 0, width, height, screen, term.CursorX, term.CursorY, showCursor, term.ViewOffset)
	return canvas.Render()
}

// HexColor converts a #RRGGBB string to a vterm.Color.
func HexColor(hex string) vterm.Color {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return vterm.Color{Type: vterm.ColorDefault}
	}
	value, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return vterm.Color{Type: vterm.ColorDefault}
	}
	return vterm.Color{Type: vterm.ColorRGB, Value: uint32(value)}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
