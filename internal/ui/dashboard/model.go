package dashboard

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// spinnerTickMsg is sent to update the spinner animation
type spinnerTickMsg struct{}

// spinnerInterval is how often the spinner updates
const spinnerInterval = 80 * time.Millisecond

// RowType identifies the type of row in the dashboard
type RowType int

const (
	RowHome RowType = iota
	RowAddProject
	RowProject
	RowWorktree
	RowCreate
	RowSpacer
)

// Row represents a single row in the dashboard
type Row struct {
	Type     RowType
	Project  *data.Project
	Worktree *data.Worktree
}

// Model is the Bubbletea model for the dashboard pane
type Model struct {
	// Data
	projects    []data.Project
	rows        []Row
	activeRoot  string // Currently active worktree root
	statusCache map[string]*git.StatusResult

	// UI state
	cursor       int
	focused      bool
	width        int
	height       int
	filterDirty  bool // Only show dirty worktrees
	scrollOffset int

	// Loading state
	loadingStatus map[string]bool // Worktrees currently loading git status
	spinnerFrame  int             // Current spinner animation frame

	// Styles
	styles common.Styles
}

// New creates a new dashboard model
func New() *Model {
	return &Model{
		projects:      []data.Project{},
		rows:          []Row{},
		statusCache:   make(map[string]*git.StatusResult),
		loadingStatus: make(map[string]bool),
		cursor:        0,
		focused:       true,
		styles:        common.DefaultStyles(),
	}
}

// Init initializes the dashboard
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if !m.focused {
			return m, nil
		}

		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
			m.moveCursor(1)
		case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
			m.moveCursor(-1)
		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			return m, m.handleEnter()
		case key.Matches(msg, key.NewBinding(key.WithKeys("n"))):
			return m, m.handleNew()
		case key.Matches(msg, key.NewBinding(key.WithKeys("d", "D"))):
			return m, m.handleDelete()
		case key.Matches(msg, key.NewBinding(key.WithKeys("a"))):
			return m, m.handleAddProject()
		case key.Matches(msg, key.NewBinding(key.WithKeys("f"))):
			m.toggleFilter()
		case key.Matches(msg, key.NewBinding(key.WithKeys("r"))):
			return m, m.refresh()
		case key.Matches(msg, key.NewBinding(key.WithKeys("G"))):
			// Jump to last selectable row
			if idx := m.findSelectableRow(len(m.rows)-1, -1); idx != -1 {
				m.cursor = idx
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("g"))):
			// Jump to first selectable row
			if idx := m.findSelectableRow(0, 1); idx != -1 {
				m.cursor = idx
			}
		}

	case spinnerTickMsg:
		// Advance spinner frame if we have loading items
		if len(m.loadingStatus) > 0 {
			m.spinnerFrame++
			cmds = append(cmds, m.tickSpinner())
		}

	case messages.ProjectsLoaded:
		m.projects = msg.Projects
		m.rebuildRows()
		// Mark all worktrees as loading status
		for _, p := range m.projects {
			for _, wt := range p.Worktrees {
				if _, ok := m.statusCache[wt.Root]; !ok {
					m.loadingStatus[wt.Root] = true
				}
			}
		}
		// Start spinner if we have loading items
		if len(m.loadingStatus) > 0 {
			cmds = append(cmds, m.tickSpinner())
		}

	case messages.GitStatusResult:
		// Remove from loading, add to cache
		delete(m.loadingStatus, msg.Root)
		if msg.Err == nil {
			m.statusCache[msg.Root] = msg.Status
		}

	case messages.WorktreeActivated:
		if msg.Worktree != nil {
			m.activeRoot = msg.Worktree.Root
		}
	}

	return m, tea.Batch(cmds...)
}

