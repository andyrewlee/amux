package sidebar

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/compositor"
	"github.com/andyrewlee/amux/internal/vterm"
)

// SelectionState tracks mouse selection state
type SelectionState struct {
	Active    bool
	StartX    int
	StartLine int // Absolute line number (0 = first scrollback line)
	EndX      int
	EndLine   int // Absolute line number
}

// TerminalTabID is a unique identifier for a terminal tab
type TerminalTabID string

// terminalTabIDCounter is used to generate unique tab IDs
var terminalTabIDCounter uint64

// generateTerminalTabID creates a new unique terminal tab ID
func generateTerminalTabID() TerminalTabID {
	id := atomic.AddUint64(&terminalTabIDCounter, 1)
	return TerminalTabID(fmt.Sprintf("term-tab-%d", id))
}

// TerminalTab represents a single terminal tab
type TerminalTab struct {
	ID    TerminalTabID
	Name  string // "Terminal 1", "Terminal 2", etc.
	State *TerminalState
}

// TerminalState holds the terminal state for a workspace
type TerminalState struct {
	Terminal         *pty.Terminal
	VTerm            *vterm.VTerm
	Running          bool
	Detached         bool
	UserDetached     bool
	reattachInFlight bool
	SessionName      string
	mu               sync.Mutex

	// Track last size to avoid unnecessary resizes
	lastWidth  int
	lastHeight int

	// PTY output buffering
	pendingOutput     []byte
	flushScheduled    bool
	lastOutputAt      time.Time
	flushPendingSince time.Time

	// Selection state
	Selection          SelectionState
	selectionScroll    common.SelectionScrollState
	selectionLastTermX int

	// Snapshot cache for VTermLayer - avoid recreating snapshot when terminal unchanged
	cachedSnap       *compositor.VTermSnapshot
	cachedVersion    uint64
	cachedShowCursor bool

	readerActive      bool
	ptyMsgCh          chan tea.Msg
	readerCancel      chan struct{}
	ptyRestartBackoff time.Duration
	ptyHeartbeat      int64
	ptyRestartCount   int
	ptyRestartSince   time.Time
}

// terminalTabHitKind identifies the type of tab bar click target
type terminalTabHitKind int

const (
	terminalTabHitTab terminalTabHitKind = iota
	terminalTabHitClose
	terminalTabHitPlus
)

// terminalTabHit represents a clickable region in the tab bar
type terminalTabHit struct {
	kind   terminalTabHitKind
	index  int
	region common.HitRegion
}

// SidebarSelectionScrollTick is sent by the tick loop to continue
// auto-scrolling during mouse-drag selection past viewport edges.
type SidebarSelectionScrollTick struct {
	WorkspaceID string
	TabID       TerminalTabID
	Gen         uint64
}

// TerminalModel is the Bubbletea model for the sidebar terminal section
type TerminalModel struct {
	// State per workspace - multiple tabs per workspace
	tabsByWorkspace      map[string][]*TerminalTab
	activeTabByWorkspace map[string]int
	tabHits              []terminalTabHit // for mouse click handling
	pendingCreation      map[string]bool  // tracks workspaces with tab creation in progress

	// Current workspace
	workspace *data.Workspace

	// Layout
	width           int
	height          int
	focused         bool
	offsetX         int
	offsetY         int
	showKeymapHints bool

	// Styles
	styles common.Styles

	// PTY message sink
	msgSink func(tea.Msg)

	// tmux config
	tmuxServerName string
	tmuxConfigPath string
	instanceID     string
}

// NewTerminalModel creates a new sidebar terminal model
func NewTerminalModel() *TerminalModel {
	return &TerminalModel{
		tabsByWorkspace:      make(map[string][]*TerminalTab),
		activeTabByWorkspace: make(map[string]int),
		pendingCreation:      make(map[string]bool),
		styles:               common.DefaultStyles(),
	}
}

// SetTmuxConfig updates the tmux configuration.
func (m *TerminalModel) SetTmuxConfig(serverName, configPath string) {
	m.tmuxServerName = serverName
	m.tmuxConfigPath = configPath
}

// SetInstanceID sets the tmux instance tag for sessions created by this model.
func (m *TerminalModel) SetInstanceID(id string) {
	m.instanceID = id
}

