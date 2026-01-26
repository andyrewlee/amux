package center

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/compositor"
	"github.com/andyrewlee/amux/internal/ui/diff"
	"github.com/andyrewlee/amux/internal/vterm"
)

// TabID is a unique identifier for a tab that survives slice reordering
type TabID string

// tabIDCounter is used to generate unique tab IDs
var tabIDCounter uint64

// generateTabID creates a new unique tab ID
func generateTabID() TabID {
	id := atomic.AddUint64(&tabIDCounter, 1)
	return TabID(fmt.Sprintf("tab-%d", id))
}

// SelectionState tracks mouse selection state for copy/paste
type SelectionState struct {
	Active    bool // Selection in progress (mouse button down)?
	StartX    int  // Start column (terminal coordinates)
	StartLine int  // Start row (absolute line number, 0 = first scrollback line)
	EndX      int  // End column
	EndLine   int  // End row (absolute line number)
}

// Tab represents a single tab in the center pane
type Tab struct {
	ID           TabID // Unique identifier that survives slice reordering
	Name         string
	Assistant    string
	Workspace    *data.Workspace
	Agent        *appPty.Agent
	Terminal     *vterm.VTerm // Virtual terminal emulator with scrollback
	DiffViewer   *diff.Model  // Native diff viewer (replaces PTY-based viewer)
	mu           sync.Mutex   // Protects Terminal
	closed       uint32
	closing      uint32
	Running      bool // Whether the agent is actively running
	readerActive bool // Guard to ensure only one PTY read loop per tab
	// Buffer PTY output to avoid rendering partial screen updates.

	pendingOutput     []byte
	flushScheduled    bool
	lastOutputAt      time.Time
	flushPendingSince time.Time
	ptyRows           int
	ptyCols           int
	ptyMsgCh          chan tea.Msg
	readerCancel      chan struct{}
	// Mouse selection state
	Selection             SelectionState
	selectionGen          uint64
	selectionScrollDir    int
	selectionScrollActive bool

	ptyTraceFile      *os.File
	ptyTraceBytes     int
	ptyTraceClosed    bool
	ptyRestartBackoff time.Duration
	ptyHeartbeat      int64
	ptyRestartCount   int
	ptyRestartSince   time.Time

	// Snapshot cache for VTermLayer - avoid recreating snapshot when terminal unchanged
	cachedSnap       *compositor.VTermSnapshot
	cachedVersion    uint64
	cachedShowCursor bool
	monitorSnapAt    time.Time
	monitorDirty     bool
}

func (t *Tab) isClosed() bool {
	if t == nil {
		return true
	}
	return atomic.LoadUint32(&t.closed) == 1 || atomic.LoadUint32(&t.closing) == 1
}

func (t *Tab) markClosing() {
	if t == nil {
		return
	}
	atomic.StoreUint32(&t.closing, 1)
}

func (t *Tab) markClosed() {
	if t == nil {
		return
	}
	atomic.StoreUint32(&t.closed, 1)
	atomic.StoreUint32(&t.closing, 1)
}

// Model is the Bubbletea model for the center pane
type Model struct {
	// State
	workspace            *data.Workspace
	tabsByWorkspace      map[string][]*Tab // tabs per workspace ID
	activeTabByWorkspace map[string]int    // active tab index per workspace
	focused              bool
	canFocusRight        bool
	monitorMode          bool
	monitorSnapshotCache map[TabID]MonitorTabSnapshot
	monitorSnapshotNext  int
	monitorSnapCh        chan monitorSnapshotRequest
	monitorSnapCancel    func()
	monitorSnapHeartbeat int64
	monitorActiveID      TabID
	tabsRevision         uint64
	monitorTabsRevision  uint64
	monitorTabsCache     []*Tab
	agentManager         *appPty.AgentManager
	monitor              MonitorModel
	msgSink              func(tea.Msg)
	tabEvents            chan tabEvent
	tabActorReady        uint32
	tabActorHeartbeat    int64

	// Layout
	width           int
	height          int
	offsetX         int // X offset from screen left (dashboard width)
	showKeymapHints bool

	// Animation
	spinnerFrame int // Current frame for activity spinner animation

	// Config
	config  *config.Config
	styles  common.Styles
	tabHits []tabHit
}

type tabHitKind int

const (
	tabHitTab tabHitKind = iota
	tabHitClose
	tabHitPlus
	tabHitPrev
	tabHitNext
)

type tabHit struct {
	kind   tabHitKind
	index  int
	region common.HitRegion
}

func (m *Model) paneWidth() int {
	if m.width < 1 {
		return 1
	}
	return m.width
}

func (m *Model) contentWidth() int {
	frameX, _ := m.styles.Pane.GetFrameSize()
	width := m.paneWidth() - frameX
	if width < 1 {
		return 1
	}
	return width
}

