package vterm

import "strings"

// HasSelection returns true if there is an active selection.
func (v *VTerm) HasSelection() bool {
	return v.selActive
}

// IsInSelection checks if coordinate (x, screenY) is within the selection.
// screenY is a screen-relative coordinate (0 to Height-1).
func (v *VTerm) IsInSelection(x, screenY int) bool {
	if !v.selActive {
		return false
	}

	// Convert screenY to absolute line number
	absLine := v.ScreenYToAbsoluteLine(screenY)

	// Normalize selection so start is before end.
	startX, startLine := v.selStartX, v.selStartLine
	endX, endLine := v.selEndX, v.selEndLine
	if startLine > endLine || (startLine == endLine && startX > endX) {
		startX, endX = endX, startX
		startLine, endLine = endLine, startLine
	}

	// Check if (x, absLine) is in selection range
	if absLine < startLine || absLine > endLine {
		return false
	}
	if v.selRect {
		minX := startX
		maxX := endX
		if minX > maxX {
			minX, maxX = maxX, minX
		}
		return x >= minX && x <= maxX
	}
	if startLine == endLine {
		minX := startX
		maxX := endX
		if minX > maxX {
			minX, maxX = maxX, minX
		}
		return x >= minX && x <= maxX
	}
	if absLine == startLine {
		return x >= startX
	}
	if absLine == endLine {
		return x <= endX
	}
	return true
}

// SetSelection stores selection coordinates for rendering with highlight.
// startLine and endLine are absolute line numbers (0 = first scrollback line).
func (v *VTerm) SetSelection(startX, startLine, endX, endLine int, active bool, rect bool) {
	changed := v.selStartX != startX ||
		v.selStartLine != startLine ||
		v.selEndX != endX ||
		v.selEndLine != endLine ||
		v.selActive != active ||
		v.selRect != rect
	if !changed {
		return
	}
	v.selStartX = startX
	v.selStartLine = startLine
	v.selEndX = endX
	v.selEndLine = endLine
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

// shiftSelectionAfterTrim updates selection line indices after scrollback trimming.
// If the selection is fully trimmed away, it is cleared. When partially trimmed,
// the remaining selection is clamped to the new start of scrollback.
func (v *VTerm) shiftSelectionAfterTrim(trim int) {
	if !v.selActive || trim <= 0 {
		return
	}

	prevStartX, prevStartLine := v.selStartX, v.selStartLine
	prevEndX, prevEndLine := v.selEndX, v.selEndLine
	prevActive := v.selActive

	v.selStartLine -= trim
	v.selEndLine -= trim

	if v.selStartLine < 0 && v.selEndLine < 0 {
		v.selActive = false
		v.selRect = false
	} else {
		if v.selStartLine < 0 {
			v.selStartLine = 0
			v.selStartX = 0
		}
		if v.selEndLine < 0 {
			v.selEndLine = 0
			v.selEndX = 0
		}
	}

	if prevActive != v.selActive ||
		prevStartX != v.selStartX ||
		prevStartLine != v.selStartLine ||
		prevEndX != v.selEndX ||
		prevEndLine != v.selEndLine {
		v.renderDirtyAll = true
		v.bumpVersion()
	}
}

// GetSelectedText extracts text from the current selection.
// Returns empty string if no selection is active.
func (v *VTerm) GetSelectedText(startX, startLine, endX, endLine int) string {
	return v.GetTextRange(startX, startLine, endX, endLine)
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
