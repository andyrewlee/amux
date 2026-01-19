package vterm

import (
	"strings"
)

// Render returns the terminal content as a string with ANSI codes
func (v *VTerm) Render() string {
	screen, scrollbackLen := v.RenderBuffers()
	if v.ViewOffset > 0 {
		return v.renderWithScrollbackFrom(screen, scrollbackLen)
	}
	if v.syncActive {
		return v.renderScreenFrom(screen)
	}
	return v.renderScreenCached(screen)
}

// RenderBuffers returns the current screen buffer and scrollback length.
// During synchronized output, it returns the frozen snapshot.
func (v *VTerm) RenderBuffers() ([][]Cell, int) {
	if v.syncActive && v.syncScreen != nil {
		scrollbackLen := v.syncScrollbackLen
		if scrollbackLen > len(v.Scrollback) {
			scrollbackLen = len(v.Scrollback)
		}
		return v.syncScreen, scrollbackLen
	}
	return v.Screen, len(v.Scrollback)
}

// VisibleScreen returns the currently visible screen buffer as a copy.
func (v *VTerm) VisibleScreen() [][]Cell {
	screen, scrollbackLen := v.RenderBuffers()
	width := v.Width
	height := v.Height

	lines := make([][]Cell, height)

	// If scrolled, pull from scrollback + screen.
	if v.ViewOffset > 0 {
		if scrollbackLen > len(v.Scrollback) {
			scrollbackLen = len(v.Scrollback)
		}
		screenLen := len(screen)
		startLine := scrollbackLen + screenLen - height - v.ViewOffset
		if startLine < 0 {
			startLine = 0
		}

		for i := 0; i < height; i++ {
			lineIdx := startLine + i
			var row []Cell
			if lineIdx < scrollbackLen {
				row = v.Scrollback[lineIdx]
			} else if lineIdx-scrollbackLen < screenLen {
				row = screen[lineIdx-scrollbackLen]
			}
			line := MakeBlankLine(width)
			if row != nil {
				copy(line, row)
			}
			lines[i] = line
		}
		return lines
	}

	// Live screen.
	for y := 0; y < height; y++ {
		line := MakeBlankLine(width)
		if y < len(screen) {
			copy(line, screen[y])
		}
		lines[y] = line
	}
	return lines
}

// VisibleScreenWithSelection returns the visible screen with selection highlighting applied.
func (v *VTerm) VisibleScreenWithSelection() [][]Cell {
	lines := v.VisibleScreen()
	if !v.selActive {
		return lines
	}

	for y := 0; y < len(lines); y++ {
		row := lines[y]
		for x := 0; x < len(row); x++ {
			if v.IsInSelection(x, y) {
				cell := row[x]
				cell.Style.Reverse = !cell.Style.Reverse
				row[x] = cell
			}
		}
		lines[y] = row
	}

	return lines
}

// renderScreenFrom renders the given screen buffer
func (v *VTerm) renderScreenFrom(screen [][]Cell) string {
	var buf strings.Builder
	buf.Grow(v.Width * v.Height * 2) // Rough estimate

	for y, row := range screen {
		buf.WriteString(v.renderRow(row, y))

		if y < len(v.Screen)-1 {
			buf.WriteString("\n")
		}
	}

	// Reset styles at end
	buf.WriteString("\x1b[0m")

	return buf.String()
}

func (v *VTerm) renderScreenCached(screen [][]Cell) string {
	v.ensureRenderCache(len(screen))

	// Invalidate cursor lines if cursor state changed
	if v.ShowCursor != v.lastShowCursor || v.CursorHidden != v.lastCursorHidden || v.CursorX != v.lastCursorX || v.CursorY != v.lastCursorY {
		// Mark old cursor line dirty
		if v.lastCursorY >= 0 && v.lastCursorY < len(v.renderDirty) {
			v.renderDirty[v.lastCursorY] = true
		}
		// Mark new cursor line dirty
		if v.CursorY >= 0 && v.CursorY < len(v.renderDirty) {
			v.renderDirty[v.CursorY] = true
		}
		v.lastShowCursor = v.ShowCursor
		v.lastCursorHidden = v.CursorHidden
		v.lastCursorX = v.CursorX
		v.lastCursorY = v.CursorY
	}

	lines := make([]string, len(screen))
	for y, row := range screen {
		if v.renderDirtyAll || v.renderDirty[y] || v.renderCache[y] == "" {
			lines[y] = v.renderRow(row, y)
			v.renderCache[y] = lines[y]
			v.renderDirty[y] = false
		} else {
			lines[y] = v.renderCache[y]
		}
	}
	v.renderDirtyAll = false

	out := strings.Join(lines, "\n")
	return out + "\x1b[0m"
}