// tickSpinner returns a command that ticks the spinner
func (m *Model) tickSpinner() tea.Cmd {
	return tea.Tick(spinnerInterval, func(t time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

// View renders the dashboard
func (m *Model) View() string {
	var b strings.Builder

	// Header
	header := m.styles.Title.Render("amux")
	if m.filterDirty {
		header += m.styles.StatusDirty.Render(" [dirty]")
	}
	b.WriteString(header)
	b.WriteString("\n\n")

	// Calculate visible area
	visibleHeight := m.height - 6 // Account for header, help, borders
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	// Adjust scroll offset to keep cursor visible
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	}
	if m.cursor >= m.scrollOffset+visibleHeight {
		m.scrollOffset = m.cursor - visibleHeight + 1
	}

	// Rows
	for i, row := range m.rows {
		if i < m.scrollOffset {
			continue
		}
		if i >= m.scrollOffset+visibleHeight {
			break
		}
		line := m.renderRow(row, i == m.cursor)
		b.WriteString(line)
		b.WriteString("\n")
	}

	// Help bar with styled keys
	helpItems := []string{
		m.styles.HelpKey.Render("j/k") + m.styles.HelpDesc.Render(":nav"),
		m.styles.HelpKey.Render("enter") + m.styles.HelpDesc.Render(":select"),
		m.styles.HelpKey.Render("n") + m.styles.HelpDesc.Render(":new"),
		m.styles.HelpKey.Render("a") + m.styles.HelpDesc.Render(":add"),
		m.styles.HelpKey.Render("ctrl+t") + m.styles.HelpDesc.Render(":agent"),
	}
	help := strings.Join(helpItems, "  ")

	// Calculate remaining height and add padding
	// Account for: border (2 lines) + help line (1)
	contentHeight := strings.Count(b.String(), "\n") + 1
	targetHeight := m.height - 4 // 2 for border, 1 for help, 1 for safety
	if targetHeight > contentHeight {
		b.WriteString(strings.Repeat("\n", targetHeight-contentHeight))
	}
	b.WriteString(help)

	// Apply pane styling
	style := m.styles.Pane
	if m.focused {
		style = m.styles.FocusedPane
	}

	return style.Width(m.width - 2).Render(b.String())
}

// renderRow renders a single dashboard row
func (m *Model) renderRow(row Row, selected bool) string {
	cursor := common.Icons.CursorEmpty + " "
	if selected {
		cursor = common.Icons.Cursor + " "
	}

	switch row.Type {
	case RowHome:
		style := m.styles.HomeRow
		if selected {
			style = m.styles.SelectedRow
		}
		return cursor + style.Render("[" + common.Icons.Home + " home]")

	case RowAddProject:
		style := m.styles.AddProjectRow
		if selected {
			style = m.styles.SelectedRow
		}
		return cursor + style.Render("[" + common.Icons.Add + " Add Project]")

	case RowProject:
		// Project headers are uppercase and muted
		return "  " + m.styles.ProjectHeader.Render(strings.ToUpper(row.Project.Name))

	case RowWorktree:
		name := row.Worktree.Name
		status := ""

		// Check loading state first
		if m.loadingStatus[row.Worktree.Root] {
			// Show spinner while loading
			frame := common.SpinnerFrame(m.spinnerFrame)
			status = " " + m.styles.StatusPending.Render(frame)
		} else if s, ok := m.statusCache[row.Worktree.Root]; ok {
			if s.Clean {
				status = " " + m.styles.StatusClean.Render(common.Icons.Clean)
			} else {
				count := len(s.Files)
				status = " " + m.styles.StatusDirty.Render(common.Icons.Dirty+strconv.Itoa(count))
			}
		}

		// Determine row style based on selection and active state
		style := m.styles.WorktreeRow
		if selected {
			style = m.styles.SelectedRow
		} else if row.Worktree.Root == m.activeRoot {
			style = m.styles.ActiveWorktree
		}
		return cursor + style.Render(name) + status

	case RowCreate:
		style := m.styles.CreateButton
		if selected {
			style = m.styles.SelectedRow
		}
		return cursor + style.Render(common.Icons.Add + " New")

	case RowSpacer:
		return ""
	}

	return ""
}

// rebuildRows rebuilds the row list from projects
func (m *Model) rebuildRows() {
	m.rows = []Row{
		{Type: RowHome},
		{Type: RowAddProject},
	}

	for i := range m.projects {
		project := &m.projects[i]

		m.rows = append(m.rows, Row{
			Type:    RowProject,
			Project: project,
		})

		for j := range project.Worktrees {
			wt := &project.Worktrees[j]

			// Filter dirty worktrees if filter is enabled
			if m.filterDirty {
				if s, ok := m.statusCache[wt.Root]; ok && s.Clean {
					continue
				}
			}

			m.rows = append(m.rows, Row{
				Type:     RowWorktree,
				Project:  project,
				Worktree: wt,
			})
		}

		m.rows = append(m.rows, Row{
			Type:    RowCreate,
			Project: project,
		})

		m.rows = append(m.rows, Row{Type: RowSpacer})
	}

	// Clamp cursor
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// isSelectable returns whether a row type can be selected
func isSelectable(rt RowType) bool {
	return rt != RowProject && rt != RowSpacer
}

// findSelectableRow finds a selectable row starting from 'from' in direction 'dir'.
// Returns -1 if none found.
func (m *Model) findSelectableRow(from, dir int) int {
	if dir == 0 {
		dir = 1 // Default to forward
	}
	for i := from; i >= 0 && i < len(m.rows); i += dir {
		if isSelectable(m.rows[i].Type) {
			return i
		}
	}
	return -1
}

// moveCursor moves the cursor by delta, skipping non-selectable rows
func (m *Model) moveCursor(delta int) {
	if len(m.rows) == 0 {
		return
	}

	target := m.cursor + delta

	// Determine search direction
	dir := delta
	if dir == 0 {
		dir = 1 // For delta==0, search forward first
	}

	newCursor := m.findSelectableRow(target, dir)

	// If not found and delta was 0, try the other direction
	if newCursor == -1 && delta == 0 {
		newCursor = m.findSelectableRow(target, -1)
	}

	if newCursor != -1 {
		m.cursor = newCursor
	}
}

// toggleFilter toggles the dirty filter
func (m *Model) toggleFilter() {
	// Remember current row for position restoration
	var currentItem interface{}
	if m.cursor < len(m.rows) {
		row := m.rows[m.cursor]
		if row.Worktree != nil {
			currentItem = row.Worktree.Root
		} else if row.Project != nil {
			currentItem = row.Project.Path
		}
	}

	m.filterDirty = !m.filterDirty
	m.rebuildRows()

	// Try to restore cursor position to the same item
	if currentItem != nil {
		for i, row := range m.rows {
			if row.Worktree != nil && row.Worktree.Root == currentItem {
				m.cursor = i
				return
			}
			if row.Project != nil && row.Project.Path == currentItem {
				m.cursor = i
				return
			}
		}
	}

	// If item not found, clamp cursor to valid range
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// handleEnter handles the enter key
func (m *Model) handleEnter() tea.Cmd {
	if m.cursor >= len(m.rows) {
		return nil
	}

	row := m.rows[m.cursor]
	switch row.Type {
	case RowHome:
		return func() tea.Msg { return messages.ShowWelcome{} }
	case RowAddProject:
		return func() tea.Msg { return messages.ShowAddProjectDialog{} }
	case RowWorktree:
		return func() tea.Msg {
			return messages.WorktreeActivated{
				Project:  row.Project,
				Worktree: row.Worktree,
			}
		}
	case RowCreate:
		return func() tea.Msg {
			return messages.ShowCreateWorktreeDialog{Project: row.Project}
		}
	}

	return nil
}

// handleNew handles the new worktree key
func (m *Model) handleNew() tea.Cmd {
	// If cursor is on a project-related row, create for that project
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		row := m.rows[m.cursor]
		if row.Project != nil {
			return func() tea.Msg {
				return messages.ShowCreateWorktreeDialog{Project: row.Project}
			}
		}
	}

	// Otherwise, if there are projects, use the first one
	if len(m.projects) > 0 {
		return func() tea.Msg {
			return messages.ShowCreateWorktreeDialog{Project: &m.projects[0]}
		}
	}

	return nil
}

// handleDelete handles the delete key
func (m *Model) handleDelete() tea.Cmd {
	if m.cursor >= len(m.rows) {
		return nil
	}

	row := m.rows[m.cursor]
	if row.Type == RowWorktree && row.Worktree != nil {
		return func() tea.Msg {
			return messages.ShowDeleteWorktreeDialog{
				Project:  row.Project,
				Worktree: row.Worktree,
			}
		}
	}

	return nil
}

// handleAddProject handles the add project key
func (m *Model) handleAddProject() tea.Cmd {
	return func() tea.Msg { return messages.ShowAddProjectDialog{} }
}

// refresh requests a dashboard refresh
func (m *Model) refresh() tea.Cmd {
	return func() tea.Msg { return messages.RefreshDashboard{} }
}

// SetSize sets the dashboard size
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Focus sets the focus state
func (m *Model) Focus() {
	m.focused = true
}

// Blur removes focus
func (m *Model) Blur() {
	m.focused = false
}

// Focused returns whether the dashboard is focused
func (m *Model) Focused() bool {
	return m.focused
}

// SetProjects sets the projects list
func (m *Model) SetProjects(projects []data.Project) {
	m.projects = projects
	m.rebuildRows()
}

// SelectedRow returns the currently selected row
func (m *Model) SelectedRow() *Row {
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		return &m.rows[m.cursor]
	}
	return nil
}

// Projects returns the current projects
func (m *Model) Projects() []data.Project {
	return m.projects
}
