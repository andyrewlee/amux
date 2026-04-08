package sidebar

import (
	"strings"

	"github.com/andyrewlee/amux/internal/git"
)

func (m *Model) maxScrollOffset() int {
	maxOffset := len(m.displayItems) - m.visibleHeight()
	if maxOffset < 0 {
		return 0
	}
	return maxOffset
}

func (m *Model) clampScrollOffset() {
	if len(m.displayItems) == 0 {
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

func (m *Model) cursorVisible() bool {
	if len(m.displayItems) == 0 {
		return true
	}
	if m.cursor < 0 || m.cursor >= len(m.displayItems) {
		return false
	}
	visibleHeight := m.visibleHeight()
	return m.cursor >= m.scrollOffset && m.cursor < m.scrollOffset+visibleHeight
}

func (m *Model) ensureCursorVisible() {
	if len(m.displayItems) == 0 {
		m.cursor = 0
		m.scrollOffset = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.displayItems) {
		m.cursor = len(m.displayItems) - 1
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

func (m *Model) scrollBy(delta int) {
	if delta == 0 {
		return
	}
	m.scrollOffset += delta
	m.clampScrollOffset()
}

func (m *Model) reanchorCursorToViewport() {
	if len(m.displayItems) == 0 || m.cursorVisible() {
		return
	}
	if m.cursor < m.scrollOffset {
		m.cursor = m.scrollOffset
		for m.cursor < len(m.displayItems) && m.displayItems[m.cursor].isHeader {
			m.cursor++
		}
		if m.cursor >= len(m.displayItems) {
			m.cursor = m.lastSelectableIndex()
		}
	} else {
		m.cursor = m.scrollOffset + m.visibleHeight() - 1
		if m.cursor >= len(m.displayItems) {
			m.cursor = len(m.displayItems) - 1
		}
		if m.displayItems[m.cursor].isHeader {
			candidate := m.cursor + 1
			for candidate < len(m.displayItems) && m.displayItems[candidate].isHeader {
				candidate++
			}
			if candidate < len(m.displayItems) {
				m.cursor = candidate
			} else {
				for m.cursor >= 0 && m.displayItems[m.cursor].isHeader {
					m.cursor--
				}
			}
		}
		if m.cursor >= 0 && m.cursor < len(m.displayItems) && m.displayItems[m.cursor].isHeader {
			for m.cursor >= 0 && m.displayItems[m.cursor].isHeader {
				m.cursor--
			}
		}
		if m.cursor < 0 {
			m.cursor = m.firstSelectableIndex()
		}
	}
	m.ensureCursorVisible()
}

type changeViewportAnchor struct {
	path     string
	mode     git.DiffMode
	section  string
	isHeader bool
}

func (m *Model) viewportAnchor() changeViewportAnchor {
	if len(m.displayItems) == 0 {
		return changeViewportAnchor{}
	}
	start := m.scrollOffset
	if start < 0 {
		start = 0
	}
	end := start + m.visibleHeight()
	if end > len(m.displayItems) {
		end = len(m.displayItems)
	}
	if start < len(m.displayItems) && m.displayItems[start].isHeader {
		return changeViewportAnchor{
			section:  headerSection(m.displayItems[start].header),
			isHeader: true,
		}
	}
	for i := start; i < end; i++ {
		item := m.displayItems[i]
		if item.change != nil {
			return changeViewportAnchor{
				path:    item.change.Path,
				mode:    item.mode,
				section: itemSection(item),
			}
		}
	}
	return changeViewportAnchor{}
}

func (m *Model) restoreViewportAnchor(anchor changeViewportAnchor) bool {
	if anchor.isHeader {
		for i, item := range m.displayItems {
			if !item.isHeader || headerSection(item.header) != anchor.section {
				continue
			}
			m.scrollOffset = i
			m.clampScrollOffset()
			return true
		}
		return false
	}
	if anchor.path == "" {
		return false
	}
	fallback := -1
	for i, item := range m.displayItems {
		if item.change == nil || item.change.Path != anchor.path {
			continue
		}
		if itemSection(item) == anchor.section && fallback == -1 {
			fallback = i
		}
		if item.mode == anchor.mode && itemSection(item) == anchor.section {
			m.scrollOffset = i
			m.clampScrollOffset()
			return true
		}
	}
	if fallback == -1 {
		return false
	}
	m.scrollOffset = fallback
	m.clampScrollOffset()
	return true
}

func itemSection(item displayItem) string {
	if item.isHeader {
		return headerSection(item.header)
	}
	if item.change != nil && item.change.Kind == git.ChangeUntracked {
		return "Untracked"
	}
	if item.mode == git.DiffModeStaged {
		return "Staged"
	}
	return "Unstaged"
}

func headerSection(header string) string {
	section, _, found := strings.Cut(header, " (")
	if found {
		return section
	}
	return header
}

func (m *Model) firstSelectableIndex() int {
	for i := 0; i < len(m.displayItems); i++ {
		if !m.displayItems[i].isHeader {
			return i
		}
	}
	return 0
}

func (m *Model) lastSelectableIndex() int {
	for i := len(m.displayItems) - 1; i >= 0; i-- {
		if !m.displayItems[i].isHeader {
			return i
		}
	}
	return 0
}

func (m *Model) advanceCursorBy(delta int) int {
	if len(m.displayItems) == 0 {
		return 0
	}
	if delta == 0 {
		if m.cursor < 0 {
			return m.firstSelectableIndex()
		}
		if m.cursor >= len(m.displayItems) {
			return m.lastSelectableIndex()
		}
		return m.cursor
	}

	newCursor := m.cursor
	direction := 1
	if delta < 0 {
		direction = -1
		delta = -delta
	}

	for range delta {
		candidate := newCursor + direction
		for candidate >= 0 && candidate < len(m.displayItems) && m.displayItems[candidate].isHeader {
			candidate += direction
		}
		if candidate < 0 {
			return m.firstSelectableIndex()
		}
		if candidate >= len(m.displayItems) {
			return m.lastSelectableIndex()
		}
		newCursor = candidate
	}

	return newCursor
}

func (m *Model) viewportHasSelectableItem() bool {
	if len(m.displayItems) == 0 {
		return false
	}
	start := m.scrollOffset
	if start < 0 {
		start = 0
	}
	end := start + m.visibleHeight()
	if end > len(m.displayItems) {
		end = len(m.displayItems)
	}
	for i := start; i < end; i++ {
		if !m.displayItems[i].isHeader {
			return true
		}
	}
	return false
}

func (m *Model) scrollPage(delta int) {
	if delta == 0 || len(m.displayItems) == 0 {
		return
	}
	visibleHeight := m.visibleHeight()
	step := max(1, visibleHeight/2)
	anchor := m.cursor
	viewportHasSelectableItem := m.viewportHasSelectableItem()
	if !m.cursorVisible() {
		if delta > 0 {
			anchor = m.scrollOffset
			for anchor < len(m.displayItems) && m.displayItems[anchor].isHeader {
				anchor++
			}
			if anchor >= len(m.displayItems) {
				anchor = m.lastSelectableIndex()
			}
		} else {
			anchor = m.scrollOffset + visibleHeight - 1
			if anchor >= len(m.displayItems) {
				anchor = len(m.displayItems) - 1
			}
			for anchor >= 0 && m.displayItems[anchor].isHeader {
				anchor--
			}
			if anchor < 0 {
				anchor = m.firstSelectableIndex()
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
	if !m.cursorVisible() && !viewportHasSelectableItem {
		m.cursor = anchor
		m.scrollOffset = m.cursor - desiredRow
		m.clampScrollOffset()
		m.ensureCursorVisible()
		return
	}
	m.cursor = anchor
	m.cursor = m.advanceCursorBy(delta * step)
	m.scrollOffset = m.cursor - desiredRow
	m.clampScrollOffset()
	m.ensureCursorVisible()
}
