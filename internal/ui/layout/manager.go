package layout

import (
	"strings"

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

	dashboardWidth  int
	centerWidth     int
	sidebarWidth    int
	terminalHeight  int  // Height of terminal pane below center (when expanded)
	gapX            int
	baseOuterGutter int
	// Some terminals effectively reserve the rightmost column (cursor/scrollbar),
	// which makes the right margin look larger. rightBias compensates for that.
	rightBias    int
	leftGutter   int
	rightGutter  int
	topGutter    int
	bottomGutter int

	// Configuration
	minChatWidth      int
	minDashboardWidth int
	minSidebarWidth   int
	startupLeftWidth  int
	startupRightWidth int

	// sidebarHidden forces two-pane mode (no sidebar) regardless of terminal width.
	sidebarHidden bool

	// terminalCollapsed controls whether terminal pane is collapsed to just a header.
	terminalCollapsed bool
}

// NewManager creates a new layout manager
func NewManager() *Manager {
	const gapX = 1
	const outerGutter = gapX + 1
	return &Manager{
		minChatWidth:      60,
		minDashboardWidth: 20,
		minSidebarWidth:   20,
		startupLeftWidth:  35,
		startupRightWidth: 55,
		gapX:              gapX,
		baseOuterGutter:   outerGutter,
		rightBias:         0,
		leftGutter:        outerGutter,
		rightGutter:       outerGutter,
		topGutter:         0,
		bottomGutter:      0,
		terminalCollapsed: true, // Default to collapsed
	}
}

// SetSidebarHidden controls the sidebar-hidden override. When true, the layout
// will never enter three-pane mode regardless of terminal width.
func (m *Manager) SetSidebarHidden(hidden bool) {
	m.sidebarHidden = hidden
}

// SidebarHidden returns whether the sidebar is force-hidden.
func (m *Manager) SidebarHidden() bool {
	return m.sidebarHidden
}

