package vterm

import "strings"

// IsInSelection checks if coordinate (x, y) is within the selection
func (v *VTerm) IsInSelection(x, y int) bool {
	if !v.selActive {
		return false
	}

	// Normalize selection so start is before end
	startX, startY := v.selStartX, v.selStartY
	endX, endY := v.selEndX, v.selEndY
	if startY > endY || (startY == endY && startX > endX) {
		startX, endX = endX, startX
		startY, endY = endY, startY
	}

	// Check if (x, y) is in selection range
	if y < startY || y > endY {
		return false
	}
	if v.selRect {
		return x >= startX && x <= endX
	}
	if y == startY && y == endY {
		// Single line selection
		return x >= startX && x <= endX
	}
	if y == startY {
		return x >= startX
	}
	if y == endY {
		return x <= endX
	}
	// Middle lines are fully selected
	return true
}

// SetSelection stores selection coordinates for rendering with highlight.
func (v *VTerm) SetSelection(startX, startY, endX, endY int, active bool, rect bool) {
	changed := v.selStartX != startX ||
		v.selStartY != startY ||
		v.selEndX != endX ||
		v.selEndY != endY ||
		v.selActive != active ||
		v.selRect != rect
	if !changed {
		return
	}
	v.selStartX = startX
	v.selStartY = startY
	v.selEndX = endX
	v.selEndY = endY
	v.selActive = active
	v.selRect = rect
	v.renderDirtyAll = true
	v.bumpVersion()
}

// ClearSelection clears the current selection
func (v *VTerm) ClearSelection() {
	if !v.selActive {
		return
	}
	v.selActive = false
	v.selRect = false
	v.renderDirtyAll = true
	v.bumpVersion()
}

// GetSelectedText extracts text from the selection range.
// startX, startY, endX, endY are in visible screen coordinates (0-indexed).
// This handles scrollback by converting visible Y to absolute line numbers.
func (v *VTerm) GetSelectedText(startX, startY, endX, endY int) string {
	// Normalize coordinates so start is before end
	if startY > endY || (startY == endY && startX > endX) {
		startX, endX = endX, startX
		startY, endY = endY, startY
	}

	// Clamp to valid range
	if startX < 0 {
		startX = 0
	}
	if endX >= v.Width {
		endX = v.Width - 1
	}
	if startY < 0 {
		startY = 0
	}
	if endY >= v.Height {
		endY = v.Height - 1
	}

	// Convert visible Y coordinates to absolute line numbers
	// (matching the logic in renderWithScrollback)
	screen, scrollbackLen := v.RenderBuffers()
	screenLen := len(screen)
	startLine := scrollbackLen + screenLen - v.Height - v.ViewOffset
	if startLine < 0 {
		startLine = 0
	}

	var result strings.Builder

	for y := startY; y <= endY; y++ {
		absLineNum := startLine + y

		// Get the row from scrollback or screen
		var row []Cell
		if absLineNum < scrollbackLen {
			row = v.Scrollback[absLineNum]
		} else if absLineNum-scrollbackLen < screenLen {
			row = screen[absLineNum-scrollbackLen]
		}

		if row == nil {
			if y < endY {
				result.WriteRune('\n')
			}
			continue
		}

		// Determine X range for this line
		xStart := 0
		xEnd := len(row) - 1
		if y == startY {
			xStart = startX
		}
		if y == endY {
			xEnd = endX
		}
		if xEnd >= len(row) {
			xEnd = len(row) - 1
		}

		// Extract characters from the row
		for x := xStart; x <= xEnd; x++ {
			if x < len(row) {
				if row[x].Width == 0 {
					continue
				}
				r := row[x].Rune
				if r == 0 {
					r = ' '
				}
				result.WriteRune(r)
			}
		}

		// Add newline between lines (but not after the last line)
		if y < endY {
			result.WriteRune('\n')
		}
	}

	// Trim trailing spaces from each line
	lines := strings.Split(result.String(), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}

// GetTextRange extracts text from a range in the combined scrollback+screen buffer.
// Coordinates are absolute line indices (0-based) and columns.
func (v *VTerm) GetTextRange(startX, startLine, endX, endLine int) string {
	if v == nil {
		return ""
	}

	screen, scrollbackLen := v.RenderBuffers()
	total := scrollbackLen + len(screen)
	if total == 0 {
		return ""
	}

	// Normalize selection so start is before end.
	if startLine > endLine || (startLine == endLine && startX > endX) {
		startX, endX = endX, startX
		startLine, endLine = endLine, startLine
	}

	if startLine < 0 {
		startLine = 0
	}
	if endLine < 0 {
		endLine = 0
	}
	if startLine >= total {
		startLine = total - 1
	}
	if endLine >= total {
		endLine = total - 1
	}

	width := v.Width
	if width < 1 {
		width = 1
	}
	if startX < 0 {
		startX = 0
	}
	if endX < 0 {
		endX = 0
	}
	if startX >= width {
		startX = width - 1
	}
	if endX >= width {
		endX = width - 1
	}

	lineAt := func(idx int) []Cell {
		if idx < 0 {
			return nil
		}
		if idx < scrollbackLen {
			return v.Scrollback[idx]
		}
		si := idx - scrollbackLen
		if si >= 0 && si < len(screen) {
			return screen[si]
		}
		return nil
	}

	var result strings.Builder
	for line := startLine; line <= endLine; line++ {
		row := lineAt(line)
		if row == nil {
			row = MakeBlankLine(width)
		}

		xStart := 0
		xEnd := len(row) - 1
		if line == startLine {
			xStart = startX
		}
		if line == endLine {
			xEnd = endX
		}
		if xStart < 0 {
			xStart = 0
		}
		if xEnd >= len(row) {
			xEnd = len(row) - 1
		}
		if xEnd < xStart {
			xEnd = xStart
		}

		for x := xStart; x <= xEnd; x++ {
			if x < len(row) {
				if row[x].Width == 0 {
					continue
				}
				r := row[x].Rune
				if r == 0 {
					r = ' '
				}
				result.WriteRune(r)
			}
		}

		if line < endLine {
			result.WriteRune('\n')
		}
	}

	lines := strings.Split(result.String(), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}

// LineCells returns the cell slice for an absolute line index in scrollback+screen.
func (v *VTerm) LineCells(line int) []Cell {
	if v == nil {
		return nil
	}
	screen, scrollbackLen := v.RenderBuffers()
	if line < 0 {
		return nil
	}
	if line < scrollbackLen {
		return v.Scrollback[line]
	}
	idx := line - scrollbackLen
	if idx >= 0 && idx < len(screen) {
		return screen[idx]
	}
	return nil
}
