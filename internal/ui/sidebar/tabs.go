package sidebar

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/compositor"
)

// SidebarTab represents a tab type in the sidebar
type SidebarTab int

const (
	TabGit SidebarTab = iota
	TabProject
)

// tabHitKind identifies the type of tab bar click target
type tabHitKind int

const (
	tabHitGit tabHitKind = iota
	tabHitProject
)

// tabHit represents a clickable region in the tab bar
type tabHit struct {
	kind   tabHitKind
	region common.HitRegion
}

// TabbedSidebar wraps the Git (lazygit) and Project views with tabs
type TabbedSidebar struct {
	activeTab   SidebarTab
	lazygit     *LazygitModel
	projectTree *ProjectTree
	tabHits     []tabHit

	workspace       *data.Workspace
	focused         bool
	width           int
	height          int
	showKeymapHints bool

	styles common.Styles
}

// NewTabbedSidebar creates a new tabbed sidebar
func NewTabbedSidebar() *TabbedSidebar {
	return &TabbedSidebar{
		activeTab:   TabGit,
		lazygit:     NewLazygitModel(),
		projectTree: NewProjectTree(),
		styles:      common.DefaultStyles(),
	}
}

// SetShowKeymapHints controls whether helper text is rendered.
func (m *TabbedSidebar) SetShowKeymapHints(show bool) {
	m.showKeymapHints = show
	m.lazygit.SetShowKeymapHints(show)
	m.projectTree.SetShowKeymapHints(show)
}

// SetStyles updates the component's styles (for theme changes).
func (m *TabbedSidebar) SetStyles(styles common.Styles) {
	m.styles = styles
	m.lazygit.SetStyles(styles)
	m.projectTree.SetStyles(styles)
}

// SetMsgSink sets the PTY message sink for the lazygit model.
func (m *TabbedSidebar) SetMsgSink(sink func(tea.Msg)) {
	m.lazygit.SetMsgSink(sink)
}

// Init initializes the tabbed sidebar
func (m *TabbedSidebar) Init() tea.Cmd {
	return common.SafeBatch(
		m.lazygit.Init(),
		m.projectTree.Init(),
	)
}

// Update handles messages
func (m *TabbedSidebar) Update(msg tea.Msg) (*TabbedSidebar, tea.Cmd) {
	var cmds []tea.Cmd

	// Handle tab switching on mouse click
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft && msg.Y == 0 {
			// Check if click is in tab bar
			for _, hit := range m.tabHits {
				if hit.region.Contains(msg.X, msg.Y) {
					switch hit.kind {
					case tabHitGit:
						m.activeTab = TabGit
						m.updateFocus()
					case tabHitProject:
						m.activeTab = TabProject
						m.updateFocus()
					}
					return m, nil
				}
			}
		}

		// Adjust Y coordinate for tab bar before forwarding to inner models
		adjustedMsg := tea.MouseClickMsg{
			Button: msg.Button,
			X:      msg.X,
			Y:      msg.Y - 1, // Subtract tab bar height
		}
		switch m.activeTab {
		case TabGit:
			var cmd tea.Cmd
			m.lazygit, cmd = m.lazygit.Update(adjustedMsg)
			cmds = append(cmds, cmd)
		case TabProject:
			var cmd tea.Cmd
			m.projectTree, cmd = m.projectTree.Update(adjustedMsg)
			cmds = append(cmds, cmd)
		}
		return m, common.SafeBatch(cmds...)

	case tea.MouseWheelMsg:
		// Adjust Y coordinate for tab bar before forwarding
		adjustedMsg := tea.MouseWheelMsg{
			Button: msg.Button,
			X:      msg.X,
			Y:      msg.Y - 1,
		}
		switch m.activeTab {
		case TabGit:
			var cmd tea.Cmd
			m.lazygit, cmd = m.lazygit.Update(adjustedMsg)
			cmds = append(cmds, cmd)
		case TabProject:
			var cmd tea.Cmd
			m.projectTree, cmd = m.projectTree.Update(adjustedMsg)
			cmds = append(cmds, cmd)
		}
		return m, common.SafeBatch(cmds...)

	case tea.KeyPressMsg:
		// Tab switching with number keys when focused
		if m.focused {
			switch {
			case key.Matches(msg, key.NewBinding(key.WithKeys("1"))):
				m.activeTab = TabGit
				m.updateFocus()
				return m, nil
			case key.Matches(msg, key.NewBinding(key.WithKeys("2"))):
				m.activeTab = TabProject
				m.updateFocus()
				return m, nil
			}
		}
	}

	// Forward lazygit PTY messages to lazygit model regardless of active tab
	switch msg.(type) {
	case LazygitStarted, messages.LazygitPTYOutput, messages.LazygitPTYFlush, messages.LazygitPTYStopped:
		var cmd tea.Cmd
		m.lazygit, cmd = m.lazygit.Update(msg)
		cmds = append(cmds, cmd)
		return m, common.SafeBatch(cmds...)
	}

	// Forward messages to active tab
	switch m.activeTab {
	case TabGit:
		var cmd tea.Cmd
		m.lazygit, cmd = m.lazygit.Update(msg)
		cmds = append(cmds, cmd)
	case TabProject:
		var cmd tea.Cmd
		m.projectTree, cmd = m.projectTree.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, common.SafeBatch(cmds...)
}

