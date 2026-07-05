package center

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/diff"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
	"github.com/andyrewlee/amux/internal/vterm"
)

// TabID is a unique identifier for a tab that survives slice reordering
type TabID string

// tabIDCounter is used to generate unique tab IDs
var (
	tabIDCounter uint64
	tabIDPrefix  = newTabIDPrefix()
)

func newTabIDPrefix() string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err == nil {
		return hex.EncodeToString(buf)
	}
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

func formatTabID(prefix string, id uint64) TabID {
	return TabID("tab-" + prefix + "-" + strconv.FormatUint(id, 36))
}

// generateTabID creates a new unique tab ID
func generateTabID() TabID {
	id := atomic.AddUint64(&tabIDCounter, 1)
	return formatTabID(tabIDPrefix, id)
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
	reattachInFlight bool
	Terminal         *vterm.VTerm // Virtual terminal emulator with scrollback
	DiffViewer       *diff.Model  // Native diff viewer (replaces PTY-based viewer)
	mu               sync.Mutex   // Protects Terminal, Agent, Running, Detached, Workspace, DiffViewer and the embedded state groups
	closed           uint32
	closing          uint32
	Running          bool // Whether the agent is actively running

	// ptyio.State holds the shared PTY buffering/reader/restart/snapshot
	// bookkeeping (locking owned by mu, as documented on the type).
	ptyio.State

	pendingOutputBytes   int
	catchUpPendingOutput bool
	catchUpTargetBytes   uint64
	ptyBytesReceived     uint64
	ptyBytesSettled      uint64

	// Embedded state groups; fields are promoted so accesses read the same as
	// before the decomposition. Locking follows the same rule as the flat
	// fields did: guarded by mu unless documented atomic.
	tabActivityState
	tabActorWriteState
	tabCursorState

	ptyRows int
	ptyCols int
	// Mouse selection state
	Selection          common.SelectionState
	selectionScroll    common.SelectionScrollState
	selectionLastTermX int

	ptyTraceFile   *os.File
	ptyTraceBytes  int
	ptyTraceClosed bool
	lastFocusedAt  time.Time

	createdAt int64 // Unix timestamp for ordering; persisted in workspace.json
}

// tabActivityState groups chat-activity detection state: visible-output
// tracking, the activity digest, bootstrap windows and prompt timing.
type tabActivityState struct {
	lastVisibleOutput      time.Time
	pendingVisibleOutput   bool
	pendingVisibleSeq      uint64
	activityDigest         [16]byte
	activityDigestInit     bool
	lastActivityTagAt      time.Time
	activityANSIState      ansiActivityState
	lastInputTagAt         time.Time
	lastUserInputAt        time.Time
	bootstrapActivity      bool
	bootstrapLastOutputAt  time.Time
	postWriteVisibleState  uint32 // atomic
	lastPromptInputAt      time.Time
	lastPromptSubmitAt     time.Time
	pendingSubmitPasteEcho string
}

// tabActorWriteState groups the tab-actor write pipeline state: queued write
// accounting and the parser carry/reset flow (see model_tab_phase.go for how
// parserResetPending gates flushing).
type tabActorWriteState struct {
	parserResetPending       bool
	actorWritesPending       int
	actorQueuedBytes         int
	actorWriteEpoch          uint64
	actorQueuedCarry         vterm.ParserCarryState
	actorQueuedNoiseTrailing []byte
}

// tabCursorState groups cursor stabilization/refresh state used by the chat
// cursor overlay.
type tabCursorState struct {
	cachedRecentLocalInput   bool
	cachedRestrictCursor     bool
	cursorRefreshGen         uint64
	cursorRefreshPending     bool
	cursorRefreshAt          time.Time
	stableCursorSet          bool
	stableCursorX            int
	stableCursorY            int
	stableCursorVersion      uint64
	lastRestrictedVersion    uint64
	pendingIdleCursorRelearn bool
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

func (t *Tab) consumeActivityVisibility(data []byte) bool {
	if t == nil || len(data) == 0 {
		return false
	}
	t.mu.Lock()
	visible, next := hasVisiblePTYOutput(data, t.activityANSIState)
	t.activityANSIState = next
	t.mu.Unlock()
	return visible
}

func (t *Tab) resetActivityANSIState() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.activityANSIState = ansiActivityText
	t.mu.Unlock()
}

func (t *Tab) setPostWriteVisible(visible bool) {
	if t == nil {
		return
	}
	if visible {
		atomic.StoreUint32(&t.postWriteVisibleState, 1)
		return
	}
	atomic.StoreUint32(&t.postWriteVisibleState, 0)
}

func (t *Tab) postWriteVisible() bool {
	if t == nil {
		return false
	}
	return atomic.LoadUint32(&t.postWriteVisibleState) == 1
}

func (t *Tab) normalizePTYAccountingLocked() {
	if t == nil {
		return
	}
	if t.ptyBytesReceived < t.ptyBytesSettled {
		t.ptyBytesReceived = t.ptyBytesSettled
	}
	outstanding := nonNegativeByteCount(t.pendingOutputBytes)
	outstanding += nonNegativeByteCount(t.actorQueuedBytes)
	minReceived := t.ptyBytesSettled + outstanding
	if t.ptyBytesReceived < minReceived {
		t.ptyBytesReceived = minReceived
	}
}

func nonNegativeByteCount(v int) uint64 {
	if v <= 0 {
		return 0
	}
	return uint64(v)
}

func (t *Tab) resetPTYStateLocked() {
	if t == nil {
		return
	}
	t.PendingOutput = nil
	t.pendingOutputBytes = 0
	t.clearCatchUpLocked()
	t.ptyBytesReceived = 0
	t.ptyBytesSettled = 0
	t.NoiseTrailing = nil
	t.actorQueuedBytes = 0
}

