package sidebar

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// SetShowKeymapHints controls whether helper text is rendered.
func (m *Model) SetShowKeymapHints(show bool) {
	m.showKeymapHints = show
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
	m.width = width
	m.height = height
}

// Focus sets the focus state.
func (m *Model) Focus() {
	m.focused = true
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
}
