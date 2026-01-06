package dashboard

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/keymap"
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

type actionButtonID int

const (
	actionMonitor actionButtonID = iota
	actionHelp
	actionKeymap
	actionQuit
)

type actionButton struct {
	id actionButtonID
	x0 int
	x1 int
}

// Model is the Bubbletea model for the dashboard pane
type Model struct {
	// Data
	projects    []data.Project
	rows        []Row
	activeRoot  string // Currently active worktree root
	statusCache map[string]*git.StatusResult

	// UI state
	cursor        int
	focused       bool
	width         int
	height        int
	offsetX       int
	offsetY       int
	filterDirty   bool // Only show dirty worktrees
	scrollOffset  int
	actionButtons []actionButton

	// Loading state
	loadingStatus     map[string]bool           // Worktrees currently loading git status
	creatingWorktrees map[string]*data.Worktree // Worktrees currently being created
	deletingWorktrees map[string]bool           // Worktrees currently being deleted
	spinnerFrame      int                       // Current spinner animation frame
	spinnerActive     bool                      // Whether spinner ticks are active

	// Styles
	styles common.Styles

	keymap keymap.KeyMap
}

// New creates a new dashboard model
func New(km keymap.KeyMap) *Model {
	return &Model{
		projects:          []data.Project{},
		rows:              []Row{},
		statusCache:       make(map[string]*git.StatusResult),
		loadingStatus:     make(map[string]bool),
		creatingWorktrees: make(map[string]*data.Worktree),
		deletingWorktrees: make(map[string]bool),
		cursor:            0,
		focused:           true,
		styles:            common.DefaultStyles(),
		keymap:            km,
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
		case key.Matches(msg, m.keymap.DashboardDown):
			m.moveCursor(1)
		case key.Matches(msg, m.keymap.DashboardUp):
			m.moveCursor(-1)
		case key.Matches(msg, m.keymap.DashboardEnter):
			return m, m.handleEnter()
		case key.Matches(msg, m.keymap.DashboardNewWorktree):
			return m, m.handleNew()
		case key.Matches(msg, m.keymap.DashboardDelete):
			return m, m.handleDelete()
		case key.Matches(msg, m.keymap.DashboardToggle):
			m.toggleFilter()
		case key.Matches(msg, m.keymap.DashboardRefresh):
			return m, m.refresh()
		case key.Matches(msg, m.keymap.DashboardBottom):
			// Jump to last selectable row
			if idx := m.findSelectableRow(len(m.rows)-1, -1); idx != -1 {
				m.cursor = idx
			}
		case key.Matches(msg, m.keymap.DashboardTop):
			// Jump to first selectable row
			if idx := m.findSelectableRow(0, 1); idx != -1 {
				m.cursor = idx
			}
		}

	case tea.MouseMsg:
		if !m.focused {
			return m, nil
		}

		localY := msg.Y - m.offsetY
		localX := msg.X - m.offsetX
		if localX < 0 || localY < 0 || localX >= m.width || localY >= m.height {
			return m, nil
		}

		contentX, contentY, ok := m.contentCoords(localX, localY)
		if !ok {
			return m, nil
		}

		if msg.Action == tea.MouseActionPress {
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				m.moveCursor(-1)
			case tea.MouseButtonWheelDown:
				m.moveCursor(1)
			case tea.MouseButtonLeft:
				if contentY == m.actionBarY() {
					if action, ok := m.actionButtonAt(contentX); ok {
						return m, m.handleAction(action)
					}
				}
				rowIdx, ok := m.rowIndexAt(contentX, contentY)
				if !ok {
					return m, nil
				}
				if rowIdx < 0 || rowIdx >= len(m.rows) {
					return m, nil
				}
				if !isSelectable(m.rows[rowIdx].Type) {
					return m, nil
				}
				m.cursor = rowIdx
				return m, m.handleEnter()
			}
		}

	case spinnerTickMsg:
		// Advance spinner frame if we have loading items
		if len(m.loadingStatus) > 0 || len(m.creatingWorktrees) > 0 || len(m.deletingWorktrees) > 0 {
			m.spinnerFrame++
			cmds = append(cmds, m.tickSpinner())
		} else {
			m.spinnerActive = false
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
		if cmd := m.startSpinnerIfNeeded(); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case messages.GitStatusResult:
		// Remove from loading, add to cache
		delete(m.loadingStatus, msg.Root)
		if msg.Err == nil {
			m.statusCache[msg.Root] = msg.Status
		}
		// Rebuild rows if dirty filter is active (status change may affect visibility)
		if m.filterDirty {
			m.rebuildRows()
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

// startSpinnerIfNeeded starts spinner ticks if we have pending activity.
func (m *Model) startSpinnerIfNeeded() tea.Cmd {
	if m.spinnerActive {
		return nil
	}
	if len(m.loadingStatus) == 0 && len(m.creatingWorktrees) == 0 && len(m.deletingWorktrees) == 0 {
		return nil
	}
	m.spinnerActive = true
	return m.tickSpinner()
}

// View renders the dashboard
func (m *Model) View() string {
	var b strings.Builder

	// Header
	if m.filterDirty {
		b.WriteString(m.styles.StatusDirty.Render("[dirty]"))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(m.renderActionBar())
	b.WriteString("\n")

	// Calculate visible area (inner height minus header + help)
	innerHeight := m.height - 2
	if innerHeight < 0 {
		innerHeight = 0
	}
	contentWidth := m.width - 4
	if contentWidth < 1 {
		contentWidth = 1
	}
	_, helpHeight, visibleHeight := m.layoutHeights(contentWidth)
	keys := m.helpKeys()

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
		m.styles.HelpKey.Render(keys.nav) + m.styles.HelpDesc.Render(":nav"),
		m.styles.HelpKey.Render(keys.selectKey) + m.styles.HelpDesc.Render(":select"),
		m.styles.HelpKey.Render(keys.newKey) + m.styles.HelpDesc.Render(":new"),
		m.styles.HelpKey.Render(keys.deleteKey) + m.styles.HelpDesc.Render(":delete"),
		m.styles.HelpKey.Render(keys.filterKey) + m.styles.HelpDesc.Render(":filter"),
		m.styles.HelpKey.Render(keys.refreshKey) + m.styles.HelpDesc.Render(":refresh"),
		m.styles.HelpKey.Render(keys.monitorKey) + m.styles.HelpDesc.Render(":monitor"),
		m.styles.HelpKey.Render(keys.helpKey) + m.styles.HelpDesc.Render(":help"),
	}
	help := strings.Join(helpItems, "  ")

	// Pad to the inner pane height (border excluded), reserving the help line.
	contentHeight := strings.Count(b.String(), "\n") + 1
	targetHeight := innerHeight - helpHeight
	if targetHeight < 0 {
		targetHeight = 0
	}
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
			style = m.styles.HomeRow.
				Bold(true).
				Foreground(common.ColorForeground).
				Background(common.ColorSelection)
		}
		return cursor + style.Render("["+common.Icons.Home+" Home]")

	case RowAddProject:
		style := m.styles.CreateButton
		if selected {
			style = m.styles.SelectedRow
		}
		return cursor + style.Render(common.Icons.Add+" Add Project")

	case RowProject:
		// Project headers are uppercase - selectable to access main branch
		// Remove MarginTop from style to keep cursor on same line as text
		// Add spacing as newline prefix instead
		style := m.styles.ProjectHeader.MarginTop(0)
		if selected {
			style = style.
				Bold(true).
				Foreground(common.ColorForeground).
				Background(common.ColorSelection)
		}
		return "\n" + cursor + style.Render(row.Project.Name)

	case RowWorktree:
		name := row.Worktree.Name
		status := ""

		// Check deletion state first
		if m.deletingWorktrees[row.Worktree.Root] {
			frame := common.SpinnerFrame(m.spinnerFrame)
			status = " " + m.styles.StatusPending.Render(frame+" deleting")
		} else if _, ok := m.creatingWorktrees[row.Worktree.Root]; ok {
			frame := common.SpinnerFrame(m.spinnerFrame)
			status = " " + m.styles.StatusPending.Render(frame+" creating")
		} else if m.loadingStatus[row.Worktree.Root] {
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
		return cursor + style.Render(common.Icons.Add+" New")

	case RowSpacer:
		return ""
	}

	return ""
}

// SetWorktreeCreating marks a worktree as creating (or clears it).
func (m *Model) SetWorktreeCreating(wt *data.Worktree, creating bool) tea.Cmd {
	if wt == nil {
		return nil
	}
	if creating {
		m.creatingWorktrees[wt.Root] = wt
		m.rebuildRows()
		return m.startSpinnerIfNeeded()
	}
	delete(m.creatingWorktrees, wt.Root)
	m.rebuildRows()
	return nil
}

// SetWorktreeDeleting marks a worktree as deleting (or clears it).
func (m *Model) SetWorktreeDeleting(root string, deleting bool) tea.Cmd {
	if deleting {
		m.deletingWorktrees[root] = true
		return m.startSpinnerIfNeeded()
	}
	delete(m.deletingWorktrees, root)
	return nil
}

// rebuildRows rebuilds the row list from projects
func (m *Model) rebuildRows() {
	m.rows = []Row{
		{Type: RowHome},
		{Type: RowAddProject},
	}

	for i := range m.projects {
		project := &m.projects[i]
		existingRoots := make(map[string]bool)

		m.rows = append(m.rows, Row{
			Type:    RowProject,
			Project: project,
		})

		for j := range project.Worktrees {
			wt := &project.Worktrees[j]
			existingRoots[wt.Root] = true

			// Hide main branch - users access via project row
			if wt.IsMainBranch() || wt.IsPrimaryCheckout() {
				continue
			}

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

		for _, wt := range m.creatingWorktrees {
			if wt == nil || wt.Repo != project.Path {
				continue
			}
			if existingRoots[wt.Root] {
				continue
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
	return rt != RowSpacer
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

type dashboardHelpKeys struct {
	nav        string
	selectKey  string
	newKey     string
	deleteKey  string
	filterKey  string
	refreshKey string
	monitorKey string
	helpKey    string
}

func (m *Model) helpKeys() dashboardHelpKeys {
	return dashboardHelpKeys{
		nav:        keymap.PairHint(m.keymap.DashboardDown, m.keymap.DashboardUp),
		selectKey:  keymap.PrimaryKey(m.keymap.DashboardEnter),
		newKey:     keymap.PrimaryKey(m.keymap.DashboardNewWorktree),
		deleteKey:  keymap.PrimaryKey(m.keymap.DashboardDelete),
		filterKey:  keymap.PrimaryKey(m.keymap.DashboardToggle),
		refreshKey: keymap.PrimaryKey(m.keymap.DashboardRefresh),
		monitorKey: keymap.BindingHint(m.keymap.MonitorToggle),
		helpKey:    keymap.BindingHint(m.keymap.Help),
	}
}

func helpTextFromKeys(keys dashboardHelpKeys) string {
	return strings.Join([]string{
		keys.nav + ":nav",
		keys.selectKey + ":select",
		keys.newKey + ":new",
		keys.deleteKey + ":delete",
		keys.filterKey + ":filter",
		keys.refreshKey + ":refresh",
		keys.monitorKey + ":monitor",
		keys.helpKey + ":help",
	}, "  ")
}

func (m *Model) layoutHeights(contentWidth int) (headerHeight int, helpHeight int, visibleHeight int) {
	innerHeight := m.height - 2
	if innerHeight < 0 {
		innerHeight = 0
	}
	headerHeight = 1 // blank line
	if m.filterDirty {
		headerHeight++
	}
	headerHeight++ // action bar
	helpHeight = calcHelpHeight(contentWidth, helpTextFromKeys(m.helpKeys()))
	visibleHeight = innerHeight - headerHeight - helpHeight
	if visibleHeight < 1 {
		visibleHeight = 1
	}
	return headerHeight, helpHeight, visibleHeight
}

func (m *Model) actionBarY() int {
	y := 1
	if m.filterDirty {
		y++
	}
	return y
}

func (m *Model) renderActionBar() string {
	contentWidth := m.width - 4
	if contentWidth < 1 {
		contentWidth = 1
	}

	type btnSpec struct {
		id    actionButtonID
		label string
	}

	buttons := []btnSpec{
		{id: actionMonitor, label: actionLabel("Monitor", bindingShort(m.keymap.MonitorToggle))},
		{id: actionHelp, label: actionLabel("Help", bindingShort(m.keymap.Help))},
		{id: actionKeymap, label: actionLabel("Keymap", bindingShort(m.keymap.KeymapEditor))},
		{id: actionQuit, label: actionLabel("Quit", bindingShort(m.keymap.Quit))},
	}

	m.actionButtons = m.actionButtons[:0]
	var parts []string
	x := 0
	for _, btn := range buttons {
		width := lipgloss.Width(btn.label)
		if x > 0 {
			if x+2+width > contentWidth {
				break
			}
			parts = append(parts, "  ")
			x += 2
		} else if width > contentWidth {
			break
		}

		m.actionButtons = append(m.actionButtons, actionButton{id: btn.id, x0: x, x1: x + width})
		parts = append(parts, m.styles.HelpKey.Render(btn.label))
		x += width
	}

	line := strings.Join(parts, "")
	return ansi.Truncate(line, contentWidth, "")
}

func actionLabel(title, hint string) string {
	if hint == "" {
		return "[ " + title + " ]"
	}
	return "[ " + title + " " + hint + " ]"
}

func bindingShort(binding key.Binding) string {
	return keymap.BindingHint(binding)
}

func (m *Model) actionButtonAt(x int) (actionButtonID, bool) {
	for _, btn := range m.actionButtons {
		if x >= btn.x0 && x < btn.x1 {
			return btn.id, true
		}
	}
	return 0, false
}

func (m *Model) handleAction(action actionButtonID) tea.Cmd {
	switch action {
	case actionMonitor:
		return func() tea.Msg { return messages.ToggleMonitor{} }
	case actionHelp:
		return func() tea.Msg { return messages.ToggleHelpOverlay{} }
	case actionKeymap:
		return func() tea.Msg { return messages.ShowKeymapEditor{} }
	case actionQuit:
		return func() tea.Msg { return messages.ShowQuitDialog{} }
	default:
		return nil
	}
}

func calcHelpHeight(contentWidth int, helpText string) int {
	if contentWidth < 1 {
		contentWidth = 1
	}
	height := (len(helpText) + contentWidth - 1) / contentWidth
	if height < 1 {
		height = 1
	}
	return height
}

func rowLineCount(row Row) int {
	switch row.Type {
	case RowProject:
		return 2
	default:
		return 1
	}
}

func (m *Model) contentCoords(localX, localY int) (int, int, bool) {
	borderTop := 1
	borderLeft := 1
	borderRight := 1
	paddingLeft := 1
	paddingRight := 1

	contentX := localX - borderLeft - paddingLeft
	contentY := localY - borderTop

	contentWidth := m.width - (borderLeft + borderRight + paddingLeft + paddingRight)
	innerHeight := m.height - 2
	if contentWidth <= 0 || innerHeight <= 0 {
		return -1, -1, false
	}
	if contentX < 0 || contentX >= contentWidth {
		return -1, -1, false
	}
	if contentY < 0 || contentY >= innerHeight {
		return -1, -1, false
	}
	return contentX, contentY, true
}

func (m *Model) rowIndexAt(contentX, contentY int) (int, bool) {
	contentWidth := m.width - 4
	innerHeight := m.height - 2
	if contentWidth <= 0 || innerHeight <= 0 {
		return -1, false
	}
	if contentX < 0 || contentX >= contentWidth {
		return -1, false
	}
	if contentY < 0 || contentY >= innerHeight {
		return -1, false
	}

	headerHeight, helpHeight, _ := m.layoutHeights(contentWidth)
	rowAreaHeight := innerHeight - headerHeight - helpHeight
	if rowAreaHeight < 1 {
		rowAreaHeight = 1
	}
	if contentY < headerHeight || contentY >= headerHeight+rowAreaHeight {
		return -1, false
	}

	rowY := contentY - headerHeight
	line := 0
	for i := m.scrollOffset; i < len(m.rows); i++ {
		if line >= rowAreaHeight {
			break
		}
		rowLines := rowLineCount(m.rows[i])
		if rowY < line+rowLines {
			return i, true
		}
		line += rowLines
	}

	return -1, false
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
	case RowProject:
		// Find and activate the main/primary worktree for this project
		var mainWT *data.Worktree
		for i := range row.Project.Worktrees {
			wt := &row.Project.Worktrees[i]
			if wt.IsMainBranch() || wt.IsPrimaryCheckout() {
				mainWT = wt
				break
			}
		}
		if mainWT != nil {
			return func() tea.Msg {
				return messages.WorktreeActivated{
					Project:  row.Project,
					Worktree: mainWT,
				}
			}
		}
		return nil
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

// refresh requests a dashboard refresh
func (m *Model) refresh() tea.Cmd {
	return func() tea.Msg { return messages.RefreshDashboard{} }
}

// SetSize sets the dashboard size
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SetKeyMap updates the keymap used for hints and input.
func (m *Model) SetKeyMap(km keymap.KeyMap) {
	m.keymap = km
}

// SetOffset sets the top-left origin for mouse hit testing.
func (m *Model) SetOffset(x, y int) {
	m.offsetX = x
	m.offsetY = y
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