// ContentWidth returns the content width inside the pane.
func (m *Model) ContentWidth() int {
	return m.contentWidth()
}

// TerminalMetrics holds the computed geometry for the terminal content area.
// This is the single source of truth for terminal positioning and sizing.
type TerminalMetrics struct {
	// For mouse hit-testing (screen coordinates to terminal coordinates)
	ContentStartX int // X offset from pane left edge (border + padding)
	ContentStartY int // Y offset from pane top edge (border + tab bar)

	// Terminal dimensions
	Width  int // Terminal width in columns
	Height int // Terminal height in rows
}

// terminalMetrics computes the terminal content area geometry.
// It preserves the original layout constants while accounting for dynamic help lines.
func (m *Model) terminalMetrics() TerminalMetrics {
	// These values match the original working implementation
	const (
		borderLeft   = 1
		paddingLeft  = 1
		borderTop    = 1
		tabBarHeight = 1 // compact tabs (no borders, single line)
		baseOverhead = 4 // borders (2) + tab bar (1) + status line reserve (1)
	)

	width := m.contentWidth()
	if width < 1 {
		width = 1
	}
	if width < 10 {
		width = 80
	}
	helpLineCount := 0
	if m.showKeymapHints {
		helpLineCount = len(m.helpLines(width))
	}
	height := m.height - baseOverhead - helpLineCount
	if height < 5 {
		height = 24
	}

	return TerminalMetrics{
		ContentStartX: borderLeft + paddingLeft,
		ContentStartY: borderTop + tabBarHeight,
		Width:         width,
		Height:        height,
	}
}

// New creates a new center pane model
func New(cfg *config.Config) *Model {
	return &Model{
		tabsByWorkspace:      make(map[string][]*Tab),
		activeTabByWorkspace: make(map[string]int),
		config:               cfg,
		agentManager:         appPty.NewAgentManager(cfg),
		styles:               common.DefaultStyles(),
		tabEvents:            make(chan tabEvent, 1024),
	}
}

// SetCanFocusRight controls whether focus-right hints should be shown.
func (m *Model) SetCanFocusRight(can bool) {
	m.canFocusRight = can
}

// SetMonitorMode controls whether monitor-mode optimizations are active.
func (m *Model) SetMonitorMode(enabled bool) {
	m.monitorMode = enabled
	if !enabled {
		m.StopMonitorSnapshots()
		m.monitorSnapshotCache = nil
		m.monitorSnapshotNext = 0
	} else if m.monitorSnapshotCache == nil {
		m.monitorSnapshotCache = make(map[TabID]MonitorTabSnapshot)
	}
}

// SetShowKeymapHints controls whether helper text is rendered.
func (m *Model) SetShowKeymapHints(show bool) {
	m.showKeymapHints = show
}

// SetStyles updates the component's styles (for theme changes).
func (m *Model) SetStyles(styles common.Styles) {
	m.styles = styles
	// Propagate to all viewers in tabs
	for _, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			if tab != nil {
				if tab.DiffViewer != nil {
					tab.DiffViewer.SetStyles(styles)
				}
			}
		}
	}
}

// SetMsgSink sets a callback for PTY messages.
func (m *Model) SetMsgSink(sink func(tea.Msg)) {
	m.msgSink = sink
}

// TabEvents returns a channel for actor-style tab mutations.
func (m *Model) TabEvents() chan tabEvent {
	return m.tabEvents
}

func (m *Model) isTabActorReady() bool {
	return atomic.LoadUint32(&m.tabActorReady) == 1
}

func (m *Model) setTabActorReady() {
	atomic.StoreUint32(&m.tabActorReady, 1)
}

func (m *Model) noteTabActorHeartbeat() {
	atomic.StoreInt64(&m.tabActorHeartbeat, time.Now().UnixNano())
	if atomic.LoadUint32(&m.tabActorReady) == 0 {
		atomic.StoreUint32(&m.tabActorReady, 1)
	}
}

// workspaceID returns the ID of the current workspace, or empty string
func (m *Model) workspaceID() string {
	if m.workspace == nil {
		return ""
	}
	return string(m.workspace.ID())
}

// getTabs returns the tabs for the current workspace
func (m *Model) getTabs() []*Tab {
	return m.tabsByWorkspace[m.workspaceID()]
}

// getTabByID returns the tab with the given ID, or nil if not found
func (m *Model) getTabByID(wsID string, tabID TabID) *Tab {
	for _, tab := range m.tabsByWorkspace[wsID] {
		if tab.ID == tabID && !tab.isClosed() {
			return tab
		}
	}
	return nil
}

// getActiveTabIdx returns the active tab index for the current workspace
func (m *Model) getActiveTabIdx() int {
	return m.activeTabByWorkspace[m.workspaceID()]
}

// setActiveTabIdx sets the active tab index for the current workspace
func (m *Model) setActiveTabIdx(idx int) {
	m.activeTabByWorkspace[m.workspaceID()] = idx
}

