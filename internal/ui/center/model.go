// Package center implements the center-pane Bubble Tea model: the agent tab
// strip, per-tab PTY I/O and flushing, the diff viewer, and mouse/keyboard
// text selection. It is the largest UI subsystem; tab work is serialized
// through a per-model actor (see tab_actor.go) and output is parsed by
// internal/vterm. For the streaming/scrolling model (flush pipeline, DEC 2026
// frame atomicity, viewport anchoring, chat history view, drag auto-scroll)
// see SCROLLING.md in this package.
package center

import (
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// Model is the Bubbletea model for the center pane
type Model struct {
	// State
	workspace         *data.Workspace
	workspaceIDCached string
	workspaceIDRepo   string
	workspaceIDRoot   string
	// deletedWorkspaceIDs tombstones workspaces seen through WorkspaceDeleted so
	// an async create result dispatched before the delete can't resurrect a
	// dead bucket when it lands. Entries clear when the workspace becomes
	// current again (setWorkspace) or is rebound to (RebindWorkspaceID).
	//
	// The sidebar's TerminalModel has no equivalent tombstone — deliberately.
	// Its result gate accepts a workspace key only when a bucket exists or
	// pendingCreation marks a create in flight, so a torn-down workspace is
	// rejected without one. Center accepts results for any non-tombstoned key
	// (a first tab creates the bucket), so it needs the explicit tombstone.
	deletedWorkspaceIDs map[string]struct{}
	tabs                common.TabSet[*Tab] // tabs + active index per workspace ID
	focused             bool
	canFocusRight       bool
	tabsRevision        uint64
	// helpVersion is a monotonic version of every input that shapes HelpLines
	// output (tab count, workspace presence, keymap-hint visibility, styles,
	// pane size). INVARIANT: every update path that changes what HelpLines
	// renders MUST call markHelpDirty, or the compose-time gate in
	// internal/app will keep reusing a stale help drawable.
	helpVersion uint64
	// helpBuilds counts HelpLines invocations; test instrumentation for the
	// compose-time skip gate in internal/app.
	helpBuilds uint64
	// stylesRev bumps on every SetStyles so the TabBarVersion/StatusLineVersion
	// fingerprints change with the theme; the fingerprints fold it in because
	// their other inputs are per-tab state that knows nothing about styles.
	stylesRev             uint64
	tabBarBuilds          uint64 // TabBarView invocations; test instrumentation
	statusLineBuilds      uint64 // ActiveTerminalStatusLine invocations
	agentManager          *appPty.AgentManager
	msgSink               func(tea.Msg)
	msgSinkTry            func(tea.Msg) bool
	tabEvents             chan tabEvent
	tabActorReady         uint32
	tabActorHeartbeat     int64
	tabActorRedrawPending uint32
	// tabActorStalled gates a single stall/recovery log per episode so the
	// otherwise-silent degradation to synchronous direct-send is observable.
	tabActorStalled    uint32
	flushLoadSampleAt  time.Time
	cachedBusyTabCount int

	// Batched @amux_last_output_at writes (plan 044): output-path tag writes
	// accumulate as a pending set and flush as ONE
	// SetSessionTagValueForSessions call per throttle window, instead of a
	// goroutine + tmux fork per tab per second. activityTagsMu guards the set
	// — marks arrive on the tab-actor goroutine while drains run on Update.
	activityTagsMu          sync.Mutex
	pendingActivityTags     map[string]struct{}
	activityTagFlushPending bool

	// Layout
	width           int
	height          int
	offsetX         int // X offset from screen left (dashboard width)
	showKeymapHints bool
	// viewerCommand is the shell fragment the file-viewer tab runs as
	// `<command> -- <file>`; "vim" when unset (normalized in the setter).
	viewerCommand string

	// Animation
	spinnerFrame int // Current frame for activity spinner animation

	// Config
	config     *config.Config
	styles     common.Styles
	tabHits    []tabHit
	tmuxOpts   tmux.Options
	instanceID string
}

// SetInstanceID sets the tmux instance tag for sessions created by this model.
func (m *Model) SetInstanceID(id string) {
	m.instanceID = id
}

// SetTmuxOptions stores the resolved tmux options and forwards them to the
// agent manager.
func (m *Model) SetTmuxOptions(opts tmux.Options) {
	m.tmuxOpts = opts
	if m.agentManager != nil {
		m.agentManager.SetTmuxOptions(opts)
	}
}

// SetSessionEnvProvider forwards the layered session env composer to the
// agent manager — the same contract ScriptRunner gives script spawns minus
// the trust-gated repo layer (see process.BuildSessionEnv).
func (m *Model) SetSessionEnvProvider(fn func(ws *data.Workspace) ([]string, error)) {
	if m.agentManager != nil {
		m.agentManager.SetSessionEnvProvider(fn)
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
		tabBarHeight = common.TabBarHeight // compact tabs (no borders, single line)
		baseOverhead = 4                   // borders (2) + tab bar (1) + status line reserve (1)
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
	if atomic.LoadUint32(&m.tabActorReady) == 0 {
		return false
	}
	lastBeat := atomic.LoadInt64(&m.tabActorHeartbeat)
	if lastBeat == 0 {
		return false
	}
	if time.Since(time.Unix(0, lastBeat)) > tabActorStallTimeout {
		atomic.StoreUint32(&m.tabActorReady, 0)
		// CAS-gate so a stall logs exactly once per episode even though the hot
		// read path calls this on every keystroke/scroll/write.
		if atomic.CompareAndSwapUint32(&m.tabActorStalled, 0, 1) {
			logging.Warn("tab actor stalled (>%s since last heartbeat); falling back to direct send", tabActorStallTimeout)
		}
		return false
	}
	return true
}

func (m *Model) setTabActorReady() {
	atomic.StoreInt64(&m.tabActorHeartbeat, time.Now().UnixNano())
	atomic.StoreUint32(&m.tabActorReady, 1)
	// Clear any prior stall episode without logging a recovery: this is a fresh
	// (re)attach, not a heartbeat-driven recovery.
	atomic.StoreUint32(&m.tabActorStalled, 0)
}

func (m *Model) noteTabActorHeartbeat() {
	observedAt := time.Now().UnixNano()
	for {
		prev := atomic.LoadInt64(&m.tabActorHeartbeat)
		if observedAt <= prev {
			observedAt = prev + 1
		}
		if atomic.CompareAndSwapInt64(&m.tabActorHeartbeat, prev, observedAt) {
			break
		}
	}
	if atomic.LoadUint32(&m.tabActorReady) == 0 {
		atomic.StoreUint32(&m.tabActorReady, 1)
	}
	// A heartbeat after a stall episode is a genuine recovery; log it once.
	if atomic.CompareAndSwapUint32(&m.tabActorStalled, 1, 0) {
		logging.Info("tab actor recovered")
	}
}

func (m *Model) requestTabActorRedraw() {
	if m == nil {
		return
	}
	if m.msgSinkTry != nil {
		if !atomic.CompareAndSwapUint32(&m.tabActorRedrawPending, 0, 1) {
			return
		}
		if m.msgSinkTry(tabActorRedraw{}) {
			return
		}
		atomic.StoreUint32(&m.tabActorRedrawPending, 0)
		return
	}
	if m.msgSink != nil {
		m.msgSink(tabActorRedraw{})
	}
}

func (m *Model) clearTabActorRedrawPending() {
	if m == nil {
		return
	}
	atomic.StoreUint32(&m.tabActorRedrawPending, 0)
}

func (m *Model) setWorkspace(ws *data.Workspace) {
	m.markHelpDirty()
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
	// Becoming current proves the workspace is alive — clear any delete
	// tombstone so future creates for it aren't dropped.
	delete(m.deletedWorkspaceIDs, m.workspaceIDCached)
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
