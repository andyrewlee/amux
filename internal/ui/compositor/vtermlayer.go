package compositor

import (
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/andyrewlee/amux/internal/vterm"
)

// ansiPalette is the standard ANSI color palette (0-15).
// Colors 0-7 are standard, 8-15 are bright variants.
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

// VTermSnapshot captures the state needed to render a VTerm.
// This is created while holding Tab.mu and can be safely used for rendering
// without holding any locks, avoiding data races with PTY output.
type VTermSnapshot struct {
	Screen       [][]vterm.Cell
	DirtyLines   []bool
	AllDirty     bool
	CursorX      int
	CursorY      int
	ViewOffset   int
	CursorHidden bool
	ShowCursor   bool
	Width        int
	Height       int
	// Selection state (already baked into Screen via VisibleScreenWithSelection)
}

// NewVTermSnapshot creates a snapshot from a VTerm.
// MUST be called while holding the appropriate lock on the VTerm.
func NewVTermSnapshot(term *vterm.VTerm, showCursor bool) *VTermSnapshot {
	if term == nil {
		return nil
	}

	// Get the visible screen with selection highlighting already applied
	screen := term.VisibleScreenWithSelection()
	if len(screen) == 0 {
		return nil
	}

	// Copy dirty lines to avoid sharing the backing array
	dirtyLines, allDirty := term.DirtyLines()
	var dirtyLinesCopy []bool
	if dirtyLines != nil {
		dirtyLinesCopy = make([]bool, len(dirtyLines))
		copy(dirtyLinesCopy, dirtyLines)
	}

	// P1 FIX: Ensure cursor lines are always dirty when cursor is visible.
	// The VTerm dirty tracking doesn't account for cursor movement or ShowCursor
	// changes, so we must force the current cursor line to be redrawn.
	// Also mark the previous cursor line dirty if it differs (cursor moved).
	if !allDirty && showCursor && !term.CursorHidden && term.ViewOffset == 0 {
		cursorY := term.CursorY
		if cursorY >= 0 && cursorY < len(dirtyLinesCopy) {
			dirtyLinesCopy[cursorY] = true
		}
		// Also mark the previous cursor position dirty if it differs
		lastCursorY := term.LastCursorY()
		if lastCursorY != cursorY && lastCursorY >= 0 && lastCursorY < len(dirtyLinesCopy) {
			dirtyLinesCopy[lastCursorY] = true
		}
	}

	snap := &VTermSnapshot{
		Screen:       screen,
		DirtyLines:   dirtyLinesCopy,
		AllDirty:     allDirty,
		CursorX:      term.CursorX,
		CursorY:      term.CursorY,
		ViewOffset:   term.ViewOffset,
		CursorHidden: term.CursorHidden,
		ShowCursor:   showCursor,
		Width:        term.Width,
		Height:       term.Height,
	}

	// Clear dirty state after snapshotting (while still holding the lock)
	// Also update cursor tracking for next frame
	term.ClearDirtyWithCursor(showCursor)

	return snap
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
	for y := 0; y < height && y < len(snap.Screen); y++ {
		row := snap.Screen[y]
		if row == nil {
			continue
		}

		for x := 0; x < width && x < len(row); x++ {
			cell := row[x]

			// Skip continuation cells (part of wide character)
			if cell.Width == 0 {
				continue
			}

			// Build the ultraviolet cell
			uvCell := cellToUVSnapshot(cell, snap, x, y)

			// Set cell at screen position
			s.SetCell(posX+x, posY+y, uvCell)
		}
	}
}

// cellToUVSnapshot converts a vterm.Cell to an ultraviolet Cell using snapshot state.
func cellToUVSnapshot(cell vterm.Cell, snap *VTermSnapshot, x, y int) *uv.Cell {
	style := cell.Style

	// Check if cursor is at this position
	cursorHere := snap.ShowCursor && !snap.CursorHidden &&
		y == snap.CursorY && x == snap.CursorX && snap.ViewOffset == 0

	// Toggle reverse for cursor (selection is already baked into the screen)
	if cursorHere {
		style.Reverse = !style.Reverse
	}

	// Suppress underline on blank cells (prevents scanlines)
	if style.Underline && (cell.Rune == 0 || cell.Rune == ' ') {
		style.Underline = false
	}

	// Get the character
	r := cell.Rune
	if r == 0 {
		r = ' '
	}

	return &uv.Cell{
		Content: string(r),
		Style:   vtermStyleToUV(style),
		Width:   cell.Width,
	}
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
			rLevel = uint32(55 + rVal*40)
		}
		gLevel := uint32(0)
		if gVal > 0 {
			gLevel = uint32(55 + gVal*40)
		}
		bLevel := uint32(0)
		if bVal > 0 {
			bLevel = uint32(55 + bVal*40)
		}

		return rLevel * 257, gLevel * 257, bLevel * 257, 65535
	}

	// Grayscale (indices 232-255)
	gray := uint32(8 + (idx-232)*10)
	return gray * 257, gray * 257, gray * 257, 65535
}

// PositionedVTermLayer wraps a VTermLayer with explicit positioning.
// This allows the layer to be positioned within a larger canvas.
type PositionedVTermLayer struct {
	*VTermLayer
	PosX, PosY    int
	Width, Height int
}

// Ensure PositionedVTermLayer implements uv.Drawable
var _ uv.Drawable = (*PositionedVTermLayer)(nil)

// Draw renders the VTerm snapshot at the specified position within the canvas.
func (l *PositionedVTermLayer) Draw(s uv.Screen, r uv.Rectangle) {
	if l.VTermLayer == nil {
		return
	}
	// Delegate to VTermLayer.DrawAt with our position and dimensions
	l.VTermLayer.DrawAt(s, l.PosX, l.PosY, l.Width, l.Height)
}
