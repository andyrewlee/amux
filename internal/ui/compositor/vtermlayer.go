package compositor

import (
	"image/color"
	"sync"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/andyrewlee/amux/internal/vterm"
)

var asciiStrings [128]string

func init() {
	for i := 0; i < 128; i++ {
		asciiStrings[i] = string(rune(i))
	}
}

func runeToString(r rune) string {
	if r >= 0 && r < 128 {
		return asciiStrings[r]
	}
	return string(r)
}

var ansiPalette = []color.RGBA{
	{0, 0, 0, 255},       // 0: Black
	{205, 49, 49, 255},   // 1: Red
	{13, 188, 121, 255},  // 2: Green
	{229, 229, 16, 255},  // 3: Yellow
	{36, 114, 200, 255},  // 4: Blue
	{188, 63, 188, 255},  // 5: Magenta
	{17, 168, 205, 255},  // 6: Cyan
	{229, 229, 229, 255}, // 7: White
	{102, 102, 102, 255}, // 8: Bright Black
	{241, 76, 76, 255},   // 9: Bright Red
	{35, 209, 139, 255},  // 10: Bright Green
	{245, 245, 67, 255},  // 11: Bright Yellow
	{59, 142, 234, 255},  // 12: Bright Blue
	{214, 112, 214, 255}, // 13: Bright Magenta
	{41, 184, 219, 255},  // 14: Bright Cyan
	{255, 255, 255, 255}, // 15: Bright White
}

const uvStyleCacheMax = 4096

var (
	uvStyleCacheMu sync.RWMutex
	uvStyleCache   = make(map[vterm.Style]uv.Style, 256)
)

// VTermLayer implements tea.Layer for direct cell-based rendering of a VTerm snapshot.
// This uses a snapshot to avoid data races - the snapshot is created while holding
// the VTerm lock, and rendering happens without any locks.
type VTermLayer struct {
	Snap *VTermSnapshot
}

var continuationCell = uv.Cell{Content: "", Width: 0}

// Ensure VTermLayer implements uv.Drawable (which is compatible with tea.Layer)
var _ uv.Drawable = (*VTermLayer)(nil)

// NewVTermLayer creates a new layer from a VTerm snapshot.
func NewVTermLayer(snap *VTermSnapshot) *VTermLayer {
	return &VTermLayer{Snap: snap}
}

// Draw renders the VTerm snapshot directly to the screen buffer.
// This is the hot path - every optimization here matters.
func (l *VTermLayer) Draw(s uv.Screen, r uv.Rectangle) {
	l.DrawAt(s, r.Min.X, r.Min.Y, r.Dx(), r.Dy())
}

// DrawAt renders the VTerm snapshot at a specific position with given dimensions.
// This is the core rendering logic shared by VTermLayer and PositionedVTermLayer.
func (l *VTermLayer) DrawAt(s uv.Screen, posX, posY, maxWidth, maxHeight int) {
	snap := l.Snap
	if snap == nil || len(snap.Screen) == 0 {
		return
	}

	width := maxWidth
	height := maxHeight
	if width > snap.Width {
		width = snap.Width
	}
	if height > snap.Height {
		height = snap.Height
	}

	selActive := snap.SelActive
	selStartX, selStartY := snap.SelStartX, snap.SelStartY
	selEndX, selEndY := snap.SelEndX, snap.SelEndY
	if selActive && (selStartY > selEndY || (selStartY == selEndY && selStartX > selEndX)) {
		selStartX, selEndX = selEndX, selStartX
		selStartY, selEndY = selEndY, selStartY
	}
	cursorVisible := snap.ShowCursor && !snap.CursorHidden && snap.ViewOffset == 0
	cursorX, cursorY := snap.CursorX, snap.CursorY

	// When compositing layers, we must draw ALL cells every frame.
	// The dirty line optimization only works for single-layer rendering.
	// Ultraviolet's cell-level diffing handles the actual screen updates.
	for y := 0; y < height && y < len(snap.Screen); y++ {
		row := snap.Screen[y]
		if row == nil {
			continue
		}
		rowWidth := width
		if rowWidth > len(row) {
			rowWidth = len(row)
		}

		rowSelActive := false
		rowSelStart, rowSelEnd := 0, -1
		if selActive && y >= selStartY && y <= selEndY {
			rowSelActive = true
			switch {
			case y == selStartY && y == selEndY:
				rowSelStart, rowSelEnd = selStartX, selEndX
			case y == selStartY:
				rowSelStart, rowSelEnd = selStartX, rowWidth-1
			case y == selEndY:
				rowSelStart, rowSelEnd = 0, selEndX
			default:
				rowSelStart, rowSelEnd = 0, rowWidth-1
			}
			if rowSelStart < 0 {
				rowSelStart = 0
			}
			if rowSelEnd >= rowWidth {
				rowSelEnd = rowWidth - 1
			}
			if rowSelEnd < rowSelStart {
				rowSelActive = false
			}
		}

		rowHasCursor := cursorVisible && y == cursorY
		var lastStyle vterm.Style
		var lastUVStyle uv.Style
		haveStyle := false

		for x := 0; x < rowWidth; x++ {
			cell := row[x]

			// For continuation cells (part of wide character), write an empty cell
			// to clear any stale content at that position from previous renders.
			if cell.Width == 0 {
				s.SetCell(posX+x, posY+y, &continuationCell)
				continue
			}

			style := cell.Style
			inSel := rowSelActive && x >= rowSelStart && x <= rowSelEnd
			cursorHere := rowHasCursor && x == cursorX
			if inSel || cursorHere {
				style.Reverse = !style.Reverse
			}

			r := cell.Rune
			if r == 0 {
				r = ' '
			}
			// Suppress underline on blank cells (prevents visual scanlines).
			if style.Underline && r == ' ' {
				style.Underline = false
			}

			if !haveStyle || style != lastStyle {
				lastStyle = style
				lastUVStyle = vtermStyleToUV(style)
				haveStyle = true
			}

			uvCell := uv.Cell{
				Content: runeToString(r),
				Style:   lastUVStyle,
				Width:   cell.Width,
			}

			// Set cell at screen position (ultraviolet copies the value)
			s.SetCell(posX+x, posY+y, &uvCell)
		}
	}
}

