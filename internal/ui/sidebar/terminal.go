package sidebar

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
	"github.com/andyrewlee/amux/internal/vterm"
)

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
	Terminal     *pty.Terminal
	VTerm        *vterm.VTerm
	Running      bool
	Detached     bool
	UserDetached bool
	// Reattach is the reattach lock+stamp shared with center agent tabs;
	// see ptyio.ReattachGuard for the sweep contract.
	Reattach    ptyio.ReattachGuard
	SessionName string
	mu          sync.Mutex

	// ptyio.State holds the shared PTY buffering/reader/restart/snapshot
	// bookkeeping (locking owned by mu, as documented on the type).
	ptyio.State

	// Track last size to avoid unnecessary resizes
	lastWidth  int
	lastHeight int

	// Selection state
	Selection          common.SelectionState
	selectionScroll    common.SelectionScrollState
	selectionLastTermX int
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

// TerminalModel is the Bubbletea model for the sidebar terminal section
type TerminalModel struct {
	// State per workspace - multiple tabs per workspace
	tabs    common.TabSet[*TerminalTab]
	tabHits []terminalTabHit // for mouse click handling
	// pendingCreation marks workspaces with a tab creation in flight. Terminal
	// creation is async, so the mark both rejects duplicate creates and serves
	// as the resurrection guard: the result gate accepts a key only when a
	// bucket exists or this mark is set, so a torn-down workspace drops late
	// results without needing a tombstone like center's deletedWorkspaceIDs.
	// The value is the create's start time — a mark older than
	// pendingCreationTimeout means its result was lost (see
	// pendingCreationActive) and must expire rather than wedge.
	pendingCreation map[string]time.Time

	// Current workspace
	workspace *data.Workspace
	// lastActiveAt records when each workspace was last selected, for
	// least-recently-used ordering in EnforceAttachedTerminalTabLimit.
	lastActiveAt map[string]time.Time

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
	tmuxOpts   tmux.Options
	instanceID string

	// sessionEnvProvider composes the layered workspace env for a spawn (the
	// app's process.ScriptRunner.BuildSessionEnv): os.Environ + AMUX_*
	// injected + project env + ws.Env — every user-controlled layer, never
	// repo env. COLORTERM/PATH spawn specifics are appended after it so they
	// always win. Nil keeps the historical minimal env.
	sessionEnvProvider func(ws *data.Workspace) ([]string, error)

	// stylesRev bumps on every SetStyles so the chrome fingerprints
	// (TabBarVersion/StatusLineVersion/HelpVersion) change with the theme.
	stylesRev uint64
	// Build counters; test instrumentation for the compose-time skip gates in
	// internal/app.
	tabBarBuilds uint64
	statusBuilds uint64
	helpBuilds   uint64
}

// NewTerminalModel creates a new sidebar terminal model
func NewTerminalModel() *TerminalModel {
	return &TerminalModel{
		tabs:            common.NewTabSet[*TerminalTab](),
		pendingCreation: make(map[string]time.Time),
		lastActiveAt:    make(map[string]time.Time),
		styles:          common.DefaultStyles(),
		tmuxOpts:        tmux.DefaultOptions(),
	}
}

// SetTmuxOptions stores the resolved tmux options for this model.
func (m *TerminalModel) SetTmuxOptions(opts tmux.Options) {
	m.tmuxOpts = opts
}

// SetInstanceID sets the tmux instance tag for sessions created by this model.
func (m *TerminalModel) SetInstanceID(id string) {
	m.instanceID = id
}

