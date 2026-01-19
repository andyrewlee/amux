package commits

// moveCursor moves the cursor by delta, handling scroll offset
func (m *Model) moveCursor(delta int) {
	// Find next valid cursor position (skip graph-only lines)
	newCursor := m.cursor + delta

	// Find valid commit lines only
	validIndices := m.getValidIndices()
	if len(validIndices) == 0 {
		return
	}

	// Find the closest valid index
	if delta > 0 {
		// Moving down
		for _, idx := range validIndices {
			if idx > m.cursor {
				newCursor = idx
				break
			}
		}
		// If we didn't find one, stay at current or go to last
		if newCursor <= m.cursor && len(validIndices) > 0 {
			newCursor = validIndices[len(validIndices)-1]
		}
	} else {
		// Moving up
		for i := len(validIndices) - 1; i >= 0; i-- {
			if validIndices[i] < m.cursor {
				newCursor = validIndices[i]
				break
			}
		}
		// If we didn't find one, stay at current or go to first
		if newCursor >= m.cursor && len(validIndices) > 0 {
			newCursor = validIndices[0]
		}
	}

	// Clamp to valid range
	if newCursor < 0 {
		newCursor = 0
	}
	if newCursor >= len(m.commits) {
		newCursor = len(m.commits) - 1
	}

	m.cursor = newCursor
	m.adjustScroll()
}

// getValidIndices returns indices of commits (not graph-only lines)
func (m *Model) getValidIndices() []int {
	var indices []int
	for i, c := range m.commits {
		if c.ShortHash != "" {
			indices = append(indices, i)
		}
	}
	return indices
}

// goToEnd moves cursor to the last commit
func (m *Model) goToEnd() {
	valid := m.getValidIndices()
	if len(valid) > 0 {
		m.cursor = valid[len(valid)-1]
		m.adjustScroll()
	}
}

// adjustScroll adjusts scroll offset to keep cursor visible
func (m *Model) adjustScroll() {
	visible := m.visibleHeight()
	if visible <= 0 {
		return
	}

	// Scroll up if cursor is above visible area
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	}

	// Scroll down if cursor is below visible area
	if m.cursor >= m.scrollOffset+visible {
		m.scrollOffset = m.cursor - visible + 1
	}
}

// visibleHeight returns the number of visible commit rows
func (m *Model) visibleHeight() int {
	// Account for header and help bar
	return m.height - 4
}

func (m *Model) rowIndexAt(screenY int) (int, bool) {
	headerLines := 1
	visible := m.visibleHeight()
	if screenY < headerLines || screenY >= headerLines+visible {
		return -1, false
	}
	index := m.scrollOffset + (screenY - headerLines)
	if index < 0 || index >= len(m.commits) {
		return -1, false
	}
	return index, true
}