func (m *TerminalModel) getTmuxOptions() tmux.Options {
	opts := tmux.DefaultOptions()
	if m.tmuxServerName != "" {
		opts.ServerName = m.tmuxServerName
	}
	if m.tmuxConfigPath != "" {
		opts.ConfigPath = m.tmuxConfigPath
	}
	return opts
}

// SetShowKeymapHints controls whether helper text is rendered.
func (m *TerminalModel) SetShowKeymapHints(show bool) {
	if m.showKeymapHints == show {
		return
	}
	m.showKeymapHints = show
	m.refreshTerminalSize()
}

// SetStyles updates the component's styles (for theme changes).
func (m *TerminalModel) SetStyles(styles common.Styles) {
	m.styles = styles
}

// SetMsgSink sets a callback for PTY messages.
func (m *TerminalModel) SetMsgSink(sink func(tea.Msg)) {
	m.msgSink = sink
}

// workspaceID returns the ID of the current workspace
func (m *TerminalModel) workspaceID() string {
	if m.workspace == nil {
		return ""
	}
	return string(m.workspace.ID())
}

// setWorkspace sets the current workspace reference.
func (m *TerminalModel) setWorkspace(ws *data.Workspace) {
	m.workspace = ws
}

// getTabs returns the tabs for the current workspace
func (m *TerminalModel) getTabs() []*TerminalTab {
	return m.tabsByWorkspace[m.workspaceID()]
}

// getActiveTabIdx returns the active tab index for the current workspace
func (m *TerminalModel) getActiveTabIdx() int {
	return m.activeTabByWorkspace[m.workspaceID()]
}

// setActiveTabIdx sets the active tab index for the current workspace
func (m *TerminalModel) setActiveTabIdx(idx int) {
	m.activeTabByWorkspace[m.workspaceID()] = idx
}

// getActiveTab returns the active tab for the current workspace
func (m *TerminalModel) getActiveTab() *TerminalTab {
	tabs := m.getTabs()
	idx := m.getActiveTabIdx()
	if idx >= 0 && idx < len(tabs) {
		return tabs[idx]
	}
	return nil
}

// getTerminal returns the terminal state for the current workspace's active tab
func (m *TerminalModel) getTerminal() *TerminalState {
	tab := m.getActiveTab()
	if tab != nil {
		return tab.State
	}
	return nil
}

// getTabByID returns the tab with the given ID, or nil if not found
func (m *TerminalModel) getTabByID(wsID string, tabID TerminalTabID) *TerminalTab {
	for _, tab := range m.tabsByWorkspace[wsID] {
		if tab.ID == tabID {
			return tab
		}
	}
	return nil
}

// nextTerminalName returns the next available terminal name
func nextTerminalName(tabs []*TerminalTab) string {
	maxNum := 0
	for _, tab := range tabs {
		var num int
		if _, err := fmt.Sscanf(tab.Name, "Terminal %d", &num); err == nil {
			if num > maxNum {
				maxNum = num
			}
		}
	}
	return fmt.Sprintf("Terminal %d", maxNum+1)
}

// NextTab switches to the next terminal tab (circular)
func (m *TerminalModel) NextTab() {
	tabs := m.getTabs()
	if len(tabs) <= 1 {
		return
	}
	idx := m.getActiveTabIdx()
	idx = (idx + 1) % len(tabs)
	m.setActiveTabIdx(idx)
	m.refreshTerminalSize()
}

// PrevTab switches to the previous terminal tab (circular)
func (m *TerminalModel) PrevTab() {
	tabs := m.getTabs()
	if len(tabs) <= 1 {
		return
	}
	idx := m.getActiveTabIdx()
	idx = (idx - 1 + len(tabs)) % len(tabs)
	m.setActiveTabIdx(idx)
	m.refreshTerminalSize()
}

// SelectTab selects a tab by index
func (m *TerminalModel) SelectTab(idx int) {
	tabs := m.getTabs()
	if idx >= 0 && idx < len(tabs) {
		m.setActiveTabIdx(idx)
		m.refreshTerminalSize()
	}
}

// HasMultipleTabs returns true if there are multiple tabs for the current workspace
func (m *TerminalModel) HasMultipleTabs() bool {
	return len(m.getTabs()) > 1
}

// Focus sets focus state
func (m *TerminalModel) Focus() {
	m.focused = true
}

// Blur removes focus
func (m *TerminalModel) Blur() {
	m.focused = false
}

// Focused returns whether the terminal is focused
func (m *TerminalModel) Focused() bool {
	return m.focused
}
