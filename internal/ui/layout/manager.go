package layout

import (
	"charm.land/lipgloss/v2"
)

// LayoutMode determines how many panes are visible
type LayoutMode int

const (
	LayoutThreePane LayoutMode = iota // Dashboard + Center + Sidebar
	LayoutTwoPane                     // Dashboard + Center
	LayoutOnePane                     // Dashboard only
)

// Manager handles the three-pane layout
type Manager struct {
	mode LayoutMode

	totalWidth  int
	totalHeight int

	dashboardWidth int
	centerWidth    int
	sidebarWidth   int

	// Configuration
	minChatWidth      int
	minDashboardWidth int
	minSidebarWidth   int
	startupLeftWidth  int
	startupRightWidth int
}

// NewManager creates a new layout manager
func NewManager() *Manager {
	return &Manager{
		minChatWidth:      60,
		minDashboardWidth: 20,
		minSidebarWidth:   20,
		startupLeftWidth:  28,
		startupRightWidth: 55,
	}
}

// Resize recalculates layout based on new dimensions
func (m *Manager) Resize(width, height int) {
	m.totalWidth = width
	m.totalHeight = height

	minThree := m.minDashboardWidth + m.minChatWidth + m.minSidebarWidth
	minTwo := m.minDashboardWidth + m.minChatWidth

	switch {
	case width >= minThree+20: // Some buffer for borders
		m.mode = LayoutThreePane
		m.calculateThreePaneWidths()
	case width >= minTwo+10:
		m.mode = LayoutTwoPane
		m.calculateTwoPaneWidths()
	default:
		m.mode = LayoutOnePane
		m.dashboardWidth = width
		m.centerWidth = 0
		m.sidebarWidth = 0
	}
}

// calculateThreePaneWidths calculates widths for three-pane mode
func (m *Manager) calculateThreePaneWidths() {
	// Dashboard: fixed width
	m.dashboardWidth = m.startupLeftWidth

	// Sidebar: fixed width
	m.sidebarWidth = m.startupRightWidth

	// Center: remaining space
	m.centerWidth = m.totalWidth - m.dashboardWidth - m.sidebarWidth

	// Ensure minimums
	if m.centerWidth < m.minChatWidth {
		// Reduce sidebar first
		deficit := m.minChatWidth - m.centerWidth
		m.sidebarWidth -= deficit
		m.centerWidth = m.minChatWidth

		if m.sidebarWidth < m.minSidebarWidth {
			m.sidebarWidth = m.minSidebarWidth
			m.centerWidth = m.totalWidth - m.dashboardWidth - m.sidebarWidth
		}
	}
}

// calculateTwoPaneWidths calculates widths for two-pane mode
func (m *Manager) calculateTwoPaneWidths() {
	m.dashboardWidth = m.startupLeftWidth
	m.centerWidth = m.totalWidth - m.dashboardWidth
	m.sidebarWidth = 0

	if m.centerWidth < m.minChatWidth {
		m.centerWidth = m.minChatWidth
		m.dashboardWidth = m.totalWidth - m.centerWidth
	}
}

// Mode returns the current layout mode
func (m *Manager) Mode() LayoutMode {
	return m.mode
}

// DashboardWidth returns the dashboard pane width
func (m *Manager) DashboardWidth() int {
	return m.dashboardWidth
}

// CenterWidth returns the center pane width
func (m *Manager) CenterWidth() int {
	return m.centerWidth
}

// SidebarWidth returns the sidebar pane width
func (m *Manager) SidebarWidth() int {
	return m.sidebarWidth
}

// Height returns the total height
func (m *Manager) Height() int {
	return m.totalHeight
}

// Render combines pane views based on current layout mode
func (m *Manager) Render(dashboard, center, sidebar string) string {
	switch m.mode {
	case LayoutThreePane:
		return lipgloss.JoinHorizontal(lipgloss.Top, dashboard, center, sidebar)
	case LayoutTwoPane:
		return lipgloss.JoinHorizontal(lipgloss.Top, dashboard, center)
	default:
		return dashboard
	}
}

// ShowSidebar returns whether the sidebar should be shown
func (m *Manager) ShowSidebar() bool {
	return m.mode == LayoutThreePane
}

// ShowCenter returns whether the center pane should be shown
func (m *Manager) ShowCenter() bool {
	return m.mode != LayoutOnePane
}
