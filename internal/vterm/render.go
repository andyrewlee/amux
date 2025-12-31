package vterm

import (
	"fmt"
	"strings"
)

// Render returns the terminal content as a string with ANSI codes
func (v *VTerm) Render() string {
	if v.ViewOffset > 0 {
		return v.renderWithScrollback()
	}
	return v.renderScreen()
}

// renderScreen renders just the current screen
func (v *VTerm) renderScreen() string {
	var buf strings.Builder
	buf.Grow(v.Width * v.Height * 2) // Rough estimate

	var lastStyle Style
	firstCell := true

	for y, row := range v.Screen {
		for _, cell := range row {
			// Apply style changes
			if firstCell || cell.Style != lastStyle {
				buf.WriteString(styleToANSI(cell.Style))
				lastStyle = cell.Style
				firstCell = false
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

// renderWithScrollback renders content from scrollback + screen
func (v *VTerm) renderWithScrollback() string {
	var buf strings.Builder
	buf.Grow(v.Width * v.Height * 2)

	// Calculate which lines to show
	// ViewOffset = how many lines scrolled up into history
	scrollbackLen := len(v.Scrollback)
	screenLen := len(v.Screen)

	// Start position in the combined buffer (scrollback + screen)
	// When ViewOffset = scrollbackLen, we show from the start of scrollback
	// When ViewOffset = 0, we show the screen
	startLine := scrollbackLen + screenLen - v.Height - v.ViewOffset
	if startLine < 0 {
		startLine = 0
	}

	var lastStyle Style
	firstCell := true

	for i := 0; i < v.Height; i++ {
		lineIdx := startLine + i

		var row []Cell
		if lineIdx < scrollbackLen {
			row = v.Scrollback[lineIdx]
		} else if lineIdx-scrollbackLen < screenLen {
			row = v.Screen[lineIdx-scrollbackLen]
		}

		// Render the row
		for x := 0; x < v.Width; x++ {
			var cell Cell
			if row != nil && x < len(row) {
				cell = row[x]
			} else {
				cell = DefaultCell()
			}

			if firstCell || cell.Style != lastStyle {
				buf.WriteString(styleToANSI(cell.Style))
				lastStyle = cell.Style
				firstCell = false
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
