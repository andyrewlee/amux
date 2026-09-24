package dashboard

import (
	"maps"
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
	// RowShelved is an intentionally archived workspace (worktree removed,
	// branch kept) — selectable for restore/purge, rendered dimmed.
	RowShelved
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
	// pendingSelectID holds a workspace ID the cursor should land on as soon as
	// that workspace has a row. A just-created workspace is activated before the
	// projects reload carrying it arrives, so the selection has to wait for the
	// rebuild rather than being dropped.
	pendingSelectID string
	// pendingSelectLoads bounds how many rebuilds that wait may span. A creation
	// that never yields a row (created-with-warning, deleted before its reload
	// landed) would otherwise strand the ID for the session — and since a
	// workspace ID is a hash of project+name, a later delete-then-recreate at the
	// same name reproduces it and would yank the cursor off the user's row long
	// afterwards. The dashboard cannot rely on input to clear it, because this
	// flow deliberately parks focus on the center pane and every input-side
	// clear is gated on being focused.
	pendingSelectLoads int

	// marked holds workspace marks for bulk lifecycle ops, keyed by
	// string(ws.MetadataID()) — the persisted store key, stable across row
	// rebuilds, renames, and NormalizePath drift. Marks outlive row
	// rebuilds by design; collection walks live rows so a vanished row's
	// stale mark simply never matches.
	marked map[string]bool

	// Loading state
	creatingWorkspaces map[string]*data.Workspace // Workspaces currently being created
	busyWorkspaces     map[string]WorkspaceOp     // Workspaces mid-mutation (root -> op label)
	spinnerFrame       int                        // Current spinner animation frame
	spinnerActive      bool                       // Whether spinner ticks are active

	// Agent activity state
	activeWorkspaceIDs map[string]bool            // Workspace IDs with active agents (synced from center)
	agentStates        map[string]data.AgentState // Per-workspace semantic agent states
	doneAcked          map[string]bool            // Workspace IDs whose "done" indicator has been seen by the user
	donePending        map[string]bool            // Unacked done latch — survives ClassifyState's Done→Idle decay
	notifyOnDone       bool                       // Ring a terminal bell on the unacked Working→Done edge

	// Styles
	styles common.Styles

	// contentVersion is bumped by markContentDirty on EVERY mutation that can
	// change View() output — the app-layer paneGate skips the string build
	// while it is unchanged at the same compose geometry. Any new field or
	// update path that affects rendered output MUST call markContentDirty;
	// a missed mark produces a stale pane with no crash. contentBuilds counts
	// actual View() builds for the skip tests.
	contentVersion uint64
	contentBuilds  uint64
}

// New creates a new dashboard model
func New() *Model {
	return &Model{
		projects:           []data.Project{},
		rows:               []Row{},
		statusCache:        make(map[string]*git.StatusResult),
		creatingWorkspaces: make(map[string]*data.Workspace),
		busyWorkspaces:     make(map[string]WorkspaceOp),
		activeWorkspaceIDs: make(map[string]bool),
		doneAcked:          make(map[string]bool),
		donePending:        make(map[string]bool),
		marked:             make(map[string]bool),
		cursor:             0,
		focused:            true,
		styles:             common.DefaultStyles(),
	}
}

// markContentDirty bumps contentVersion. It MUST be called by every update
// path that changes View() output; see the contentVersion field invariant.
func (m *Model) markContentDirty() { m.contentVersion++ }

// ContentVersion reports the current content version for the compose-side
// pane gate.
func (m *Model) ContentVersion() uint64 { return m.contentVersion }

// ContentBuildCount reports how many times View() actually built — test
// instrumentation for the pane gate.
func (m *Model) ContentBuildCount() uint64 { return m.contentBuilds }

// syncScrollToCursor keeps the cursor inside the visible window. Cursor-moving
// update paths call it directly so click hit-tests (rowIndexAt) see a fresh
// offset without waiting for a render; View calls it again as a backstop.
func (m *Model) syncScrollToCursor() {
	visibleHeight := m.visibleHeight()
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	}
	if m.cursor >= m.scrollOffset+visibleHeight {
		m.scrollOffset = m.cursor - visibleHeight + 1
	}
	m.clampScrollOffset()
}

// SetActiveWorkspaces updates the set of workspaces with active agents.
// The activity sync feeds this every scan — dirtying only on real change
// keeps steady-state scans from forcing content rebuilds.
func (m *Model) SetActiveWorkspaces(active map[string]bool) {
	if maps.Equal(m.activeWorkspaceIDs, active) {
		return
	}
	m.activeWorkspaceIDs = active
	m.markContentDirty()
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
func (m *Model) SetAgentStates(states map[string]data.AgentState) tea.Cmd {
	if m.donePending == nil {
		m.donePending = make(map[string]bool)
	}
	prev := m.agentStates
	m.agentStates = states
	if !maps.Equal(prev, states) {
		m.markContentDirty()
	}
	bell := false
	for wsID, st := range states {
		switch st {
		case data.StateWorking:
			delete(m.doneAcked, wsID)
			delete(m.donePending, wsID)
		case data.StateDone:
			// Fire only on the fresh, unacked Working→Done transition. Gating on
			// prev == Working de-dupes steady-state Done (prev is already Done)
			// and skips Idle/absent→Done, which is not a finish the user watched.
			if prev[wsID] == data.StateWorking {
				// The done badge latches past DoneWindow's decay — it stays
				// visible until ackDone (row activation) or a new Working edge.
				m.donePending[wsID] = true
				if m.notifyOnDone && !m.doneAcked[wsID] {
					bell = true
				}
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
	m.markContentDirty()
}

// SetCanFocusRight controls whether focus-right hints should be shown.
func (m *Model) SetCanFocusRight(can bool) {
	m.canFocusRight = can
	m.markContentDirty()
}

// SetShowKeymapHints controls whether helper text is rendered.
func (m *Model) SetShowKeymapHints(show bool) {
	m.showKeymapHints = show
	m.markContentDirty()
}

// SetStyles updates the component's styles (for theme changes).
func (m *Model) SetStyles(styles common.Styles) {
	m.styles = styles
	m.markContentDirty()
}

// Init initializes the dashboard
func (m *Model) Init() tea.Cmd {
	return nil
}

// SetSize sets the dashboard size
func (m *Model) SetSize(width, height int) {
	if m.width == width && m.height == height {
		return
	}
	m.width = width
	m.height = height
	m.markContentDirty()
}

// Focus sets the focus state
func (m *Model) Focus() {
	if m.focused {
		return
	}
	m.focused = true
	m.markContentDirty()
}

// Blur removes focus
func (m *Model) Blur() {
	if !m.focused {
		return
	}
	m.focused = false
	m.markContentDirty()
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
	m.applyPendingSelectionForLoad()
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
