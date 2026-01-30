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

// SpinnerTickMsg is sent to update the spinner animation
type SpinnerTickMsg struct{}

// spinnerInterval is how often the spinner updates
const spinnerInterval = 80 * time.Millisecond

// RowType identifies the type of row in the dashboard
type RowType int

const (
	RowHome RowType = iota
	RowAddProject
	RowProject
	RowWorkspace
	RowCreate
	RowSpacer
)

// Row represents a single row in the dashboard
type Row struct {
	Type      RowType
	Project   *data.Project
	Workspace *data.Workspace
}

// toolbarButtonKind identifies toolbar buttons
type toolbarButtonKind int

const (
	toolbarHelp toolbarButtonKind = iota
	toolbarMonitor
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
	activeRoot  string // Currently active workspace root
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
	deleteIconX     int             // X position of delete "x" icon for currently selected row

	// Loading state
	creatingWorkspaces map[string]*data.Workspace // Workspaces currently being created
	deletingWorkspaces map[string]bool            // Workspaces currently being deleted
	spinnerFrame       int                        // Current spinner animation frame
	spinnerActive      bool                       // Whether spinner ticks are active

	// Agent activity state
	activeWorkspaces map[string]bool // Workspaces with active agents (synced from center)

	// Styles
	styles common.Styles
}

// New creates a new dashboard model
func New() *Model {
	return &Model{
		projects:           []data.Project{},
		rows:               []Row{},
		statusCache:        make(map[string]*git.StatusResult),
		creatingWorkspaces: make(map[string]*data.Workspace),
		deletingWorkspaces: make(map[string]bool),
		activeWorkspaces:   make(map[string]bool),
		cursor:             0,
		focused:            true,
		styles:             common.DefaultStyles(),
	}
}

// SetActiveWorkspaces updates the set of workspaces with active agents.
func (m *Model) SetActiveWorkspaces(active map[string]bool) {
	m.activeWorkspaces = active
}

// InvalidateStatus removes a workspace's cached status.
// This should be called when git status is invalidated externally (e.g., file watcher events)
// to keep the dashboard cache in sync with the StatusManager cache.
func (m *Model) InvalidateStatus(root string) {
	delete(m.statusCache, root)
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
			return m, m.previewCurrentRow()
		}
		if msg.Button == tea.MouseWheelDown {
			m.moveCursor(delta)
			return m, m.previewCurrentRow()
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

			// Check if click is on the delete "x" icon for the currently selected row
			if idx == m.cursor {
				rowType := m.rows[idx].Type
				if rowType == RowProject || rowType == RowWorkspace {
					// Convert screen X to content X
					borderLeft := 1
					paddingLeft := 0
					contentX := msg.X - borderLeft - paddingLeft
					// Check if click is on the delete slot (space + x + space)
					if contentX >= m.deleteIconX && contentX < m.deleteIconX+3 {
						m.toolbarFocused = false
						return m, m.handleDelete()
					}
				}
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
				return m, m.previewCurrentRow()
			case key.Matches(msg, key.NewBinding(key.WithKeys("down", "j"))):
				m.toolbarFocused = false
				if last := m.findSelectableRow(len(m.rows)-1, -1); last != -1 {
					m.cursor = last
				}
				return m, m.previewCurrentRow()
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
				return m, m.previewCurrentRow()
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
			m.moveCursor(-1)
			return m, m.previewCurrentRow()
		case key.Matches(msg, key.NewBinding(key.WithKeys("pgdown", "ctrl+d"))):
			// Half-page scroll to maintain context overlap
			delta := m.visibleHeight() / 2
			if delta < 1 {
				delta = 1
			}
			m.moveCursor(delta)
			return m, m.previewCurrentRow()
		case key.Matches(msg, key.NewBinding(key.WithKeys("pgup", "ctrl+u"))):
			// Half-page scroll to maintain context overlap
			delta := m.visibleHeight() / 2
			if delta < 1 {
				delta = 1
			}
			m.moveCursor(-delta)
			return m, m.previewCurrentRow()
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
				return m, m.previewCurrentRow()
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("g"))):
			// Jump to first selectable row
			if idx := m.findSelectableRow(0, 1); idx != -1 {
				m.cursor = idx
				return m, m.previewCurrentRow()
			}
		}

	case SpinnerTickMsg:
		// Advance spinner frame if we have loading items or active agents
		if len(m.creatingWorkspaces) > 0 || len(m.deletingWorkspaces) > 0 {
			m.spinnerFrame++
			cmds = append(cmds, m.tickSpinner())
		} else {
			m.spinnerActive = false
		}

	case messages.ProjectsLoaded:
		m.SetProjects(msg.Projects)

	case messages.GitStatusResult:
		if msg.Err == nil {
			m.statusCache[msg.Root] = msg.Status
		}

	case messages.WorkspaceActivated:
		if msg.Workspace != nil {
			m.activeRoot = msg.Workspace.Root
		}

	case messages.WorkspacePreviewed:
		if msg.Workspace != nil {
			m.activeRoot = msg.Workspace.Root
		}

	case messages.ShowWelcome:
		m.activeRoot = ""
	}

	return m, common.SafeBatch(cmds...)
}

// View renders the dashboard
func (m *Model) View() string {
	var b strings.Builder

	// Calculate visible area (inner height minus toolbar + help)
	innerHeight := m.height - 2
	if innerHeight < 0 {
		innerHeight = 0
	}
	headerHeight := 0
	helpHeight := m.helpLineCount()
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
	// Add +1 to account for toolbar not having a trailing newline
	padding := targetHeight - contentHeight + 1
	if padding > 0 {
		b.WriteString(strings.Repeat("\n", padding))
		m.toolbarY = targetHeight
	} else {
		m.toolbarY = contentHeight - 1
	}

	// Render toolbar
	toolbar := m.renderToolbar()
	b.WriteString(toolbar)

	// Help lines
	if m.showKeymapHints {
		contentWidth := m.width - 3
		if contentWidth < 1 {
			contentWidth = 1
		}
		helpLines := m.helpLines(contentWidth)
		if len(helpLines) > 0 {
			b.WriteString("\n")
			b.WriteString(strings.Join(helpLines, "\n"))
		}
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
	prevCursor := m.cursor
	prevOffset := m.scrollOffset
	m.projects = projects
	m.rebuildRows()
	if m.cursor == prevCursor {
		m.scrollOffset = prevOffset
		m.clampScrollOffset()
	}
}

// visibleHeight returns the number of visible rows in the dashboard
func (m *Model) visibleHeight() int {
	innerHeight := m.height - 2
	if innerHeight < 0 {
		innerHeight = 0
	}
	headerHeight := 0
	helpHeight := m.helpLineCount()
	toolbarHeight := m.toolbarHeight()
	visibleHeight := innerHeight - headerHeight - toolbarHeight - helpHeight
	if visibleHeight < 1 {
		visibleHeight = 1
	}
	return visibleHeight
}