func (m *Model) noteTabsChanged() {
	m.tabsRevision++
}

func (m *Model) isActiveTab(wsID string, tabID TabID) bool {
	if m.workspace == nil || wsID != m.workspaceID() {
		return false
	}
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if activeIdx < 0 || activeIdx >= len(tabs) {
		return false
	}
	return tabs[activeIdx].ID == tabID
}

// removeTab removes a tab at index from the current workspace
func (m *Model) removeTab(idx int) {
	wsID := m.workspaceID()
	tabs := m.tabsByWorkspace[wsID]
	if idx >= 0 && idx < len(tabs) {
		m.tabsByWorkspace[wsID] = append(tabs[:idx], tabs[idx+1:]...)
		m.noteTabsChanged()
	}
}

// CleanupWorkspace removes all tabs and state for a deleted workspace
func (m *Model) CleanupWorkspace(ws *data.Workspace) {
	if ws == nil {
		return
	}
	wsID := string(ws.ID())

	// Close resources for each tab before removing
	for _, tab := range m.tabsByWorkspace[wsID] {
		tab.markClosing()
		m.stopPTYReader(tab)
		tab.mu.Lock()
		if tab.ptyTraceFile != nil {
			_ = tab.ptyTraceFile.Close()
			tab.ptyTraceFile = nil
			tab.ptyTraceClosed = true
		}
		tab.pendingOutput = nil
		tab.Running = false
		tab.mu.Unlock()
		tab.markClosed()
	}

	delete(m.tabsByWorkspace, wsID)
	delete(m.activeTabByWorkspace, wsID)
	m.noteTabsChanged()

	// Also cleanup agents for this workspace
	if m.agentManager != nil {
		m.agentManager.CloseWorkspaceAgents(ws)
	}
}

// Init initializes the center pane
func (m *Model) Init() tea.Cmd {
	return nil
}

// Focus sets the focus state
func (m *Model) Focus() {
	m.focused = true
}

// Blur removes focus
func (m *Model) Blur() {
	m.focused = false
}

// Focused returns whether the center pane is focused
func (m *Model) Focused() bool {
	return m.focused
}

// SetWorkspace sets the active workspace
func (m *Model) SetWorkspace(ws *data.Workspace) {
	m.workspace = ws
}

// HasTabs returns whether there are any tabs for the current workspace
func (m *Model) HasTabs() bool {
	return len(m.getTabs()) > 0
}

// SetSize sets the center pane size
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height

	// Use centralized metrics for terminal sizing
	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height

	// CommitViewer uses the same dimensions
	viewerWidth := termWidth
	viewerHeight := termHeight

	// Update all terminals across all workspaces
	for _, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			tab.mu.Lock()
			if tab.Terminal != nil {
				if tab.Terminal.Width != termWidth || tab.Terminal.Height != termHeight {
					tab.Terminal.Resize(termWidth, termHeight)
				}
			}
			if tab.DiffViewer != nil {
				tab.DiffViewer.SetSize(viewerWidth, viewerHeight)
			}
			tab.mu.Unlock()
			m.resizePTY(tab, termHeight, termWidth)
		}
	}
}

// SetOffset sets the X offset of the pane from screen left (for mouse coordinate conversion)
func (m *Model) SetOffset(x int) {
	m.offsetX = x
}

// Close cleans up all resources
func (m *Model) Close() {
	m.StopMonitorSnapshots()
	for _, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			tab.markClosing()
			m.stopPTYReader(tab)
			tab.mu.Lock()
			if tab.ptyTraceFile != nil {
				_ = tab.ptyTraceFile.Close()
				tab.ptyTraceFile = nil
				tab.ptyTraceClosed = true
			}
			tab.pendingOutput = nil
			tab.DiffViewer = nil
			tab.Running = false
			tab.mu.Unlock()
			tab.markClosed()
		}
	}
	if m.agentManager != nil {
		m.agentManager.CloseAll()
	}
}

// TickSpinner advances the spinner animation frame.
func (m *Model) TickSpinner() {
	m.spinnerFrame++
}

// screenToTerminal converts screen coordinates to terminal coordinates
// Returns the terminal X, Y and whether the coordinates are within the terminal content area
func (m *Model) screenToTerminal(screenX, screenY int) (termX, termY int, inBounds bool) {
	// Use centralized metrics for consistent geometry
	tm := m.terminalMetrics()

	// X offset includes pane position + border + padding
	contentStartX := m.offsetX + tm.ContentStartX
	// Y offset is just border + tab bar (pane Y starts at 0)
	contentStartY := tm.ContentStartY

	// Convert screen coordinates to terminal coordinates
	termX = screenX - contentStartX
	termY = screenY - contentStartY

	// Check bounds
	inBounds = termX >= 0 && termX < tm.Width && termY >= 0 && termY < tm.Height
	return
}