// Resize recalculates layout based on new dimensions
func (m *Manager) Resize(width, height int) {
	m.leftGutter = m.baseOuterGutter
	m.rightGutter = m.baseOuterGutter - m.rightBias
	if m.rightGutter < 0 {
		m.rightGutter = 0
	}
	usableWidth := width - (m.leftGutter + m.rightGutter)
	if usableWidth < 0 {
		usableWidth = 0
	}
	m.totalWidth = usableWidth
	usableHeight := height - m.topGutter - m.bottomGutter
	if usableHeight < 0 {
		usableHeight = 0
	}
	m.totalHeight = usableHeight

	// Calculate terminal height (~25% of total, min 5, max 12 lines)
	m.terminalHeight = usableHeight * 25 / 100
	if m.terminalHeight < 5 {
		m.terminalHeight = 5
	}
	if m.terminalHeight > 12 {
		m.terminalHeight = 12
	}
	// Ensure we don't allocate more than available
	if m.terminalHeight > usableHeight {
		m.terminalHeight = usableHeight
	}

	minThree := m.minDashboardWidth + m.minChatWidth + m.minSidebarWidth + (m.gapX * 2)
	minTwo := m.minDashboardWidth + m.minChatWidth + m.gapX

	switch {
	case !m.sidebarHidden && usableWidth >= minThree+20: // Some buffer for borders
		m.mode = LayoutThreePane
		m.calculateThreePaneWidths()
	case usableWidth >= minTwo+10:
		m.mode = LayoutTwoPane
		m.calculateTwoPaneWidths()
	default:
		m.mode = LayoutOnePane
		m.dashboardWidth = usableWidth
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
	m.centerWidth = m.totalWidth - m.dashboardWidth - m.sidebarWidth - (m.gapX * 2)

	// Ensure minimums
	if m.centerWidth < m.minChatWidth {
		// Reduce sidebar first
		deficit := m.minChatWidth - m.centerWidth
		m.sidebarWidth -= deficit
		m.centerWidth = m.minChatWidth

		if m.sidebarWidth < m.minSidebarWidth {
			m.sidebarWidth = m.minSidebarWidth
			m.centerWidth = m.totalWidth - m.dashboardWidth - m.sidebarWidth - (m.gapX * 2)
		}
	}
}

// calculateTwoPaneWidths calculates widths for two-pane mode
func (m *Manager) calculateTwoPaneWidths() {
	m.dashboardWidth = m.startupLeftWidth
	m.centerWidth = m.totalWidth - m.dashboardWidth - m.gapX
	m.sidebarWidth = 0

	if m.centerWidth < m.minChatWidth {
		m.centerWidth = m.minChatWidth
		m.dashboardWidth = m.totalWidth - m.centerWidth - m.gapX
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

// LeftGutter returns the left margin before the dashboard pane.
func (m *Manager) LeftGutter() int {
	return m.leftGutter
}

// RightGutter returns the right margin after the sidebar pane.
func (m *Manager) RightGutter() int {
	return m.rightGutter
}

// TopGutter returns the top margin above panes.
func (m *Manager) TopGutter() int {
	return m.topGutter
}

// GapX returns the horizontal gap between panes.
func (m *Manager) GapX() int {
	return m.gapX
}

// Height returns the total height
func (m *Manager) Height() int {
	return m.totalHeight
}

// Render combines pane views based on current layout mode
func (m *Manager) Render(dashboard, center, sidebar string) string {
	topPad := strings.Repeat("\n", m.topGutter)
	bottomPad := strings.Repeat("\n", m.bottomGutter)
	padLines := func(view string) string {
		if m.leftGutter == 0 && m.rightGutter == 0 {
			return view
		}
		return lipgloss.NewStyle().
			PaddingLeft(m.leftGutter).
			PaddingRight(m.rightGutter).
			Render(view)
	}
	switch m.mode {
	case LayoutThreePane:
		if m.gapX > 0 {
			gap := strings.Repeat(" ", m.gapX)
			return topPad + padLines(lipgloss.JoinHorizontal(lipgloss.Top, dashboard, gap, center, gap, sidebar)) + bottomPad
		}
		return topPad + padLines(lipgloss.JoinHorizontal(lipgloss.Top, dashboard, center, sidebar)) + bottomPad
	case LayoutTwoPane:
		if m.gapX > 0 {
			gap := strings.Repeat(" ", m.gapX)
			return topPad + padLines(lipgloss.JoinHorizontal(lipgloss.Top, dashboard, gap, center)) + bottomPad
		}
		return topPad + padLines(lipgloss.JoinHorizontal(lipgloss.Top, dashboard, center)) + bottomPad
	default:
		return topPad + padLines(dashboard) + bottomPad
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

// CollapsedTerminalHeight is the height of terminal pane when collapsed (just header bar)
const CollapsedTerminalHeight = 3

// TerminalHeight returns the terminal pane height
func (m *Manager) TerminalHeight() int {
	if m.terminalCollapsed {
		return CollapsedTerminalHeight
	}
	return m.terminalHeight
}

// ShowTerminal returns whether the terminal pane should be shown
// Terminal is visible when center is shown (not in one-pane/monitor mode)
func (m *Manager) ShowTerminal() bool {
	return m.mode != LayoutOnePane
}

// TerminalCollapsed returns whether the terminal is collapsed
func (m *Manager) TerminalCollapsed() bool {
	return m.terminalCollapsed
}

// SetTerminalCollapsed sets the terminal collapsed state
func (m *Manager) SetTerminalCollapsed(collapsed bool) {
	m.terminalCollapsed = collapsed
}

// ToggleTerminalCollapsed toggles the terminal collapsed state
func (m *Manager) ToggleTerminalCollapsed() {
	m.terminalCollapsed = !m.terminalCollapsed
}

// CenterContentHeight returns the center pane height minus terminal height
func (m *Manager) CenterContentHeight() int {
	if !m.ShowTerminal() {
		return m.totalHeight
	}
	return m.totalHeight - m.TerminalHeight()
}
