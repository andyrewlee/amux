package dashboard

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
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
	// ActivityWorkspaceID is precomputed to avoid per-frame path normalization.
	ActivityWorkspaceID string
	// MainWorkspace points to a project's primary/main workspace for project rows.
	MainWorkspace *data.Workspace
}

// toolbarButtonKind identifies toolbar buttons
type toolbarButtonKind int

const (
	toolbarHelp toolbarButtonKind = iota
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
	activeWorkspaceIDs map[string]bool // Workspace IDs with active agents (synced from center)

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
		activeWorkspaceIDs: make(map[string]bool),
		cursor:             0,
		focused:            true,
		styles:             common.DefaultStyles(),
	}
}

// SetActiveWorkspaces updates the set of workspaces with active agents.
func (m *Model) SetActiveWorkspaces(active map[string]bool) {
	m.activeWorkspaceIDs = active
}

// InvalidateStatus marks a workspace's cached status stale.
// Keep dirty status sticky until a fresh clean result arrives to avoid
// temporary clean flicker between invalidation and refresh.
func (m *Model) InvalidateStatus(root string) {
	if status := m.statusCache[root]; status != nil && !status.Clean {
		return
	}
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