// SetSessionEnvProvider installs the layered session env composer — the
// same contract ScriptRunner gives script spawns minus the trust-gated repo
// layer (see process.BuildSessionEnv for the trust-scope decision). A
// provider error (port-range exhaustion) propagates to the create/reattach
// result rather than silently spawning without the reservation.
func (m *TerminalModel) SetSessionEnvProvider(fn func(ws *data.Workspace) ([]string, error)) {
	m.sessionEnvProvider = fn
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
	m.stylesRev++
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

// pendingCreationTimeout bounds how long a pending-creation mark can gate
// creates before it is treated as lost. A terminal create is subsecond; the
// creation result messages are critical-marked (non-evicting), so a mark
// this old means its result was dropped under queue pressure before the
// critical marking existed, or the producer died mid-flight — either way the
// next ensure must be allowed to retry rather than wedge forever.
const pendingCreationTimeout = 60 * time.Second

// markPendingCreation stamps the create's start time under wsID.
func (m *TerminalModel) markPendingCreation(wsID string) {
	m.pendingCreation[wsID] = time.Now()
}

// pendingCreationActive reports whether wsID has a fresh in-flight create.
// A stale mark is expired in place — lost results must not wedge creation.
func (m *TerminalModel) pendingCreationActive(wsID string) bool {
	at, ok := m.pendingCreation[wsID]
	if !ok {
		return false
	}
	if time.Since(at) > pendingCreationTimeout {
		delete(m.pendingCreation, wsID)
		logging.Warn("sidebar terminal create: pending mark for %s exceeded %s; expiring for retry", wsID, pendingCreationTimeout)
		return false
	}
	return true
}

// setWorkspace sets the current workspace reference.
func (m *TerminalModel) setWorkspace(ws *data.Workspace) {
	m.workspace = ws
	if ws != nil {
		m.lastActiveAt[string(ws.ID())] = time.Now()
	}
}

// getTabs returns the tabs for the current workspace
func (m *TerminalModel) getTabs() []*TerminalTab {
	return m.tabs.Tabs(m.workspaceID())
}

// getActiveTabIdx returns the active tab index for the current workspace
func (m *TerminalModel) getActiveTabIdx() int {
	return m.tabs.ActiveIdx(m.workspaceID())
}

// setActiveTabIdx sets the active tab index for the current workspace
func (m *TerminalModel) setActiveTabIdx(idx int) {
	m.tabs.SetActiveIdx(m.workspaceID(), idx)
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

// HasActiveTerminal reports whether the active tab has a terminal — the
// cheap gate for transcript export visibility (ActiveTranscript does the
// full-buffer extraction; this only checks presence).
func (m *TerminalModel) HasActiveTerminal() bool {
	ts := m.getTerminal()
	if ts == nil {
		return false
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.VTerm != nil
}

// ActiveTranscript returns the active terminal tab's full transcript — the
// combined scrollback+screen buffer as plain text — or "" when no terminal
// is active. The text is captured under the terminal lock and returned;
// callers that copy it to the clipboard must do so after this returns.
func (m *TerminalModel) ActiveTranscript() string {
	ts := m.getTerminal()
	if ts == nil {
		return ""
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	term := ts.VTerm
	if term == nil {
		return ""
	}
	screen, scrollbackLen := term.RenderBuffers()
	total := scrollbackLen + len(screen)
	if total == 0 {
		return ""
	}
	width := term.Width
	if width < 1 {
		width = 1
	}
	return term.GetTextRange(0, 0, width-1, total-1)
}

// getTabByID returns the tab with the given ID, or nil if not found
func (m *TerminalModel) getTabByID(wsID string, tabID TerminalTabID) *TerminalTab {
	for _, tab := range m.tabs.ByWorkspace[wsID] {
		if tab.ID == tabID {
			return tab
		}
	}
	return nil
}

// resolveTabForResult finds the tab an async attach result belongs to,
// preferring the workspace the result was stamped with and falling back to an
// ID-only scan. It returns the key the tab is actually filed under, which is
// the one follow-up work must use.
//
// Unlike the center resolver there is no closed-tab filter: TerminalTab has
// no closed state — closed terminals leave the map entirely.
func (m *TerminalModel) resolveTabForResult(wsID string, tabID TerminalTabID, context string) (*TerminalTab, string) {
	return ptyio.ResolveKeyedTab(m.tabs.ByWorkspace, wsID, tabID,
		func(t *TerminalTab) TerminalTabID { return t.ID },
		func(t *TerminalTab) bool { return t != nil },
		context, "terminal tab")
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
	if len(m.getTabs()) <= 1 {
		return
	}
	if _, ok := m.tabs.NextIdx(m.workspaceID()); ok {
		m.refreshTerminalSize()
	}
}

// PrevTab switches to the previous terminal tab (circular)
func (m *TerminalModel) PrevTab() {
	if len(m.getTabs()) <= 1 {
		return
	}
	if _, ok := m.tabs.PrevIdx(m.workspaceID()); ok {
		m.refreshTerminalSize()
	}
}

// SelectTab selects a tab by index
func (m *TerminalModel) SelectTab(idx int) {
	if m.tabs.SelectIdx(m.workspaceID(), idx) {
		m.refreshTerminalSize()
	}
}

// HasMultipleTabs returns true if there are multiple tabs for the current workspace
func (m *TerminalModel) HasMultipleTabs() bool {
	return len(m.getTabs()) > 1
}

// Focus sets focus state
func (m *TerminalModel) Focus() {
	if m.focused {
		return
	}
	m.focused = true
	m.setActiveTerminalCursorVisibility(true)
}

// Blur removes focus
func (m *TerminalModel) Blur() {
	if !m.focused {
		return
	}
	m.focused = false
	m.setActiveTerminalCursorVisibility(false)
}

// Focused returns whether the terminal is focused
func (m *TerminalModel) Focused() bool {
	return m.focused
}

func (m *TerminalModel) setActiveTerminalCursorVisibility(visible bool) {
	ts := m.getTerminal()
	if ts == nil {
		return
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.VTerm != nil {
		ts.VTerm.ShowCursor = visible
	}
	// Invalidate cached snapshot so focus transitions cannot reuse stale
	// cursor-painted frames.
	ts.ResetSnapshotCache()
}
