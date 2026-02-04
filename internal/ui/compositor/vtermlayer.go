package compositor

import (
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/andyrewlee/amux/internal/perf"
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
	// Selection state (used during rendering)
	SelActive            bool
	SelStartX, SelStartY int
	SelEndX, SelEndY     int
}

// NewVTermSnapshot creates a snapshot from a VTerm.
// MUST be called while holding the appropriate lock on the VTerm.
func NewVTermSnapshot(term *vterm.VTerm, showCursor bool) *VTermSnapshot {
	return NewVTermSnapshotWithCache(term, showCursor, nil)
}

// NewVTermSnapshotWithCache creates a snapshot from a VTerm, optionally reusing
// lines from a previous snapshot when dirty line tracking allows.
// MUST be called while holding the appropriate lock on the VTerm.
func NewVTermSnapshotWithCache(term *vterm.VTerm, showCursor bool, prev *VTermSnapshot) *VTermSnapshot {
	if term == nil {
		return nil
	}
	defer perf.Time("vterm_snapshot")()

	width := term.Width
	height := term.Height
	if width <= 0 || height <= 0 {
		return nil
	}

	// Copy dirty lines to avoid sharing the backing array
	dirtyLines, allDirty := term.DirtyLines()
	var dirtyLinesCopy []bool
	if dirtyLines != nil {
		if prev != nil && len(prev.DirtyLines) == len(dirtyLines) {
			dirtyLinesCopy = prev.DirtyLines[:len(dirtyLines)]
		} else {
			dirtyLinesCopy = make([]bool, len(dirtyLines))
		}
		copy(dirtyLinesCopy, dirtyLines)
	}

	// Ensure cursor-only changes mark lines dirty for layer rendering.
	// Cursor moves or visibility toggles don't always touch renderDirty,
	// so we force redraw of the previous and current cursor lines when needed.
	if !allDirty && term.ViewOffset == 0 {
		lastCursorX := term.LastCursorX()
		lastCursorY := term.LastCursorY()
		lastShowCursor := term.LastShowCursor()
		lastCursorHidden := term.LastCursorHidden()

		cursorChanged := showCursor != lastShowCursor ||
			term.CursorHidden != lastCursorHidden ||
			term.CursorX != lastCursorX ||
			term.CursorY != lastCursorY

		if cursorChanged {
			// Defensive: ensure dirtyLinesCopy matches screen height.
			if dirtyLinesCopy == nil {
				dirtyLinesCopy = make([]bool, height)
			}
			if lastCursorY >= 0 && lastCursorY < len(dirtyLinesCopy) {
				dirtyLinesCopy[lastCursorY] = true
			}
			if term.CursorY >= 0 && term.CursorY < len(dirtyLinesCopy) {
				dirtyLinesCopy[term.CursorY] = true
			}
		}
	}

	canReuse := prev != nil && prev.Width == width && prev.Height == height && len(prev.Screen) == height
	useDirty := canReuse &&
		prev.ViewOffset == term.ViewOffset &&
		!allDirty &&
		term.ViewOffset == 0 &&
		dirtyLines != nil &&
		len(dirtyLines) == height

	var screen [][]vterm.Cell
	if useDirty {
		screen = prev.Screen
		if screen == nil || len(screen) != height {
			screen = make([][]vterm.Cell, height)
		}

		visible, _ := term.RenderBuffers()
		for y := 0; y < height; y++ {
			needsCopy := dirtyLines[y]
			if screen[y] == nil || len(screen[y]) != width {
				needsCopy = true
			}
			if !needsCopy {
				continue
			}

			line := screen[y]
			if line == nil || len(line) != width {
				line = vterm.MakeBlankLine(width)
			}
			if y < len(visible) {
				copy(line, visible[y])
			} else {
				for i := range line {
					line[i] = vterm.DefaultCell()
				}
			}
			screen[y] = line
		}
	} else {
		// Full snapshot when dirty tracking isn't usable.
		reuseScreen := prev != nil && prev.Width == width && prev.Height == height && len(prev.Screen) == height
		if reuseScreen {
			screen = term.VisibleScreenInto(prev.Screen)
		} else {
			screen = term.VisibleScreen()
		}
		if len(screen) == 0 {
			return nil
		}
	}

	snap := prev
	if snap == nil {
		snap = &VTermSnapshot{}
	}

	snap.Screen = screen
	snap.DirtyLines = dirtyLinesCopy
	snap.AllDirty = allDirty
	snap.CursorX = term.CursorX
	snap.CursorY = term.CursorY
	snap.ViewOffset = term.ViewOffset
	snap.CursorHidden = term.CursorHidden
	snap.ShowCursor = showCursor
	snap.Width = width
	snap.Height = height
	snap.SelActive = term.SelActive()
	snap.SelStartX = 0
	snap.SelStartY = 0
	snap.SelEndX = 0
	snap.SelEndY = 0

	if snap.SelActive {
		startLine := term.SelStartLine()
		endLine := term.SelEndLine()
		startX := term.SelStartX()
		endX := term.SelEndX()

		// Normalize so start is before end.
		if startLine > endLine || (startLine == endLine && startX > endX) {
			startLine, endLine = endLine, startLine
			startX, endX = endX, startX
		}

		visibleStartLine := term.ScreenYToAbsoluteLine(0)
		visibleEndLine := term.ScreenYToAbsoluteLine(height - 1)

		// If selection is entirely outside viewport, disable selection rendering.
		if endLine < visibleStartLine || startLine > visibleEndLine {
			snap.SelActive = false
		} else {
			if startLine < visibleStartLine {
				snap.SelStartY = 0
				startX = 0
			} else {
				snap.SelStartY = startLine - visibleStartLine
			}

			if endLine > visibleEndLine {
				snap.SelEndY = height - 1
				endX = width - 1
			} else {
				snap.SelEndY = endLine - visibleStartLine
			}

			if startX < 0 {
				startX = 0
			}
			if startX >= width {
				startX = width - 1
			}
			if endX < 0 {
				endX = 0
			}
			if endX >= width {
				endX = width - 1
			}

			snap.SelStartX = startX
			snap.SelEndX = endX
		}
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

			// For continuation cells (part of wide character), write an empty cell
			// to clear any stale content at that position from previous renders.
			if cell.Width == 0 {
				uvCell := getCell()
				uvCell.Content = ""
				uvCell.Width = 0
				s.SetCell(posX+x, posY+y, uvCell)
				putCell(uvCell)
				continue
			}

			// Build the ultraviolet cell from pool
			uvCell := cellToUVSnapshot(cell, snap, x, y)

			// Set cell at screen position (ultraviolet copies the value)
			s.SetCell(posX+x, posY+y, uvCell)

			// Return cell to pool for reuse
			putCell(uvCell)
		}
	}
}

