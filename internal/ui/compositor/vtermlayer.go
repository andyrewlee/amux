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

// VTermLayer implements tea.Layer for direct cell-based rendering of a VTerm snapshot.
// This uses a snapshot to avoid data races - the snapshot is created while holding
// the VTerm lock, and rendering happens without any locks.
type VTermLayer struct {
	Snap *VTermSnapshot
}

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

	// When compositing layers, we must draw ALL cells every frame.
	// The dirty line optimization only works for single-layer rendering.
	// Ultraviolet's cell-level diffing handles the actual screen updates.
	//
	// SetCell copies the cell value (ultraviolet's Line.Set does `l[x] = *c`),
	// so a single local cell can be reused across every iteration instead of
	// renting one from a sync.Pool per cell per frame.
	var uvCell uv.Cell
	for y := 0; y < height && y < len(snap.Screen); y++ {
		row := snap.Screen[y]
		if row == nil {
			continue
		}

		for x := 0; x < width && x < len(row); x++ {
			cell := row[x]

			// For continuation cells (part of wide character), write an empty cell
			// to clear any stale content at that position from previous renders.
			if cell.Width == 0 {
				uvCell = uv.Cell{Content: "", Width: 0}
				s.SetCell(posX+x, posY+y, &uvCell)
				continue
			}

			// Build the ultraviolet cell into the reused local.
			cellToUVSnapshot(&uvCell, cell, snap, x, y)

			// Set cell at screen position (ultraviolet copies the value).
			s.SetCell(posX+x, posY+y, &uvCell)
		}
	}
}

// cellToUVSnapshot fills uvCell with the ultraviolet representation of a
// vterm.Cell. It fully overwrites uvCell so the same value can be reused
// across draw iterations.
func cellToUVSnapshot(uvCell *uv.Cell, cell vterm.Cell, snap *VTermSnapshot, x, y int) {
	style := cell.Style

	// Apply selection and cursor reverse (selection has precedence over cursor)
	inSel := inSelectionSnapshot(snap, x, y)
	cursorHere := snap.ShowCursor && !snap.CursorHidden &&
		y == snap.CursorY && x == snap.CursorX && snap.ViewOffset == 0
	if inSel || cursorHere {
		style.Reverse = !style.Reverse
	}

	// Suppress underline on blank cells (prevents visual scanlines)
	style = vterm.SuppressBlankUnderline(cell.Rune, style)

	if snap.SuppressBlink {
		style.Blink = false
	}

	content := cell.GraphemeCluster
	if content == "" {
		r := vterm.RenderableRune(cell.Rune)
		content = runeToString(r)
	}

	*uvCell = uv.Cell{
		Content: content,
		Style:   vtermStyleToUV(style),
		Width:   cell.Width,
	}
}

// inSelectionSnapshot checks if a coordinate is within the snapshot's selection.
func inSelectionSnapshot(snap *VTermSnapshot, x, y int) bool {
	if snap == nil || !snap.SelActive {
		return false
	}
	startX, startY, endX, endY := vterm.NormalizeSelectionRange(
		snap.SelStartX, snap.SelStartY, snap.SelEndX, snap.SelEndY)
	return vterm.SelectionContains(startX, startY, endX, endY, x, y)
}

// vtermStyleToUV converts a vterm.Style to ultraviolet's Style.
func vtermStyleToUV(s vterm.Style) uv.Style {
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
		return rgbToUV(c.Value)
	}
	return nil
}

// rgbColorCache memoizes color.Color interface values keyed by the 24-bit packed
// RGB value. Boxing a color.RGBA into the color.Color interface allocates, and
// vtermColorToUV runs twice per cell per frame; caching collapses that churn to
// a bounded set since the same handful of colors dominate any given frame.
var rgbColorCache sync.Map // uint32 -> color.Color

// rgbToUV returns a cached color.Color for a 24-bit packed RGB value, building
// (and boxing) it once on first use.
func rgbToUV(v uint32) color.Color {
	if cached, ok := rgbColorCache.Load(v); ok {
		if col, ok := cached.(color.Color); ok {
			return col
		}
	}
	c := color.Color(color.RGBA{
		R: uint8((v >> 16) & 0xFF),
		G: uint8((v >> 8) & 0xFF),
		B: uint8(v & 0xFF),
		A: 255,
	})
	rgbColorCache.Store(v, c)
	return c
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
