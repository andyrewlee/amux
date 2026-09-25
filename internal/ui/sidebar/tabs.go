package sidebar

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// SidebarTab represents a tab type in the sidebar
type SidebarTab int

const (
	TabChanges SidebarTab = iota
	TabProject
)

// sidebarTabDef describes one sidebar tab in display order. Adding a tab is:
// a SidebarTab const, an entry here, and wiring its content model into
// TabbedSidebar — the bar, number keys, click regions, focus, and
// Next/Prev cycling all follow this table.
type sidebarTabDef struct {
	tab   SidebarTab
	label string
	key   string // number key that selects this tab while the sidebar is focused
}

var sidebarTabDefs = []sidebarTabDef{
	{tab: TabChanges, label: "Changes", key: "1"},
	{tab: TabProject, label: "Project", key: "2"},
}

// tabHit represents a clickable region in the tab bar
type tabHit struct {
	tab    SidebarTab
	region common.HitRegion
}

// TabbedSidebar wraps the Changes and Project views with tabs
type TabbedSidebar struct {
	activeTab   SidebarTab
	changes     *ChangesModel
	projectTree *ProjectTree
	tabHits     []tabHit
	// tabBarVersion is a monotonic version of every input that shapes the
	// tab bar render (active tab, styles/theme). INVARIANT: every update path
	// that changes what renderTabBar produces MUST call markTabBarDirty, or
	// the compose-time gate in internal/app will keep reusing a stale tab bar
	// drawable.
	tabBarVersion uint64
	// tabBarBuilds counts TabBarView invocations; test instrumentation for
	// the compose-time skip gate in internal/app.
	tabBarBuilds uint64
	// contentBuilds counts ContentView invocations; test instrumentation for
	// the compose-time skip gate in internal/app.
	contentBuilds uint64

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
		activeTab:   TabChanges,
		changes:     NewChangesModel(),
		projectTree: NewProjectTree(),
		styles:      common.DefaultStyles(),
	}
}

// SetShowKeymapHints controls whether helper text is rendered.
func (m *TabbedSidebar) SetShowKeymapHints(show bool) {
	m.showKeymapHints = show
	m.changes.SetShowKeymapHints(show)
	m.projectTree.SetShowKeymapHints(show)
}

// SetStyles updates the component's styles (for theme changes).
func (m *TabbedSidebar) SetStyles(styles common.Styles) {
	m.styles = styles
	m.markTabBarDirty()
	m.changes.SetStyles(styles)
	m.projectTree.SetStyles(styles)
}

// Init initializes the tabbed sidebar
func (m *TabbedSidebar) Init() tea.Cmd {
	return common.SafeBatch(
		m.changes.Init(),
		m.projectTree.Init(),
	)
}

