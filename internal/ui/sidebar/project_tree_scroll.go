package sidebar

func (m *ProjectTree) maxScrollOffset() int {
	maxOffset := len(m.flatNodes) - m.visibleHeight()
	if maxOffset < 0 {
		return 0
	}
	return maxOffset
}

func (m *ProjectTree) clampScrollOffset() {
	if len(m.flatNodes) == 0 {
		m.scrollOffset = 0
		return
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
	maxOffset := m.maxScrollOffset()
	if m.scrollOffset > maxOffset {
		m.scrollOffset = maxOffset
	}
}

func (m *ProjectTree) ensureCursorVisible() {
	if len(m.flatNodes) == 0 {
		m.cursor = 0
		m.scrollOffset = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.flatNodes) {
		m.cursor = len(m.flatNodes) - 1
	}
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	}
	visibleHeight := m.visibleHeight()
	if m.cursor >= m.scrollOffset+visibleHeight {
		m.scrollOffset = m.cursor - visibleHeight + 1
	}
	m.clampScrollOffset()
}

func (m *ProjectTree) cursorVisible() bool {
	if len(m.flatNodes) == 0 {
		return true
	}
	if m.cursor < 0 || m.cursor >= len(m.flatNodes) {
		return false
	}
	visibleHeight := m.visibleHeight()
	return m.cursor >= m.scrollOffset && m.cursor < m.scrollOffset+visibleHeight
}

func (m *ProjectTree) scrollBy(delta int) {
	if delta == 0 {
		return
	}
	m.scrollOffset += delta
	m.clampScrollOffset()
}

func (m *ProjectTree) reanchorCursorToViewport() {
	if len(m.flatNodes) == 0 || m.cursorVisible() {
		return
	}
	if m.cursor < m.scrollOffset {
		m.cursor = m.scrollOffset
		return
	}
	m.cursor = m.scrollOffset + m.visibleHeight() - 1
	if m.cursor >= len(m.flatNodes) {
		m.cursor = len(m.flatNodes) - 1
	}
}

func (m *ProjectTree) viewportAnchorPath() string {
	if len(m.flatNodes) == 0 {
		return ""
	}
	if m.scrollOffset < 0 || m.scrollOffset >= len(m.flatNodes) {
		return ""
	}
	return m.flatNodes[m.scrollOffset].Path
}

func (m *ProjectTree) restoreViewportAnchor(path string) bool {
	if path == "" {
		return false
	}
	for i, node := range m.flatNodes {
		if node.Path != path {
			continue
		}
		m.scrollOffset = i
		m.clampScrollOffset()
		return true
	}
	return false
}

func (m *ProjectTree) scrollPage(delta int) {
	if delta == 0 || len(m.flatNodes) == 0 {
		return
	}
	visibleHeight := m.visibleHeight()
	step := max(1, visibleHeight/2)
	anchor := m.cursor
	if !m.cursorVisible() {
		if delta > 0 {
			anchor = m.scrollOffset
		} else {
			anchor = m.scrollOffset + visibleHeight - 1
			if anchor >= len(m.flatNodes) {
				anchor = len(m.flatNodes) - 1
			}
		}
	}
	desiredRow := anchor - m.scrollOffset
	if desiredRow < 0 {
		desiredRow = 0
	}
	if desiredRow >= visibleHeight {
		desiredRow = visibleHeight - 1
	}
	m.cursor = anchor + delta*step
	m.scrollOffset = m.cursor - desiredRow
	m.clampScrollOffset()
	m.ensureCursorVisible()
}