// vtermStyleToUV converts a vterm.Style to ultraviolet's Style.
func vtermStyleToUV(s vterm.Style) uv.Style {
	uvStyleCacheMu.RLock()
	cached, ok := uvStyleCache[s]
	uvStyleCacheMu.RUnlock()
	if ok {
		return cached
	}

	var uvStyle uv.Style

	// Convert colors
	uvStyle.Fg = vtermColorToUV(s.Fg)
	uvStyle.Bg = vtermColorToUV(s.Bg)

	// Convert attributes
	var attrs uint8
	if s.Bold {
		attrs |= uv.AttrBold
	}
	if s.Dim {
		attrs |= uv.AttrFaint
	}
	if s.Italic {
		attrs |= uv.AttrItalic
	}
	if s.Blink {
		attrs |= uv.AttrBlink
	}
	if s.Reverse {
		attrs |= uv.AttrReverse
	}
	if s.Hidden {
		attrs |= uv.AttrConceal
	}
	if s.Strike {
		attrs |= uv.AttrStrikethrough
	}
	uvStyle.Attrs = attrs

	// Handle underline
	if s.Underline {
		uvStyle.Underline = uv.UnderlineSingle
	}

	uvStyleCacheMu.Lock()
	if len(uvStyleCache) >= uvStyleCacheMax {
		clear(uvStyleCache)
	}
	uvStyleCache[s] = uvStyle
	uvStyleCacheMu.Unlock()

	return uvStyle
}

// vtermColorToUV converts a vterm.Color to a color.Color for ultraviolet.
func vtermColorToUV(c vterm.Color) color.Color {
	switch c.Type {
	case vterm.ColorDefault:
		return nil
	case vterm.ColorIndexed:
		// Use ANSI indexed colors
		return ansiColor(c.Value)
	case vterm.ColorRGB:
		// Extract RGB components
		r := uint8((c.Value >> 16) & 0xFF)
		g := uint8((c.Value >> 8) & 0xFF)
		b := uint8(c.Value & 0xFF)
		return color.RGBA{R: r, G: g, B: b, A: 255}
	}
	return nil
}

// ansiColor returns an indexed ANSI color.
// Uses ultraviolet's BasicColor for 0-15, ExtendedColor for 16-255.
type ansiColor uint32

func (c ansiColor) RGBA() (r, g, b, a uint32) {
	idx := uint32(c)
	if idx < 16 {
		col := ansiPalette[idx]
		return uint32(col.R) * 257, uint32(col.G) * 257, uint32(col.B) * 257, 65535
	}

	// For 16-255: compute from 6x6x6 color cube or grayscale
	if idx < 232 {
		// 6x6x6 color cube (indices 16-231)
		idx -= 16
		rVal := (idx / 36) % 6
		gVal := (idx / 6) % 6
		bVal := idx % 6

		// Each component is 0, 95, 135, 175, 215, or 255
		rLevel := uint32(0)
		if rVal > 0 {
			rLevel = 55 + rVal*40
		}
		gLevel := uint32(0)
		if gVal > 0 {
			gLevel = 55 + gVal*40
		}
		bLevel := uint32(0)
		if bVal > 0 {
			bLevel = 55 + bVal*40
		}

		return rLevel * 257, gLevel * 257, bLevel * 257, 65535
	}

	gray := 8 + (idx-232)*10
	return gray * 257, gray * 257, gray * 257, 65535
}

type PositionedVTermLayer struct {
	*VTermLayer
	PosX, PosY    int
	Width, Height int
}

var _ uv.Drawable = (*PositionedVTermLayer)(nil)

// Draw renders the VTerm snapshot at the specified position within the canvas.
func (l *PositionedVTermLayer) Draw(s uv.Screen, r uv.Rectangle) {
	if l.VTermLayer == nil {
		return
	}
	// Delegate to VTermLayer.DrawAt with our position and dimensions
	l.VTermLayer.DrawAt(s, l.PosX, l.PosY, l.Width, l.Height)
}
