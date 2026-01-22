package dashboard

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

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

// toolbarButtonKind identifies toolbar buttons
type toolbarButtonKind int

const (
	toolbarHelp toolbarButtonKind = iota
	toolbarMonitor
	toolbarDelete
	toolbarRemove
	toolbarSettings
)

// toolbarButton tracks a clickable button in the toolbar
type toolbarButton struct {
	kind   toolbarButtonKind
	region common.HitRegion
}

// Model is the Bubbletea model for the dashboard pane
type Model struct {
	// Data
	projects    []data.Project
	rows        []Row
	activeRoot  string // Currently active worktree root
	statusCache map[string]*git.StatusResult

	// UI state
	cursor          int
	focused         bool
	width           int
	height          int
	scrollOffset    int
	canFocusRight   bool
	showKeymapHints bool
	toolbarHits     []toolbarButton // Clickable toolbar buttons
	toolbarY        int             // Y position of toolbar in content coordinates
	toolbarFocused  bool            // Whether toolbar actions are focused
	toolbarIndex    int             // Focused toolbar action index

	// Loading state
	loadingStatus     map[string]bool           // Worktrees currently loading git status
	creatingWorktrees map[string]*data.Worktree // Worktrees currently being created
	deletingWorktrees map[string]bool           // Worktrees currently being deleted
	spinnerFrame      int                       // Current spinner animation frame
	spinnerActive     bool                      // Whether spinner ticks are active

	// Styles
	styles common.Styles
}

// New creates a new dashboard model
func New() *Model {
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
	}
}

// SetCanFocusRight controls whether focus-right hints should be shown.
func (m *Model) SetCanFocusRight(can bool) {
	m.canFocusRight = can
}

// SetShowKeymapHints controls whether helper text is rendered.
func (m *Model) SetShowKeymapHints(show bool) {
	m.showKeymapHints = show
}

// SetStyles updates the component's styles (for theme changes).
func (m *Model) SetStyles(styles common.Styles) {
	m.styles = styles
}

// Init initializes the dashboard
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		if !m.focused {
			return m, nil
		}
		delta := common.ScrollDeltaForHeight(m.visibleHeight(), 10) // ~10% of visible
		if msg.Button == tea.MouseWheelUp {
			m.moveCursor(-delta)
			return m, nil
		}
		if msg.Button == tea.MouseWheelDown {
			m.moveCursor(delta)
			return m, nil
		}

	case tea.MouseClickMsg:
		if !m.focused {
			return m, nil
		}
		if msg.Button == tea.MouseLeft {
			// Check toolbar clicks first
			if cmd := m.handleToolbarClick(msg.X, msg.Y); cmd != nil {
				return m, cmd
			}

			// Then check row clicks
			idx, ok := m.rowIndexAt(msg.X, msg.Y)
			if !ok {
				return m, nil
			}
			if idx < 0 || idx >= len(m.rows) {
				return m, nil
			}
			if !isSelectable(m.rows[idx].Type) {
				return m, nil
			}
			m.toolbarFocused = false
			m.cursor = idx
			return m, m.handleEnter()
		}

	case tea.KeyPressMsg:
		if !m.focused {
			return m, nil
		}

		toolbarItems := m.toolbarVisibleItems(m.toolbarItems())
		if m.toolbarFocused {
			if len(toolbarItems) == 0 {
				m.toolbarFocused = false
				break
			}
			switch {
			case key.Matches(msg, key.NewBinding(key.WithKeys("left", "h"))):
				m.toolbarIndex = (m.toolbarIndex - 1 + len(toolbarItems)) % len(toolbarItems)
			case key.Matches(msg, key.NewBinding(key.WithKeys("right", "l"))):
				m.toolbarIndex = (m.toolbarIndex + 1) % len(toolbarItems)
			case key.Matches(msg, key.NewBinding(key.WithKeys("up", "k"))):
				m.toolbarFocused = false
				if last := m.findSelectableRow(len(m.rows)-1, -1); last != -1 {
					m.cursor = last
				}
			case key.Matches(msg, key.NewBinding(key.WithKeys("down", "j"))):
				m.toolbarFocused = false
				if last := m.findSelectableRow(len(m.rows)-1, -1); last != -1 {
					m.cursor = last
				}
			case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
				return m, m.toolbarCommand(toolbarItems[m.toolbarIndex].kind)
			}
			return m, nil
		}

		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
			last := m.findSelectableRow(len(m.rows)-1, -1)
			if last != -1 && m.cursor == last && len(toolbarItems) > 0 {
				m.toolbarFocused = true
				m.toolbarIndex = 0
			} else {
				m.moveCursor(1)
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
			m.moveCursor(-1)
		case key.Matches(msg, key.NewBinding(key.WithKeys("pgdown", "ctrl+d"))):
			// Half-page scroll to maintain context overlap
			delta := m.visibleHeight() / 2
			if delta < 1 {
				delta = 1
			}
			m.moveCursor(delta)
		case key.Matches(msg, key.NewBinding(key.WithKeys("pgup", "ctrl+u"))):
			// Half-page scroll to maintain context overlap
			delta := m.visibleHeight() / 2
			if delta < 1 {
				delta = 1
			}
			m.moveCursor(-delta)
		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			return m, m.handleEnter()
		case key.Matches(msg, key.NewBinding(key.WithKeys("D"))):
			return m, m.handleDelete()
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

	case messages.WorktreeActivated:
		if msg.Worktree != nil {
			m.activeRoot = msg.Worktree.Root
		}

	case messages.ShowWelcome:
		m.activeRoot = ""
	}

	return m, tea.Batch(cmds...)
}

