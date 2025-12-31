package vterm

import (
	"fmt"
	"strings"
)

// Render returns the terminal content as a string with ANSI codes
func (v *VTerm) Render() string {
	screen, scrollbackLen := v.renderBuffers()
	if v.ViewOffset > 0 {
		return v.renderWithScrollbackFrom(screen, scrollbackLen)
	}
	return v.renderScreenFrom(screen)
}

func (v *VTerm) renderBuffers() ([][]Cell, int) {
	if v.syncActive && v.syncScreen != nil {
		scrollbackLen := v.syncScrollbackLen
		if scrollbackLen > len(v.Scrollback) {
			scrollbackLen = len(v.Scrollback)
		}
		return v.syncScreen, scrollbackLen
	}
	return v.Screen, len(v.Scrollback)
}

// isInSelection checks if coordinate (x, y) is within the selection
func (v *VTerm) isInSelection(x, y int) bool {
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

// renderScreenFrom renders the given screen buffer
func (v *VTerm) renderScreenFrom(screen [][]Cell) string {
	var buf strings.Builder
	buf.Grow(v.Width * v.Height * 2) // Rough estimate

	var lastStyle Style
	var lastReverse bool
	firstCell := true

	for y, row := range screen {
		for x, cell := range row {
			// Check if this cell is in selection
			inSel := v.isInSelection(x, y)

			// Apply style changes (toggle reverse for selection)
			style := cell.Style
			if inSel {
				style.Reverse = !style.Reverse
			}

			if firstCell || style != lastStyle || inSel != lastReverse {
				buf.WriteString(styleToANSI(style))
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

		if y < len(v.Screen)-1 {
			buf.WriteString("\n")
		}
	}

	// Reset styles at end
	buf.WriteString("\x1b[0m")

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
			inSel := v.isInSelection(x, i)

			// Apply style changes (toggle reverse for selection)
			style := cell.Style
			if inSel {
				style.Reverse = !style.Reverse
			}

			if firstCell || style != lastStyle || inSel != lastReverse {
				buf.WriteString(styleToANSI(style))
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

// styleToANSI converts a Style to ANSI escape codes
func styleToANSI(s Style) string {
	var codes []string

	// Reset first if any attributes
	codes = append(codes, "0")

	if s.Bold {
		codes = append(codes, "1")
	}
	if s.Dim {
		codes = append(codes, "2")
	}
	if s.Italic {
		codes = append(codes, "3")
	}
	if s.Underline {
		codes = append(codes, "4")
	}
	if s.Blink {
		codes = append(codes, "5")
	}
	if s.Reverse {
		codes = append(codes, "7")
	}
	if s.Hidden {
		codes = append(codes, "8")
	}
	if s.Strike {
		codes = append(codes, "9")
	}

	// Foreground color
	codes = append(codes, colorToANSI(s.Fg, true)...)

	// Background color
	codes = append(codes, colorToANSI(s.Bg, false)...)

	return fmt.Sprintf("\x1b[%sm", strings.Join(codes, ";"))
}

// colorToANSI converts a Color to ANSI code strings
func colorToANSI(c Color, fg bool) []string {
	switch c.Type {
	case ColorDefault:
		return nil
	case ColorIndexed:
		idx := c.Value
		if idx < 8 {
			if fg {
				return []string{fmt.Sprintf("%d", 30+idx)}
			}
			return []string{fmt.Sprintf("%d", 40+idx)}
		} else if idx < 16 {
			if fg {
				return []string{fmt.Sprintf("%d", 90+idx-8)}
			}
			return []string{fmt.Sprintf("%d", 100+idx-8)}
		} else {
			if fg {
				return []string{"38", "5", fmt.Sprintf("%d", idx)}
			}
			return []string{"48", "5", fmt.Sprintf("%d", idx)}
		}
	case ColorRGB:
		r := (c.Value >> 16) & 0xFF
		g := (c.Value >> 8) & 0xFF
		b := c.Value & 0xFF
		if fg {
			return []string{"38", "2", fmt.Sprintf("%d", r), fmt.Sprintf("%d", g), fmt.Sprintf("%d", b)}
		}
		return []string{"48", "2", fmt.Sprintf("%d", r), fmt.Sprintf("%d", g), fmt.Sprintf("%d", b)}
	}
	return nil
}

// GetAllLines returns all content (scrollback + screen) as lines for search
func (v *VTerm) GetAllLines() []string {
	lines := make([]string, 0, len(v.Scrollback)+len(v.Screen))

	for _, row := range v.Scrollback {
		lines = append(lines, rowToString(row))
	}
	for _, row := range v.Screen {
		lines = append(lines, rowToString(row))
	}

	return lines
}

// rowToString converts a row of cells to a plain string (no ANSI)
func rowToString(row []Cell) string {
	var buf strings.Builder
	for _, cell := range row {
		if cell.Rune == 0 {
			buf.WriteRune(' ')
		} else {
			buf.WriteRune(cell.Rune)
		}
	}
	// Trim trailing spaces
	return strings.TrimRight(buf.String(), " ")
}

// Search finds all line indices matching query
func (v *VTerm) Search(query string) []int {
	if query == "" {
		return nil
	}

	query = strings.ToLower(query)
	lines := v.GetAllLines()
	var matches []int

	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), query) {
			matches = append(matches, i)
		}
	}

	return matches
}

// ScrollToLine scrolls view to show the given line index (in combined buffer)
func (v *VTerm) ScrollToLine(lineIdx int) {
	totalLines := len(v.Scrollback) + len(v.Screen)

	// Calculate ViewOffset to center this line
	targetOffset := totalLines - lineIdx - v.Height/2
	if targetOffset < 0 {
		targetOffset = 0
	}
	if targetOffset > len(v.Scrollback) {
		targetOffset = len(v.Scrollback)
	}

	v.ViewOffset = targetOffset
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
	screen, scrollbackLen := v.renderBuffers()
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

// SetSelection stores selection coordinates for rendering with highlight.
// Pass nil coordinates to clear selection.
func (v *VTerm) SetSelection(startX, startY, endX, endY int, active bool) {
	v.selStartX = startX
	v.selStartY = startY
	v.selEndX = endX
	v.selEndY = endY
	v.selActive = active
}

// ClearSelection clears the current selection
func (v *VTerm) ClearSelection() {
	v.selActive = false
}
