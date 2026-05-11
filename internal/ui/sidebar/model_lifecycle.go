package sidebar

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// SetShowKeymapHints controls whether helper text is rendered.
func (m *Model) SetShowKeymapHints(show bool) {
	if m.showKeymapHints == show {
		return
	}
	oldVisibleHeight := m.visibleHeight()
	m.showKeymapHints = show
	newVisibleHeight := m.visibleHeight()
	switch {
	case newVisibleHeight < oldVisibleHeight:
		if m.focused {
			m.ensureCursorVisible()
		}
	case newVisibleHeight > oldVisibleHeight:
		m.clampScrollOffset()
	}
}

// SetStyles updates the component's styles (for theme changes).
func (m *Model) SetStyles(styles common.Styles) {
	m.styles = styles
}

// Init initializes the sidebar.
func (m *Model) Init() tea.Cmd {
	return nil
}

// SetSize sets the sidebar size.
func (m *Model) SetSize(width, height int) {
	if m.width == width && m.height == height {
		return
	}
	oldVisibleHeight := m.visibleHeight()
	m.width = width
	m.height = height
	newVisibleHeight := m.visibleHeight()
	switch {
	case newVisibleHeight < oldVisibleHeight:
		if m.focused {
			m.ensureCursorVisible()
		}
	case newVisibleHeight > oldVisibleHeight:
		m.clampScrollOffset()
	}
}

// Focus sets the focus state.
func (m *Model) Focus() {
	if m.focused {
		return
	}
	m.focused = true
	if !m.cursorVisible() {
		m.ensureCursorVisible()
	}
}

// Blur removes focus.
func (m *Model) Blur() {
	m.focused = false
	// Exit filter mode when losing focus
	if m.filterMode {
		m.filterMode = false
		m.filterInput.Blur()
	}
}

// Focused returns whether the sidebar is focused.
func (m *Model) Focused() bool {
	return m.focused
}

// SetWorkspace sets the active workspace.
func (m *Model) SetWorkspace(ws *data.Workspace) {
	if sameWorkspaceByCanonicalPaths(m.workspace, ws) {
		wasCursorVisible := m.cursorVisible()
		// Rebind pointer for metadata freshness without resetting UI state.
		m.workspace = ws
		if m.focused && wasCursorVisible && !m.cursorVisible() {
			m.ensureCursorVisible()
		}
		return
	}
	m.workspace = ws
	m.cursor = 0
	m.scrollOffset = 0
	m.filterQuery = ""
	m.filterInput.SetValue("")
	m.rebuildDisplayList()
}

// SetGitStatus sets the git status.
func (m *Model) SetGitStatus(status *git.StatusResult) {
	m.gitStatus = status
	m.rebuildDisplayList()
	m.clampScrollOffset()
}