// cellToUVSnapshot converts a vterm.Cell to a pooled uv.Cell.
// Caller must call putCell() after passing to SetCell.
func cellToUVSnapshot(cell vterm.Cell, snap *VTermSnapshot, x, y int) *uv.Cell {
	style := cell.Style

	// Apply selection and cursor reverse (selection has precedence over cursor)
	inSel := inSelectionSnapshot(snap, x, y)
	cursorHere := snap.ShowCursor && !snap.CursorHidden &&
		y == snap.CursorY && x == snap.CursorX && snap.ViewOffset == 0
	if inSel || cursorHere {
		style.Reverse = !style.Reverse
	}

	// Suppress underline on blank cells (prevents visual scanlines)
	if style.Underline && (cell.Rune == 0 || cell.Rune == ' ') {
		style.Underline = false
	}

	r := cell.Rune
	if r == 0 {
		r = ' '
	}

	uvCell := getCell()
	uvCell.Content = runeToString(r)
	uvCell.Style = vtermStyleToUV(style)
	uvCell.Width = cell.Width
	return uvCell
}

// inSelectionSnapshot checks if a coordinate is within the snapshot's selection.
func inSelectionSnapshot(snap *VTermSnapshot, x, y int) bool {
	if snap == nil || !snap.SelActive {
		return false
	}

	startX, startY := snap.SelStartX, snap.SelStartY
	endX, endY := snap.SelEndX, snap.SelEndY

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

	gray := uint32(8 + (idx-232)*10)
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