// updateFocus ensures only the active tab is focused
func (m *TabbedSidebar) updateFocus() {
	if m.focused {
		switch m.activeTab {
		case TabGit:
			m.lazygit.Focus()
			m.projectTree.Blur()
		case TabProject:
			m.lazygit.Blur()
			m.projectTree.Focus()
		}
	} else {
		m.lazygit.Blur()
		m.projectTree.Blur()
	}
}

// renderTabBar renders the tab bar
func (m *TabbedSidebar) renderTabBar() string {
	m.tabHits = m.tabHits[:0]

	// Tab styles
	inactiveStyle := m.styles.Tab
	activeTabStyle := lipgloss.NewStyle().
		Padding(0, 1).
		Foreground(common.ColorForeground()).
		Background(common.ColorSurface2())

	var tabs []string
	x := 0

	// Git tab (lazygit)
	gitLabel := "Git"
	var gitRendered string
	if m.activeTab == TabGit {
		gitRendered = activeTabStyle.Render(gitLabel)
	} else {
		gitRendered = inactiveStyle.Render(m.styles.Muted.Render(gitLabel))
	}
	gitWidth := lipgloss.Width(gitRendered)
	m.tabHits = append(m.tabHits, tabHit{
		kind: tabHitGit,
		region: common.HitRegion{
			X:      x,
			Y:      0,
			Width:  gitWidth,
			Height: 1,
		},
	})
	tabs = append(tabs, gitRendered)
	x += gitWidth

	// Project tab
	projectLabel := "Project"
	var projectRendered string
	if m.activeTab == TabProject {
		projectRendered = activeTabStyle.Render(projectLabel)
	} else {
		projectRendered = inactiveStyle.Render(m.styles.Muted.Render(projectLabel))
	}
	projectWidth := lipgloss.Width(projectRendered)
	m.tabHits = append(m.tabHits, tabHit{
		kind: tabHitProject,
		region: common.HitRegion{
			X:      x,
			Y:      0,
			Width:  projectWidth,
			Height: 1,
		},
	})
	tabs = append(tabs, projectRendered)

	return lipgloss.JoinHorizontal(lipgloss.Bottom, tabs...)
}

