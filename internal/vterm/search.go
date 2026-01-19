package vterm

import "strings"

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
		if cell.Width == 0 {
			continue
		}
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

// VisibleLineRange returns the [start, end) line indices in the combined
// scrollback+screen buffer that are currently visible, along with total lines.
func (v *VTerm) VisibleLineRange() (start, end, total int) {
	if v == nil {
		return 0, 0, 0
	}

	screen, scrollbackLen := v.RenderBuffers()
	total = scrollbackLen + len(screen)
	if total <= 0 || v.Height <= 0 {
		return 0, 0, total
	}

	start = total - v.Height - v.ViewOffset
	if start < 0 {
		start = 0
	}
	end = start + v.Height
	if end > total {
		end = total
	}
	return
}

// TotalLines returns the total number of lines in scrollback+screen.
func (v *VTerm) TotalLines() int {
	if v == nil {
		return 0
	}
	screen, scrollbackLen := v.RenderBuffers()
	return scrollbackLen + len(screen)
}

// MaxViewOffset returns the maximum scrollback offset for the current buffers.
func (v *VTerm) MaxViewOffset() int {
	if v == nil {
		return 0
	}
	_, scrollbackLen := v.RenderBuffers()
	return scrollbackLen
}