// Update handles messages
func (m *TabbedSidebar) Update(msg tea.Msg) (*TabbedSidebar, tea.Cmd) {
	var cmds []tea.Cmd

	// Handle tab switching on mouse click
	switch msg := msg.(type) {
	case messages.BranchChangesLoaded, messages.AheadBehindLoaded:
		// Route straight to the Changes model regardless of which tab is
		// active: these are background fetches (workspace switch, "g"
		// refresh, branch-mode toggle) that must land even if the user has
		// since switched to the Project tab, or the ahead/behind badge and
		// branch list would go stale until the next fetch.
		var cmd tea.Cmd
		m.changes, cmd = m.changes.Update(msg)
		return m, cmd

	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft && msg.Y == 0 {
			// Check if click is in tab bar
			for _, hit := range m.tabHits {
				if hit.region.Contains(msg.X, msg.Y) {
					m.activeTab = hit.tab
					m.markTabBarDirty()
					m.updateFocus()
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
		cmds = append(cmds, m.updateActiveContent(adjustedMsg))
		return m, common.SafeBatch(cmds...)

	case tea.MouseWheelMsg:
		// Adjust Y coordinate for tab bar before forwarding
		adjustedMsg := tea.MouseWheelMsg{
			Button: msg.Button,
			X:      msg.X,
			Y:      msg.Y - 1,
		}
		cmds = append(cmds, m.updateActiveContent(adjustedMsg))
		return m, common.SafeBatch(cmds...)

	case tea.KeyPressMsg:
		// Tab switching with number keys when focused, but not while the Changes
		// view is in filter mode (so digits get typed into the filter instead of
		// silently switching tabs).
		if m.focused && !(m.activeTab == TabChanges && m.changes.FilterActive()) {
			for _, def := range sidebarTabDefs {
				if key.Matches(msg, key.NewBinding(key.WithKeys(def.key))) {
					m.activeTab = def.tab
					m.markTabBarDirty()
					m.updateFocus()
					return m, nil
				}
			}
		}
	}

	// Forward messages to active tab
	cmds = append(cmds, m.updateActiveContent(msg))

	return m, common.SafeBatch(cmds...)
}

// updateActiveContent forwards msg to the model backing the active tab. The
// children's concrete Update return types differ, so this is the one place
// the per-tab dispatch lives.
func (m *TabbedSidebar) updateActiveContent(msg tea.Msg) tea.Cmd {
	switch m.activeTab {
	case TabChanges:
		var cmd tea.Cmd
		m.changes, cmd = m.changes.Update(msg)
		return cmd
	case TabProject:
		var cmd tea.Cmd
		m.projectTree, cmd = m.projectTree.Update(msg)
		return cmd
	}
	return nil
}

// setContentFocus focuses or blurs the model backing tab.
func (m *TabbedSidebar) setContentFocus(tab SidebarTab, focused bool) {
	switch tab {
	case TabChanges:
		if focused {
			m.changes.Focus()
		} else {
			m.changes.Blur()
		}
	case TabProject:
		if focused {
			m.projectTree.Focus()
		} else {
			m.projectTree.Blur()
		}
	}
}

// updateFocus ensures only the active tab is focused
func (m *TabbedSidebar) updateFocus() {
	for _, def := range sidebarTabDefs {
		m.setContentFocus(def.tab, m.focused && m.activeTab == def.tab)
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

	for _, def := range sidebarTabDefs {
		var rendered string
		if m.activeTab == def.tab {
			rendered = activeTabStyle.Render(def.label)
		} else {
			rendered = inactiveStyle.Render(m.styles.Muted.Render(def.label))
		}
		w := lipgloss.Width(rendered)
		m.tabHits = append(m.tabHits, tabHit{
			tab: def.tab,
			region: common.HitRegion{
				X:      x,
				Y:      0,
				Width:  w,
				Height: 1,
			},
		})
		tabs = append(tabs, rendered)
		x += w
	}

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
	b.WriteString(m.viewContent(contentHeight))
	return b.String()
}

// viewContent sizes and renders the active tab's content. The children's
// concrete types differ, so this is the one place the per-tab view dispatch
// lives.
func (m *TabbedSidebar) viewContent(contentHeight int) string {
	switch m.activeTab {
	case TabChanges:
		m.changes.SetSize(m.width, contentHeight)
		return m.changes.View()
	case TabProject:
		m.projectTree.SetSize(m.width, contentHeight)
		return m.projectTree.View()
	}
	return ""
}

// TabBarView returns only the tab bar view (for compositor)
func (m *TabbedSidebar) TabBarView() string {
	m.tabBarBuilds++
	return m.renderTabBar()
}

// TabBarVersion returns the monotonic version of the inputs to TabBarView.
// The compose layer in internal/app skips rebuilding the tab bar string
// while this version and the compose geometry are unchanged.
func (m *TabbedSidebar) TabBarVersion() uint64 {
	return m.tabBarVersion
}

// markTabBarDirty bumps tabBarVersion. It MUST be called by every update
// path that changes renderTabBar output; see the tabBarVersion field
// invariant.
func (m *TabbedSidebar) markTabBarDirty() {
	m.tabBarVersion++
}

// TabBarBuildCount reports how many times TabBarView has been invoked. Test
// instrumentation for the compose-time skip gate; not for production use.
func (m *TabbedSidebar) TabBarBuildCount() uint64 {
	return m.tabBarBuilds
}

// ContentView returns only the content view without tab bar (for compositor)
func (m *TabbedSidebar) ContentView() string {
	m.contentBuilds++
	contentHeight := m.height - 1
	if contentHeight <= 0 {
		return ""
	}
	return m.viewContent(contentHeight)
}

// ContentVersion folds the versions of every input to ContentView: the
// active tab (tabBarVersion bumps on every activeTab write) and each child
// model's own content version, which cover their mutation surfaces (see the
// contentVersion invariants on ChangesModel and ProjectTree).
func (m *TabbedSidebar) ContentVersion() uint64 {
	fp := common.FoldFingerprint(0, m.tabBarVersion)
	fp = common.FoldFingerprint(fp, m.changes.ContentVersion())
	fp = common.FoldFingerprint(fp, m.projectTree.ContentVersion())
	return fp
}

// ContentBuildCount reports how many times ContentView has been invoked.
// Test instrumentation for the compose-time skip gate; not for production use.
func (m *TabbedSidebar) ContentBuildCount() uint64 {
	return m.contentBuilds
}

// SetSize sets the sidebar size
func (m *TabbedSidebar) SetSize(width, height int) {
	m.width = width
	m.height = height

	contentHeight := height - 1 // -1 for tab bar
	if contentHeight < 0 {
		contentHeight = 0
	}
	m.changes.SetSize(width, contentHeight)
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
	m.changes.Blur()
	m.projectTree.Blur()
}

// Focused returns whether the sidebar is focused
func (m *TabbedSidebar) Focused() bool {
	return m.focused
}

// SetWorkspace sets the active workspace. It returns the Changes view's
// ahead/behind refresh command (nil for a no-op rebind); see ChangesModel.SetWorkspace.
func (m *TabbedSidebar) SetWorkspace(ws *data.Workspace) tea.Cmd {
	m.workspace = ws
	cmd := m.changes.SetWorkspace(ws)
	m.projectTree.SetWorkspace(ws)
	return cmd
}

// SetGitStatus sets the git status (forwards to changes view)
func (m *TabbedSidebar) SetGitStatus(status *git.StatusResult) {
	m.changes.SetGitStatus(status)
}

// SetScriptRunning forwards the run-script state for the workspace at root to
// the changes view, which renders the indicator.
func (m *TabbedSidebar) SetScriptRunning(root string, running bool) {
	m.changes.SetScriptRunning(root, running)
}

// RefreshAheadBehind re-fetches the ahead/behind badge for the active
// workspace (e.g. after a commit changes HEAD's distance from base).
func (m *TabbedSidebar) RefreshAheadBehind() tea.Cmd {
	return m.changes.refreshAheadBehind()
}

// ActiveTab returns the currently active tab
func (m *TabbedSidebar) ActiveTab() SidebarTab {
	return m.activeTab
}

// SetActiveTab sets the active tab
func (m *TabbedSidebar) SetActiveTab(tab SidebarTab) {
	m.activeTab = tab
	m.markTabBarDirty()
	m.updateFocus()
}

// NextTab switches to the next tab (circular)
func (m *TabbedSidebar) NextTab() {
	m.stepTab(1)
}

// PrevTab switches to the previous tab (circular)
func (m *TabbedSidebar) PrevTab() {
	m.stepTab(-1)
}

// stepTab cycles the active tab by delta positions through sidebarTabDefs.
func (m *TabbedSidebar) stepTab(delta int) {
	idx := 0
	for i, def := range sidebarTabDefs {
		if def.tab == m.activeTab {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(sidebarTabDefs)) % len(sidebarTabDefs)
	m.activeTab = sidebarTabDefs[idx].tab
	m.markTabBarDirty()
	m.updateFocus()
}

// Changes returns the changes model (for direct access if needed)
func (m *TabbedSidebar) Changes() *ChangesModel {
	return m.changes
}

// ProjectTree returns the project tree model (for direct access if needed)
func (m *TabbedSidebar) ProjectTree() *ProjectTree {
	return m.projectTree
}
