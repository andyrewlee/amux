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
	Active bool // Selection in progress?
	StartX int  // Start column (terminal coordinates)
	StartY int  // Start row (terminal coordinates, relative to visible area)
	EndX   int  // End column
	EndY   int  // End row
}

// Tab represents a single tab in the center pane
type Tab struct {
	ID           TabID // Unique identifier that survives slice reordering
	Name         string
	Assistant    string
	Worktree     *data.Worktree
	Agent        *appPty.Agent
	Terminal     *vterm.VTerm // Virtual terminal emulator with scrollback
	DiffViewer   *diff.Model  // Native diff viewer (replaces PTY-based viewer)
	mu           sync.Mutex   // Protects Terminal
	Running      bool         // Whether the agent is actively running
	readerActive bool         // Guard to ensure only one PTY read loop per tab
	CopyMode     bool         // Whether the tab is in copy/scroll mode (keys not sent to PTY)
	CopyState    common.CopyState
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
	Selection SelectionState

	ptyTraceFile   *os.File
	ptyTraceBytes  int
	ptyTraceClosed bool

	// Snapshot cache for VTermLayer - avoid recreating snapshot when terminal unchanged
	cachedSnap       *compositor.VTermSnapshot
	cachedVersion    uint64
	cachedShowCursor bool
}

// Model is the Bubbletea model for the center pane
type Model struct {
	// State
	worktree            *data.Worktree
	tabsByWorktree      map[string][]*Tab // tabs per worktree ID
	activeTabByWorktree map[string]int    // active tab index per worktree
	focused             bool
	canFocusRight       bool
	agentManager        *appPty.AgentManager
	monitor             MonitorModel

	// Layout
	width           int
	height          int
	offsetX         int // X offset from screen left (dashboard width)
	showKeymapHints bool

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
		tabBarHeight = 3 // tab border + content (tabs render as 3 lines)
		baseOverhead = 6 // borders + tab bar + status line reserve
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
		tabsByWorktree:      make(map[string][]*Tab),
		activeTabByWorktree: make(map[string]int),
		config:              cfg,
		agentManager:        appPty.NewAgentManager(cfg),
		styles:              common.DefaultStyles(),
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
	// Propagate to all viewers in tabs
	for _, tabs := range m.tabsByWorktree {
		for _, tab := range tabs {
			if tab != nil {
				if tab.DiffViewer != nil {
					tab.DiffViewer.SetStyles(styles)
				}
			}
		}
	}
}

// worktreeID returns the ID of the current worktree, or empty string
func (m *Model) worktreeID() string {
	if m.worktree == nil {
		return ""
	}
	return string(m.worktree.ID())
}

// getTabs returns the tabs for the current worktree
func (m *Model) getTabs() []*Tab {
	return m.tabsByWorktree[m.worktreeID()]
}

// getTabByID returns the tab with the given ID, or nil if not found
func (m *Model) getTabByID(wtID string, tabID TabID) *Tab {
	for _, tab := range m.tabsByWorktree[wtID] {
		if tab.ID == tabID {
			return tab
		}
	}
	return nil
}

// getActiveTabIdx returns the active tab index for the current worktree
func (m *Model) getActiveTabIdx() int {
	return m.activeTabByWorktree[m.worktreeID()]
}

// setActiveTabIdx sets the active tab index for the current worktree
func (m *Model) setActiveTabIdx(idx int) {
	m.activeTabByWorktree[m.worktreeID()] = idx
}

func (m *Model) isActiveTab(wtID string, tabID TabID) bool {
	if m.worktree == nil || wtID != m.worktreeID() {
		return false
	}
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if activeIdx < 0 || activeIdx >= len(tabs) {
		return false
	}
	return tabs[activeIdx].ID == tabID
}

// removeTab removes a tab at index from the current worktree
func (m *Model) removeTab(idx int) {
	wtID := m.worktreeID()
	tabs := m.tabsByWorktree[wtID]
	if idx >= 0 && idx < len(tabs) {
		m.tabsByWorktree[wtID] = append(tabs[:idx], tabs[idx+1:]...)
	}
}

// CleanupWorktree removes all tabs and state for a deleted worktree
func (m *Model) CleanupWorktree(wt *data.Worktree) {
	if wt == nil {
		return
	}
	wtID := string(wt.ID())

	// Close resources for each tab before removing
	for _, tab := range m.tabsByWorktree[wtID] {
		m.stopPTYReader(tab)
		if tab.ptyTraceFile != nil {
			_ = tab.ptyTraceFile.Close()
		}
		tab.pendingOutput = nil
	}

	delete(m.tabsByWorktree, wtID)
	delete(m.activeTabByWorktree, wtID)

	// Also cleanup agents for this worktree
	if m.agentManager != nil {
		m.agentManager.CloseWorktreeAgents(wt)
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

// SetWorktree sets the active worktree
func (m *Model) SetWorktree(wt *data.Worktree) {
	m.worktree = wt
}

// HasTabs returns whether there are any tabs for the current worktree
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

	// Update all terminals across all worktrees
	for _, tabs := range m.tabsByWorktree {
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
	m.agentManager.CloseAll()
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