func (t *Tab) clearCatchUpLocked() {
	if t == nil {
		return
	}
	t.catchUpPendingOutput = false
	t.catchUpTargetBytes = 0
}

// resetActorWriteStateLocked resets all per-(re)attach actor-write and parser
// state for a terminal that is being attached or recreated. The caller must
// hold t.mu and t.Terminal must be non-nil (the final line reads
// t.Terminal.ParserCarryState()). This sequence was duplicated byte-for-byte at
// the reattach and recreate sites; a field addition or ordering fix now happens
// once rather than being mirrored across both.
func (t *Tab) resetActorWriteStateLocked() {
	t.parserResetPending = false
	t.settlePTYBytesLocked(t.actorQueuedBytes)
	t.actorQueuedBytes = 0
	t.actorWritesPending = 0
	t.actorWriteEpoch++
	t.clearCatchUpLocked()
	t.pendingOutputBytes = len(t.PendingOutput)
	t.OverflowTrimCarry = vterm.ParserCarryState{}
	t.NoiseTrailing = nil
	t.actorQueuedNoiseTrailing = t.actorQueuedNoiseTrailing[:0]
	t.actorQueuedCarry = t.Terminal.ParserCarryState()
}

func (t *Tab) expireCatchUpLocked() {
	if t == nil {
		return
	}
	if t.catchUpPendingOutput && t.ptyBytesSettled >= t.catchUpTargetBytes {
		t.clearCatchUpLocked()
	}
}

func (t *Tab) catchUpActiveLocked() bool {
	if t == nil {
		return false
	}
	t.normalizePTYAccountingLocked()
	return t.catchUpPendingOutput && t.ptyBytesSettled < t.catchUpTargetBytes
}

func (t *Tab) latchCatchUpLocked() bool {
	if t == nil {
		return false
	}
	t.normalizePTYAccountingLocked()
	t.expireCatchUpLocked()
	if t.ptyBytesSettled >= t.ptyBytesReceived {
		t.clearCatchUpLocked()
		return false
	}
	t.catchUpPendingOutput = true
	t.catchUpTargetBytes = t.ptyBytesReceived
	return true
}

func (t *Tab) settlePTYBytesLocked(n int) (before, after bool) {
	if t == nil {
		return false, false
	}
	before = t.catchUpActiveLocked()
	if n > 0 {
		t.normalizePTYAccountingLocked()
		t.ptyBytesSettled += uint64(n)
		if t.ptyBytesSettled > t.ptyBytesReceived {
			t.ptyBytesSettled = t.ptyBytesReceived
		}
	}
	t.expireCatchUpLocked()
	after = t.catchUpActiveLocked()
	return before, after
}

// getTabs returns the tabs for the current workspace
func (m *Model) getTabs() []*Tab {
	return m.tabs.ByWorkspace[m.workspaceID()]
}

// getTabByID returns the tab with the given ID, or nil if not found
func (m *Model) getTabByID(wsID string, tabID TabID) *Tab {
	for _, tab := range m.tabs.ByWorkspace[wsID] {
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
	for _, tab := range m.tabs.ByWorkspace[wsID] {
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
	return m.tabs.ActiveByWorkspace[m.workspaceID()]
}

// setActiveTabIdx sets the active tab index for the current workspace
func (m *Model) setActiveTabIdx(idx int) {
	m.setActiveTabIdxForWorkspace(m.workspaceID(), idx)
}

func (m *Model) setActiveTabIdxForWorkspace(wsID string, idx int) {
	if wsID == "" {
		return
	}
	m.tabs.ActiveByWorkspace[wsID] = idx
	m.syncPostWriteVisibility()
	m.markTabFocused(wsID, idx)
}

func (m *Model) markTabFocused(wsID string, idx int) {
	tabs := m.tabs.ByWorkspace[wsID]
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
	m.syncPostWriteVisibility()
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

func (m *Model) syncPostWriteVisibility() {
	activeWSID := m.workspaceID()
	activeIdx := -1
	if activeWSID != "" {
		activeIdx = m.tabs.ActiveByWorkspace[activeWSID]
	}
	for wsID, tabs := range m.tabs.ByWorkspace {
		for idx, tab := range tabs {
			if tab == nil || tab.isClosed() {
				continue
			}
			tab.setPostWriteVisible(wsID == activeWSID && idx == activeIdx)
		}
	}
}

// removeTab removes a tab at index from the current workspace
func (m *Model) removeTab(idx int) {
	wsID := m.workspaceID()
	tabs := m.tabs.ByWorkspace[wsID]
	if idx >= 0 && idx < len(tabs) {
		m.tabs.ByWorkspace[wsID] = append(tabs[:idx], tabs[idx+1:]...)
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
	for _, tab := range m.tabs.ByWorkspace[wsID] {
		tab.markClosing()
		m.stopPTYReader(tab)
		tab.mu.Lock()
		if tab.ptyTraceFile != nil {
			_ = tab.ptyTraceFile.Close()
			tab.ptyTraceFile = nil
			tab.ptyTraceClosed = true
		}
		tab.resetPTYStateLocked()
		tab.DiffViewer = nil
		tab.Terminal = nil
		tab.CachedSnap = nil
		tab.Workspace = nil
		tab.Running = false
		tab.mu.Unlock()
		tab.markClosed()
	}

	m.tabs.DeleteWorkspace(wsID)
	m.noteTabsChanged()

	// Also cleanup agents for this workspace
	if m.agentManager != nil {
		m.agentManager.CloseWorkspaceAgents(ws)
	}
}
