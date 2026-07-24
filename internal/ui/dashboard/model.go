package dashboard

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// SpinnerTickMsg is sent to update the spinner animation
type SpinnerTickMsg struct{}

// spinnerInterval is how often the spinner updates
const spinnerInterval = 80 * time.Millisecond

// bellSequence is the ASCII BEL control byte (0x07). Written verbatim to the
// program output it rings the user's terminal bell.
const bellSequence = "\a"

// bellCmd rings the terminal bell. tea.Raw writes the byte straight to the
// program's output buffer (via the RawMsg path), bypassing the alt-screen
// canvas so the BEL reaches the real terminal rather than a rendered cell.
func bellCmd() tea.Cmd {
	return tea.Raw(bellSequence)
}

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
	toolbarCommands toolbarButtonKind = iota
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
	// resourceLabels maps workspace root -> pre-rendered workload annotation
	// (e.g. "1.2c"), derived once per reaper tick in SetResourceStats. Rows
	// annotate heavy workloads so a forgotten dev stack burning CPU is
	// visible at a glance; renderRow only does a map lookup, keeping the
	// per-frame render path allocation-free for data that changes every 60s.
	resourceLabels map[string]string

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
	activeWorkspaceIDs map[string]bool                // Workspace IDs with active agents (synced from center)
	agentStates        map[string]activity.AgentState // Per-workspace semantic agent states
	doneAcked          map[string]bool                // Workspace IDs whose "done" indicator has been seen by the user
	notifyOnDone       bool                           // Ring a terminal bell on the unacked Working→Done edge

	// Styles
	styles common.Styles
}

// New creates a new dashboard model
func New() *Model {
	return &Model{
		projects:           []data.Project{},
		rows:               []Row{},
		statusCache:        make(map[string]*git.StatusResult),
		resourceLabels:     make(map[string]string),
		creatingWorkspaces: make(map[string]*data.Workspace),
		deletingWorkspaces: make(map[string]bool),
		activeWorkspaceIDs: make(map[string]bool),
		doneAcked:          make(map[string]bool),
		cursor:             0,
		focused:            true,
		styles:             common.DefaultStyles(),
	}
}

// resourceLabelThreshold is the %CPU (100 = one core) above which a
// workspace's workload is annotated on its row. Lighter workloads stay
// silent to keep rows quiet.
const resourceLabelThreshold = 50.0

// SetResourceStats replaces the per-workspace workload ledger, pre-rendering
// the row annotations so the render path only looks them up.
func (m *Model) SetResourceStats(stats map[string]process.WorkspaceStats) {
	labels := make(map[string]string, len(stats))
	for root, s := range stats {
		if s.CPU >= resourceLabelThreshold {
			labels[root] = fmt.Sprintf("%.1fc", s.CPU/100)
		}
	}
	m.resourceLabels = labels
}

// SetActiveWorkspaces updates the set of workspaces with active agents.
func (m *Model) SetActiveWorkspaces(active map[string]bool) {
	m.activeWorkspaceIDs = active
}

// SetNotifyOnDone controls whether a terminal bell fires when a workspace
// transitions Working→Done (the same edge the "done" indicator surfaces).
func (m *Model) SetNotifyOnDone(enabled bool) {
	m.notifyOnDone = enabled
}

// SetAgentStates updates the per-workspace semantic agent states.
// It also clears the doneAcked flag for any workspace that has started
// working again, so the next "done" is visible to the user.
//
// It returns a bell command exactly once per unacked Working→Done edge when
// notify-on-done is enabled: the previous state is compared against the new one
// so a workspace that stays Done across frames does not re-bell. A frame with
// several simultaneous edges still rings a single bell.
func (m *Model) SetAgentStates(states map[string]activity.AgentState) tea.Cmd {
	prev := m.agentStates
	m.agentStates = states
	bell := false
	for wsID, st := range states {
		switch st {
		case activity.StateWorking:
			delete(m.doneAcked, wsID)
		case activity.StateDone:
			// Fire only on the fresh, unacked Working→Done transition. Gating on
			// prev == Working de-dupes steady-state Done (prev is already Done)
			// and skips Idle/absent→Done, which is not a finish the user watched.
			if m.notifyOnDone && prev[wsID] == activity.StateWorking && !m.doneAcked[wsID] {
				bell = true
			}
		}
	}
	if bell {
		return bellCmd()
	}
	return nil
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
	// Capture the selected workspace's identity before the rebuild so a delete
	// (or reorder) re-anchors selection to that workspace rather than letting the
	// same index silently slide onto the row that was below it.
	selectedID := m.selectedWorkspaceIDAt(prevCursor)
	m.projects = projects
	m.rebuildRows()
	m.resolveCursorAfterRebuild(prevCursor, selectedID)
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
