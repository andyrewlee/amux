package center

import (
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
)

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