func (v *VTerm) renderRow(row []Cell, y int) string {
	var buf strings.Builder
	buf.Grow(v.Width * 2)

	// Reset per-line to make cached lines independent.
	buf.WriteString("\x1b[0m")
	var lastStyle Style
	var lastReverse bool

	// Determine if cursor is on this row and should be shown
	// Don't show cursor if terminal app hid it via DECTCEM
	cursorOnRow := v.ShowCursor && !v.CursorHidden && y == v.CursorY && v.ViewOffset == 0

	for x := 0; x < v.Width; x++ {
		var cell Cell
		if x < len(row) {
			cell = row[x]
		} else {
			cell = DefaultCell()
		}
		// Check if this cell is in selection
		inSel := v.IsInSelection(x, y)

		// Check if cursor is at this position
		isCursor := cursorOnRow && x == v.CursorX

		// Apply style changes (toggle reverse for selection or cursor)
		style := cell.Style
		if inSel || isCursor {
			style.Reverse = !style.Reverse
		}
		style = suppressBlankUnderline(cell, style)

		if style != lastStyle || inSel != lastReverse || isCursor {
			// Use delta encoding after the first style (which has reset)
			if x == 0 {
				buf.WriteString(StyleToANSI(style))
			} else {
				buf.WriteString(StyleToDeltaANSI(lastStyle, style))
			}
			lastStyle = style
			lastReverse = inSel
		}

		// Skip continuation cells (part of wide character)
		if cell.Width == 0 {
			continue
		}

		if cell.Rune == 0 {
			buf.WriteRune(' ')
		} else {
			buf.WriteRune(cell.Rune)
		}
	}

	return buf.String()
}

// renderWithScrollbackFrom renders content from scrollback + screen
func (v *VTerm) renderWithScrollbackFrom(screen [][]Cell, scrollbackLen int) string {
	var buf strings.Builder
	buf.Grow(v.Width * v.Height * 2)

	// Calculate which lines to show
	// ViewOffset = how many lines scrolled up into history
	if scrollbackLen > len(v.Scrollback) {
		scrollbackLen = len(v.Scrollback)
	}
	screenLen := len(screen)

	// Start position in the combined buffer (scrollback + screen)
	// When ViewOffset = scrollbackLen, we show from the start of scrollback
	// When ViewOffset = 0, we show the screen
	startLine := scrollbackLen + screenLen - v.Height - v.ViewOffset
	if startLine < 0 {
		startLine = 0
	}

	var lastStyle Style
	var lastReverse bool
	firstCell := true

	for i := 0; i < v.Height; i++ {
		lineIdx := startLine + i

		var row []Cell
		if lineIdx < scrollbackLen {
			row = v.Scrollback[lineIdx]
		} else if lineIdx-scrollbackLen < screenLen {
			row = screen[lineIdx-scrollbackLen]
		}

		// Render the row
		for x := 0; x < v.Width; x++ {
			var cell Cell
			if row != nil && x < len(row) {
				cell = row[x]
			} else {
				cell = DefaultCell()
			}

			// Check if this cell is in selection (i is the visible Y coord)
			inSel := v.IsInSelection(x, i)

			// Apply style changes (toggle reverse for selection)
			style := cell.Style
			if inSel {
				style.Reverse = !style.Reverse
			}
			style = suppressBlankUnderline(cell, style)

			if firstCell || style != lastStyle || inSel != lastReverse {
				buf.WriteString(StyleToANSI(style))
				lastStyle = style
				lastReverse = inSel
				firstCell = false
			}

			// Skip continuation cells (part of wide character)
			if cell.Width == 0 {
				continue
			}

			if cell.Rune == 0 {
				buf.WriteRune(' ')
			} else {
				buf.WriteRune(cell.Rune)
			}
		}

		if i < v.Height-1 {
			buf.WriteString("\n")
		}
	}

	buf.WriteString("\x1b[0m")
	return buf.String()
}

func suppressBlankUnderline(cell Cell, style Style) Style {
	// Some TUIs leave underline enabled while clearing rows; underline on spaces
	// renders as scanlines, so drop it for blank cells at render time.
	if !style.Underline {
		return style
	}
	if cell.Rune == 0 || cell.Rune == ' ' {
		style.Underline = false
	}
	return style
}