// View renders the dashboard
func (m *Model) View() string {
	var b strings.Builder

	// Header
	b.WriteString("\n")

	// Calculate visible area (inner height minus header + toolbar + help)
	innerHeight := m.height - 2
	if innerHeight < 0 {
		innerHeight = 0
	}
	headerHeight := 1 // trailing blank line
	// Calculate help height based on content width (pane width minus border and padding)
	contentWidth := m.width - 4
	if contentWidth < 1 {
		contentWidth = 1
	}
	helpLines := m.helpLines(contentWidth)
	if !m.showKeymapHints {
		helpLines = nil
	}
	helpHeight := len(helpLines)
	toolbarHeight := m.toolbarHeight()
	visibleHeight := innerHeight - headerHeight - toolbarHeight - helpHeight
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

	// Pad to the inner pane height (border excluded), reserving toolbar and help lines.
	contentHeight := strings.Count(b.String(), "\n") + 1
	targetHeight := innerHeight - toolbarHeight - helpHeight
	if targetHeight < 0 {
		targetHeight = 0
	}
	paddedHeight := targetHeight
	if contentHeight > paddedHeight {
		paddedHeight = contentHeight
	}
	if targetHeight > contentHeight {
		b.WriteString(strings.Repeat("\n", targetHeight-contentHeight))
	}

	// Render toolbar and track its Y position for click handling
	// toolbarY is the first line of the toolbar
	toolbar := m.renderToolbar()
	m.toolbarY = paddedHeight - 1
	b.WriteString(toolbar)
	b.WriteString("\n")

	// Help lines
	if len(helpLines) > 0 {
		b.WriteString(strings.Join(helpLines, "\n"))
	}

	// Return raw content - buildBorderedPane in app.go handles truncation
	return b.String()
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

// visibleHeight returns the number of visible rows in the dashboard
func (m *Model) visibleHeight() int {
	innerHeight := m.height - 2
	if innerHeight < 0 {
		innerHeight = 0
	}
	headerHeight := 1 // trailing blank line
	contentWidth := m.width - 4
	if contentWidth < 1 {
		contentWidth = 1
	}
	helpLines := m.helpLines(contentWidth)
	if !m.showKeymapHints {
		helpLines = nil
	}
	helpHeight := len(helpLines)
	toolbarHeight := m.toolbarHeight()
	visibleHeight := innerHeight - headerHeight - toolbarHeight - helpHeight
	if visibleHeight < 1 {
		visibleHeight = 1
	}
	return visibleHeight
}