// View renders the tabbed sidebar
func (m *TabbedSidebar) View() string {
	if m.height <= 0 {
		return ""
	}
	// Tab bar
	tabBar := m.renderTabBar()

	// Content based on active tab
	contentHeight := m.height - 1 // -1 for tab bar
	if contentHeight <= 0 {
		return tabBar
	}

	var b strings.Builder
	b.WriteString(tabBar)
	b.WriteString("\n")

	var content string
	switch m.activeTab {
	case TabGit:
		m.lazygit.SetSize(m.width, contentHeight)
		content = m.lazygit.View()
	case TabProject:
		m.projectTree.SetSize(m.width, contentHeight)
		content = m.projectTree.View()
	}

	b.WriteString(content)
	return b.String()
}

// TabBarView returns only the tab bar view (for compositor)
func (m *TabbedSidebar) TabBarView() string {
	return m.renderTabBar()
}

// ContentView returns only the content view without tab bar (for compositor)
func (m *TabbedSidebar) ContentView() string {
	contentHeight := m.height - 1
	if contentHeight <= 0 {
		return ""
	}

	switch m.activeTab {
	case TabGit:
		m.lazygit.SetSize(m.width, contentHeight)
		return m.lazygit.View()
	case TabProject:
		m.projectTree.SetSize(m.width, contentHeight)
		return m.projectTree.View()
	}
	return ""
}

// SetSize sets the sidebar size
func (m *TabbedSidebar) SetSize(width, height int) {
	m.width = width
	m.height = height

	contentHeight := height - 1 // -1 for tab bar
	if contentHeight < 0 {
		contentHeight = 0
	}
	m.lazygit.SetSize(width, contentHeight)
	m.projectTree.SetSize(width, contentHeight)
}

// Focus sets the focus state
func (m *TabbedSidebar) Focus() {
	m.focused = true
	m.updateFocus()
}

// Blur removes focus
func (m *TabbedSidebar) Blur() {
	m.focused = false
	m.lazygit.Blur()
	m.projectTree.Blur()
}

// Focused returns whether the sidebar is focused
func (m *TabbedSidebar) Focused() bool {
	return m.focused
}

// SetWorkspace sets the active workspace
func (m *TabbedSidebar) SetWorkspace(ws *data.Workspace) tea.Cmd {
	m.workspace = ws
	cmd := m.lazygit.SetWorkspace(ws)
	m.projectTree.SetWorkspace(ws)
	return cmd
}

// ActiveTab returns the currently active tab
func (m *TabbedSidebar) ActiveTab() SidebarTab {
	return m.activeTab
}

// SetActiveTab sets the active tab
func (m *TabbedSidebar) SetActiveTab(tab SidebarTab) {
	m.activeTab = tab
	m.updateFocus()
}

// NextTab switches to the next tab (circular)
func (m *TabbedSidebar) NextTab() {
	if m.activeTab == TabGit {
		m.activeTab = TabProject
	} else {
		m.activeTab = TabGit
	}
	m.updateFocus()
}

// PrevTab switches to the previous tab (circular)
func (m *TabbedSidebar) PrevTab() {
	if m.activeTab == TabGit {
		m.activeTab = TabProject
	} else {
		m.activeTab = TabGit
	}
	m.updateFocus()
}

// Lazygit returns the lazygit model (for direct access if needed)
func (m *TabbedSidebar) Lazygit() *LazygitModel {
	return m.lazygit
}

// ProjectTree returns the project tree model (for direct access if needed)
func (m *TabbedSidebar) ProjectTree() *ProjectTree {
	return m.projectTree
}

// TerminalLayerWithCursorOwner returns a VTermLayer for the lazygit pane when
// the Git tab is active, enforcing cursor ownership.
func (m *TabbedSidebar) TerminalLayerWithCursorOwner(cursorOwner bool) *compositor.VTermLayer {
	if m.activeTab != TabGit {
		return nil
	}
	return m.lazygit.TerminalLayerWithCursorOwner(cursorOwner)
}

// Close shuts down the sidebar (stops lazygit PTY).
func (m *TabbedSidebar) Close() {
	m.lazygit.Close()
}
