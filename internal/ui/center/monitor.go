package center

// MonitorModel tracks selection state for the monitor grid.
type MonitorModel struct {
	selectedIndex int
}

// Reset clears the monitor selection.
func (m *MonitorModel) Reset() {
	m.selectedIndex = 0
}

// SelectedIndex returns the clamped selection index for the given count.
func (m *MonitorModel) SelectedIndex(count int) int {
	if count <= 0 {
		m.selectedIndex = 0
		return 0
	}
	if m.selectedIndex < 0 {
		m.selectedIndex = 0
	}
	if m.selectedIndex >= count {
		m.selectedIndex = count - 1
	}
	return m.selectedIndex
}

// SetSelectedIndex updates the selection and clamps it to bounds.
func (m *MonitorModel) SetSelectedIndex(index, count int) {
	m.selectedIndex = index
	m.SelectedIndex(count)
}

// MoveSelection updates the selection based on grid movement.
func (m *MonitorModel) MoveSelection(dx, dy, cols, rows, count int) {
	if count <= 0 || cols <= 0 || rows <= 0 {
		m.selectedIndex = 0
		return
	}

	idx := m.SelectedIndex(count)
	row := idx / cols
	col := idx % cols

	row += dy
	col += dx

	if row < 0 {
		row = 0
	}
	if row >= rows {
		row = rows - 1
	}
	if col < 0 {
		col = 0
	}
	if col >= cols {
		col = cols - 1
	}

	newIndex := row*cols + col
	if newIndex >= count {
		newIndex = count - 1
	}
	if newIndex < 0 {
		newIndex = 0
	}
	m.selectedIndex = newIndex
}
