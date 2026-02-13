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
	"github.com/andyrewlee/amux/internal/tmux"
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
	ID          TabID // Unique identifier that survives slice reordering
	Name        string
	Assistant   string
	Workspace   *data.Workspace
	Agent       *appPty.Agent
	SessionName string
	Detached    bool
	// reattachInFlight prevents overlapping reattach attempts for the same tab.
	reattachInFlight  bool
	Terminal          *vterm.VTerm // Virtual terminal emulator with scrollback
	DiffViewer        *diff.Model  // Native diff viewer (replaces PTY-based viewer)
	mu                sync.Mutex   // Protects Terminal
	closed            uint32
	closing           uint32
	Running           bool   // Whether the agent is actively running
	readerActive      bool   // Guard to ensure only one PTY read loop per tab
	readerActiveState uint32 // Mirrors readerActive for lock-free atomic reads
	// Buffer PTY output to avoid rendering partial screen updates.

	pendingOutput     []byte
	flushScheduled    bool
	lastOutputAt      time.Time
	lastActivityTagAt time.Time
	lastInputTagAt    time.Time
	flushPendingSince time.Time
	ptyRows           int
	ptyCols           int
	ptyMsgCh          chan tea.Msg
	readerCancel      chan struct{}
	// Mouse selection state
	Selection          SelectionState
	selectionScroll    common.SelectionScrollState
	selectionLastTermX int

	ptyTraceFile      *os.File
	ptyTraceBytes     int
	ptyTraceClosed    bool
	ptyRestartBackoff time.Duration
	ptyHeartbeat      int64
	ptyRestartCount   int
	ptyRestartSince   time.Time
	lastFocusedAt     time.Time

	// Snapshot cache for VTermLayer - avoid recreating snapshot when terminal unchanged
	cachedSnap       *compositor.VTermSnapshot
	cachedVersion    uint64
	cachedShowCursor bool
	createdAt        int64 // Unix timestamp for ordering; persisted in workspace.json
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
	workspaceIDCached    string
	workspaceIDRepo      string
	workspaceIDRoot      string
	tabsByWorkspace      map[string][]*Tab // tabs per workspace ID
	activeTabByWorkspace map[string]int    // active tab index per workspace
	focused              bool
	canFocusRight        bool
	tabsRevision         uint64
	agentManager         *appPty.AgentManager
	msgSink              func(tea.Msg)
	tabEvents            chan tabEvent
	tabActorReady        uint32
	tabActorHeartbeat    int64
	flushLoadSampleAt    time.Time
	cachedBusyTabCount   int

	// Layout
	width           int
	height          int
	offsetX         int // X offset from screen left (dashboard width)
	showKeymapHints bool

	// Animation
	spinnerFrame int // Current frame for activity spinner animation

	// Config
	config     *config.Config
	styles     common.Styles
	tabHits    []tabHit
	tmuxConfig tmuxConfig
	instanceID string
}

// tmuxConfig holds tmux-related configuration
type tmuxConfig struct {
	ServerName string
	ConfigPath string
}

func (m *Model) getTmuxOptions() tmux.Options {
	opts := tmux.DefaultOptions()
	if m.tmuxConfig.ServerName != "" {
		opts.ServerName = m.tmuxConfig.ServerName
	}
	if m.tmuxConfig.ConfigPath != "" {
		opts.ConfigPath = m.tmuxConfig.ConfigPath
	}
	return opts
}

// SetInstanceID sets the tmux instance tag for sessions created by this model.
func (m *Model) SetInstanceID(id string) {
	m.instanceID = id
}

// SetTmuxConfig updates the tmux configuration.
func (m *Model) SetTmuxConfig(serverName, configPath string) {
	m.tmuxConfig.ServerName = serverName
	m.tmuxConfig.ConfigPath = configPath
	if m.agentManager != nil {
		m.agentManager.SetTmuxOptions(m.getTmuxOptions())
	}
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

func (m *Model) setWorkspace(ws *data.Workspace) {
	m.workspace = ws
	m.workspaceIDCached = ""
	m.workspaceIDRepo = ""
	m.workspaceIDRoot = ""
	if ws == nil {
		return
	}
	m.workspaceIDRepo = ws.Repo
	m.workspaceIDRoot = ws.Root
	m.workspaceIDCached = string(ws.ID())
}

// workspaceID returns the ID of the current workspace, or empty string
func (m *Model) workspaceID() string {
	if m.workspace == nil {
		m.workspaceIDCached = ""
		m.workspaceIDRepo = ""
		m.workspaceIDRoot = ""
		return ""
	}
	if m.workspaceIDCached == "" ||
		m.workspaceIDRepo != m.workspace.Repo ||
		m.workspaceIDRoot != m.workspace.Root {
		m.workspaceIDRepo = m.workspace.Repo
		m.workspaceIDRoot = m.workspace.Root
		m.workspaceIDCached = string(m.workspace.ID())
	}
	return m.workspaceIDCached
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

// getTabBySession returns the tab with the given tmux session name.
func (m *Model) getTabBySession(wsID, sessionName string) *Tab {
	if sessionName == "" {
		return nil
	}
	for _, tab := range m.tabsByWorkspace[wsID] {
		if tab == nil || tab.isClosed() {
			continue
		}
		if tab.SessionName == sessionName {
			return tab
		}
		if tab.Agent != nil && tab.Agent.Session == sessionName {
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
	m.setActiveTabIdxForWorkspace(m.workspaceID(), idx)
}

func (m *Model) setActiveTabIdxForWorkspace(wsID string, idx int) {
	if wsID == "" {
		return
	}
	m.activeTabByWorkspace[wsID] = idx
	m.markTabFocused(wsID, idx)
}

func (m *Model) markTabFocused(wsID string, idx int) {
	tabs := m.tabsByWorkspace[wsID]
	if idx < 0 || idx >= len(tabs) {
		return
	}
	tab := tabs[idx]
	if tab == nil || tab.isClosed() {
		return
	}
	tab.mu.Lock()
	tab.lastFocusedAt = time.Now()
	tab.mu.Unlock()
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
		tab.DiffViewer = nil
		tab.Terminal = nil
		tab.cachedSnap = nil
		tab.Workspace = nil
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
