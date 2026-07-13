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

// SetWorkspace sets the active workspace. It returns a command that fetches
// the ahead/behind badge for the new workspace (nil when ws is nil or this is
// just a pointer rebind of the same workspace).
func (m *Model) SetWorkspace(ws *data.Workspace) tea.Cmd {
	if sameWorkspaceByCanonicalPaths(m.workspace, ws) {
		// Rebind pointer for metadata freshness without resetting UI state.
		m.workspace = ws
		return nil
	}
	m.workspace = ws
	m.cursor = 0
	m.scrollOffset = 0
	m.filterQuery = ""
	m.filterInput.SetValue("")
	// Fully exit filter mode (mirroring Blur) so a workspace switch mid-filter
	// doesn't leave a phantom filter capturing subsequent keystrokes.
	if m.filterMode {
		m.filterMode = false
		m.filterInput.Blur()
	}
	// Branch-mode data (list + ahead/behind) belongs to the previous
	// workspace; drop it rather than showing stale results under the new one.
	m.branchMode = false
	m.branchChanges = nil
	m.branchErr = nil
	m.branchLoading = false
	m.ahead = 0
	m.behind = 0
	m.aheadBehindErr = nil
	m.rebuildDisplayList()
	return m.refreshAheadBehind()
}

// FilterActive reports whether the Changes view is currently in filter-input
// mode (used so the tab bar doesn't steal digit keys meant for the filter).
func (m *Model) FilterActive() bool {
	return m.filterMode
}

// SetGitStatus sets the git status.
func (m *Model) SetGitStatus(status *git.StatusResult) {
	m.gitStatus = status
	m.rebuildDisplayList()
}
