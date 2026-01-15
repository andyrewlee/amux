package compositor

import (
	"strconv"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/andyrewlee/amux/internal/perf"
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
	defer perf.Time("compositor_render")()
	b := &c.renderBuffers[c.renderBufferNext]
	c.renderBufferNext = (c.renderBufferNext + 1) % len(c.renderBuffers)
	b.Reset()
	b.Grow(c.Width * c.Height * 2)

	for y := 0; y < c.Height; y++ {
		// Reset per line to make lines independent for caching.
		b.WriteString("\x1b[0m")
		var lastStyle vterm.Style
		firstCell := true
		for x := 0; x < c.Width; x++ {
			cell := c.Cells[y][x]
			if cell.Width == 0 {
				continue
			}
			style := cell.Style
			// Prevent underline-on-spaces from rendering as scanlines.
			if style.Underline && (cell.Rune == 0 || cell.Rune == ' ') {
				style.Underline = false
			}
			if style != lastStyle {
				// Use full style for first cell (after reset), delta for subsequent
				if firstCell {
					b.WriteString(vterm.StyleToANSI(style))
				} else {
					b.WriteString(vterm.StyleToDeltaANSI(lastStyle, style))
				}
				lastStyle = style
			}
			firstCell = false
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

// StringDrawable wraps a styled ANSI string to implement uv.Drawable.
// This allows string-based content to be composed onto a lipgloss.Canvas.
type StringDrawable struct {
	content string
	x, y    int
	width   int
	height  int
	lines   []string
}

// Ensure StringDrawable implements uv.Drawable
var _ uv.Drawable = (*StringDrawable)(nil)

// NewStringDrawable creates a drawable from a styled string at the given position.
func NewStringDrawable(content string, x, y int) *StringDrawable {
	lines := strings.Split(content, "\n")
	width := 0
	for _, line := range lines {
		if w := ansi.StringWidth(line); w > width {
			width = w
		}
	}
	return &StringDrawable{
		content: content,
		x:       x,
		y:       y,
		width:   width,
		height:  len(lines),
		lines:   lines,
	}
}

// Draw renders the string onto the screen buffer.
func (d *StringDrawable) Draw(screen uv.Screen, r uv.Rectangle) {
	if d.content == "" {
		return
	}

	p := ansi.GetParser()
	defer ansi.PutParser(p)

	for lineIdx, line := range d.lines {
		screenY := d.y + lineIdx
		if screenY < r.Min.Y || screenY >= r.Max.Y {
			continue
		}

		// Parse ANSI styled string and write cells
		screenX := d.x
		var style uv.Style
		var state byte

		for len(line) > 0 {
			seq, width, n, newState := ansi.DecodeSequence(line, state, p)
			if n == 0 {
				break
			}

			if width == 0 {
				// Control sequence - check for SGR
				cmd := ansi.Cmd(p.Command())
				if cmd.Final() == 'm' {
					style = applySGR(style, p.Params())
				}
			} else {
				// Printable grapheme
				if screenX >= r.Min.X && screenX < r.Max.X {
					screen.SetCell(screenX, screenY, &uv.Cell{
						Content: seq,
						Style:   style,
						Width:   width,
					})
				}
				screenX += width
			}

			line = line[n:]
			state = newState
		}
	}
}

// applySGR updates the style based on SGR parameters.
func applySGR(style uv.Style, params ansi.Params) uv.Style {
	if len(params) == 0 {
		return uv.Style{}
	}

	for i := 0; i < len(params); i++ {
		p, _, _ := params.Param(i, 0)
		switch {
		case p == 0:
			style = uv.Style{}
		case p == 1:
			style.Attrs |= uv.AttrBold
		case p == 2:
			style.Attrs |= uv.AttrFaint
		case p == 3:
			style.Attrs |= uv.AttrItalic
		case p == 4:
			style.Underline = uv.UnderlineSingle
		case p == 5:
			style.Attrs |= uv.AttrBlink
		case p == 7:
			style.Attrs |= uv.AttrReverse
		case p == 8:
			style.Attrs |= uv.AttrConceal
		case p == 9:
			style.Attrs |= uv.AttrStrikethrough
		case p == 22:
			style.Attrs &^= (uv.AttrBold | uv.AttrFaint)
		case p == 23:
			style.Attrs &^= uv.AttrItalic
		case p == 24:
			style.Underline = uv.UnderlineNone
		case p == 25:
			style.Attrs &^= uv.AttrBlink
		case p == 27:
			style.Attrs &^= uv.AttrReverse
		case p == 28:
			style.Attrs &^= uv.AttrConceal
		case p == 29:
			style.Attrs &^= uv.AttrStrikethrough
		case p >= 30 && p <= 37:
			style.Fg = ansiColor(p - 30)
		case p == 38:
			// Extended foreground color
			if i+2 < len(params) {
				mode, _, _ := params.Param(i+1, 0)
				if mode == 5 {
					idx, _, _ := params.Param(i+2, 0)
					style.Fg = ansiColor(idx)
					i += 2
				} else if mode == 2 && i+4 < len(params) {
					rv, _, _ := params.Param(i+2, 0)
					gv, _, _ := params.Param(i+3, 0)
					bv, _, _ := params.Param(i+4, 0)
					style.Fg = rgbColorVal{uint8(rv), uint8(gv), uint8(bv)}
					i += 4
				}
			}
		case p == 39:
			style.Fg = nil
		case p >= 40 && p <= 47:
			style.Bg = ansiColor(p - 40)
		case p == 48:
			// Extended background color
			if i+2 < len(params) {
				mode, _, _ := params.Param(i+1, 0)
				if mode == 5 {
					idx, _, _ := params.Param(i+2, 0)
					style.Bg = ansiColor(idx)
					i += 2
				} else if mode == 2 && i+4 < len(params) {
					rv, _, _ := params.Param(i+2, 0)
					gv, _, _ := params.Param(i+3, 0)
					bv, _, _ := params.Param(i+4, 0)
					style.Bg = rgbColorVal{uint8(rv), uint8(gv), uint8(bv)}
					i += 4
				}
			}
		case p == 49:
			style.Bg = nil
		case p >= 90 && p <= 97:
			style.Fg = ansiColor(p - 90 + 8)
		case p >= 100 && p <= 107:
			style.Bg = ansiColor(p - 100 + 8)
		}
	}
	return style
}

// rgbColorVal is an RGB color value.
type rgbColorVal [3]uint8

func (c rgbColorVal) RGBA() (r, g, b, a uint32) {
	return uint32(c[0]) * 257, uint32(c[1]) * 257, uint32(c[2]) * 257, 65535
}
