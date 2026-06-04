// Package compositor composes vterm snapshots and UI layers into a single
// rendered frame. A Canvas holds the working cell grid and is reused across
// frames; snapshot rendering (RenderSnapshotWithCanvas) and delta-aware ANSI
// emission keep redraws cheap on the hot render path.
package compositor

import (
	"strconv"
	"strings"

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

// CursorState holds cursor position and visibility for DrawScreen.
type CursorState struct {
	X, Y    int
	Visible bool
}

// SelectionRegion holds selection bounds for DrawScreen.
type SelectionRegion struct {
	Active         bool
	StartX, StartY int
	EndX, EndY     int
}

// DrawScreen draws a vterm screen into the canvas with clipping.
func (c *Canvas) DrawScreen(x, y, w, h int, screen [][]vterm.Cell, cursor CursorState, viewOffset int, selection SelectionRegion) {
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
			if inSelection(selection, col, row) {
				cell.Style.Reverse = !cell.Style.Reverse
			}
			targetX := x + col
			targetY := y + row
			if targetX < 0 || targetY < 0 || targetX >= c.Width || targetY >= c.Height {
				continue
			}
			c.SetCell(targetX, targetY, cell)
		}
	}

	if cursor.Visible && viewOffset == 0 {
		if cursor.X >= 0 && cursor.Y >= 0 && cursor.X < w && cursor.Y < h {
			targetX := x + cursor.X
			targetY := y + cursor.Y
			if targetX >= 0 && targetX < c.Width && targetY >= 0 && targetY < c.Height {
				cell := c.Cells[targetY][targetX]
				cell.Style.Reverse = !cell.Style.Reverse
				c.Cells[targetY][targetX] = cell
			}
		}
	}
}

func inSelection(sel SelectionRegion, x, y int) bool {
	if !sel.Active {
		return false
	}

	startX, startY := sel.StartX, sel.StartY
	endX, endY := sel.EndX, sel.EndY

	if startY > endY || (startY == endY && startX > endX) {
		startX, endX = endX, startX
		startY, endY = endY, startY
	}

	if y < startY || y > endY {
		return false
	}
	if y == startY && y == endY {
		return x >= startX && x <= endX
	}
	if y == startY {
		return x >= startX
	}
	if y == endY {
		return x <= endX
	}
	return true
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

// RenderSnapshotWithCanvas renders a vterm snapshot into a reusable canvas.
func RenderSnapshotWithCanvas(canvas *Canvas, snap *VTermSnapshot, width, height int, fg, bg vterm.Color) string {
	if snap == nil {
		return ""
	}
	if width <= 0 || width > snap.Width {
		width = snap.Width
	}
	if height <= 0 || height > snap.Height {
		height = snap.Height
	}
	if width <= 0 || height <= 0 {
		return ""
	}

	if canvas == nil {
		canvas = NewCanvas(width, height)
	} else {
		canvas.Resize(width, height)
	}
	canvas.Fill(vterm.Style{Fg: fg, Bg: bg})
	canvas.DrawScreen(
		0,
		0,
		width,
		height,
		snap.Screen,
		CursorState{
			X:       snap.CursorX,
			Y:       snap.CursorY,
			Visible: snap.ShowCursor && !snap.CursorHidden,
		},
		snap.ViewOffset,
		SelectionRegion{
			Active: snap.SelActive,
			StartX: snap.SelStartX,
			StartY: snap.SelStartY,
			EndX:   snap.SelEndX,
			EndY:   snap.SelEndY,
		},
	)
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
