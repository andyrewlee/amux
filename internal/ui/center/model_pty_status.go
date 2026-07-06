package center

import (
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/ui/compositor"
)

const tabActiveWindow = 2 * time.Second

// HasRunningAgents returns whether any tab has an active agent across workspaces.
func (m *Model) HasRunningAgents() bool {
	for _, tabs := range m.tabs.ByWorkspace {
		for _, tab := range tabs {
			if tab.isClosed() {
				continue
			}
			if !m.isChatTab(tab) {
				continue
			}
			tab.mu.Lock()
			running := tab.Running
			tab.mu.Unlock()
			if running {
				return true
			}
		}
	}
	return false
}

// HasActiveAgents returns whether any tab has emitted output recently.
// This is used to drive UI activity indicators without relying on process liveness alone.
func (m *Model) HasActiveAgents() bool {
	for _, tabs := range m.tabs.ByWorkspace {
		for _, tab := range tabs {
			if m.IsTabActive(tab) {
				return true
			}
		}
	}
	return false
}

// IsTabActive returns whether a specific tab has emitted output recently.
// This is used for the tab bar spinner animation (shows activity, not just running state).
func (m *Model) IsTabActive(tab *Tab) bool {
	if tab == nil {
		return false
	}
	if tab.isClosed() {
		return false
	}
	if !m.isChatTab(tab) {
		return false
	}
	tab.mu.Lock()
	active := isTabVisiblyActiveLocked(tab, time.Now())
	tab.mu.Unlock()
	return active
}

func isTabVisiblyActiveLocked(tab *Tab, now time.Time) bool {
	if tab == nil || tab.Detached || !tab.Running {
		return false
	}
	return !tab.lastVisibleOutput.IsZero() && now.Sub(tab.lastVisibleOutput) < tabActiveWindow
}

// isTabCursorOutputActiveLocked reports whether recent PTY output should keep
// chat cursor trust constrained even when that output did not mutate visible
// screen content (for example cursor-move or DECTCEM-only sequences).
func isTabCursorOutputActiveLocked(tab *Tab, now time.Time) bool {
	if tab == nil || tab.Detached || !tab.Running || tab.bootstrapActivity {
		return false
	}
	if tab.LastOutputAt.IsZero() || now.Sub(tab.LastOutputAt) >= tabActiveWindow {
		return false
	}
	// Ignore output that is still attributable to the user's own recent edit.
	// That prevents wrapped multiline prompts from becoming cursor-restricted
	// after the short local-input window expires.
	if !tab.lastUserInputAt.IsZero() &&
		!tab.LastOutputAt.Before(tab.lastUserInputAt) &&
		tab.LastOutputAt.Sub(tab.lastUserInputAt) <= localInputEchoSuppressWindow &&
		(tab.lastVisibleOutput.IsZero() || !tab.lastVisibleOutput.After(tab.lastUserInputAt)) {
		return false
	}
	return true
}

// HasActiveAgentsInWorkspace returns whether any tab in a workspace is actively outputting.
func (m *Model) HasActiveAgentsInWorkspace(wsID string) bool {
	for _, tab := range m.tabs.ByWorkspace[wsID] {
		if m.IsTabActive(tab) {
			return true
		}
	}
	return false
}

// GetActiveWorkspaceRoots returns all workspace root paths with active agents.
func (m *Model) GetActiveWorkspaceRoots() []string {
	var active []string
	for wsID, tabs := range m.tabs.ByWorkspace {
		if m.HasActiveAgentsInWorkspace(wsID) {
			// Get the root path from one of the tabs
			for _, tab := range tabs {
				if tab.Workspace != nil {
					active = append(active, tab.Workspace.Root)
					break
				}
			}
		}
	}
	return active
}

// GetActiveWorkspaceIDs returns all workspace IDs with active agents.
func (m *Model) GetActiveWorkspaceIDs() []string {
	var active []string
	for wsID := range m.tabs.ByWorkspace {
		if m.HasActiveAgentsInWorkspace(wsID) {
			active = append(active, wsID)
		}
	}
	return active
}

// GetRunningWorkspaceRoots returns all workspace root paths with running agents.
// This includes agents that are running but idle (waiting at prompt).
func (m *Model) GetRunningWorkspaceRoots() []string {
	var running []string
	for _, tabs := range m.tabs.ByWorkspace {
		for _, tab := range tabs {
			if !m.isChatTab(tab) {
				continue
			}
			tab.mu.Lock()
			isRunning := tab.Running
			var root string
			if tab.Workspace != nil {
				root = tab.Workspace.Root
			}
			tab.mu.Unlock()
			if isRunning && root != "" {
				running = append(running, root)
				break // Only need one per workspace
			}
		}
	}
	return running
}

// FocusedAgentTitle returns the OSC-reported window title of the currently
// displayed agent tab, or "" when there is none. Reads the tab's VTerm under
// tab.mu (the VTerm has no internal lock).
func (m *Model) FocusedAgentTitle() string {
	tabs := m.getTabs()
	idx := m.getActiveTabIdx()
	if idx < 0 || idx >= len(tabs) {
		return ""
	}
	tab := tabs[idx]
	if tab == nil {
		return ""
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if tab.Terminal == nil {
		return ""
	}
	return tab.Terminal.Title()
}

// StartPTYReaders starts reading from all PTYs across all workspaces
func (m *Model) StartPTYReaders() tea.Cmd {
	for wtID, tabs := range m.tabs.ByWorkspace {
		for _, tab := range tabs {
			if tab == nil || tab.isClosed() {
				continue
			}
			tab.mu.Lock()
			readerActive := tab.ReaderActive
			tab.mu.Unlock()
			if readerActive {
				lastBeat := atomic.LoadInt64(&tab.Heartbeat)
				if lastBeat > 0 && time.Since(time.Unix(0, lastBeat)) > ptyReaderStallTimeout {
					logging.Warn("PTY reader stalled for tab %s; restarting", tab.ID)
					m.stopPTYReader(tab)
				}
			}
			_ = m.startPTYReader(wtID, tab)
		}
	}
	return nil
}

// TerminalLayer returns a VTermLayer for the active terminal, if any.
// It snapshots terminal state under lock and reuses cached snapshots when valid.
func (m *Model) TerminalLayer() *compositor.VTermLayer {
	return m.TerminalLayerWithCursorOwner(true)
}

// TerminalLayerWithCursorOwner returns a VTermLayer for the active terminal while
// enforcing whether this pane currently owns cursor rendering.
func (m *Model) TerminalLayerWithCursorOwner(cursorOwner bool) *compositor.VTermLayer {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return nil
	}
	tab := tabs[activeIdx]
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if tab.Terminal == nil {
		return nil
	}
	m.applyTerminalCursorPolicyLocked(tab)
	isChat := m.isChatTabLocked(tab)
	version := tab.Terminal.Version()
	showCursor := m.focused
	if !cursorOwner {
		showCursor = false
	}
	now := time.Now()
	recentLocalInput := false
	restrictCursor := false
	visibleOutputActive := false
	cursorOutputActive := false
	if isChat && showCursor && !tab.Terminal.AltScreen {
		visibleOutputActive = isTabVisiblyActiveLocked(tab, now)
		cursorOutputActive = isTabCursorOutputActiveLocked(tab, now)
		recentLocalInput = isRecentPromptEditInput(tab.lastPromptInputAt, tab.lastPromptSubmitAt, now) ||
			isRecentSubmitPromptBeforeOutput(tab.lastPromptSubmitAt, tab.lastVisibleOutput, now)
		if !recentLocalInput &&
			isRecentLocalChatInput(tab.lastPromptSubmitAt, now) &&
			(!tab.stableCursorSet ||
				isNearbySubmitPromptCursor(
					tab.stableCursorX,
					tab.stableCursorY,
					tab.Terminal.CursorX,
					tab.Terminal.CursorY,
				)) {
			recentLocalInput = true
		}
		restrictCursor = !recentLocalInput && (visibleOutputActive || cursorOutputActive)
	}
	// Cache key: (version, showCursor, recentLocalInput, restrictCursor) for chat
	// tabs. Both recent local input and recent PTY activity can change cursor
	// policy without a PTY version bump.
	if tab.CachedSnap != nil &&
		tab.CachedVersion == version &&
		tab.CachedShowCursor == showCursor &&
		(!isChat ||
			(tab.cachedRecentLocalInput == recentLocalInput &&
				tab.cachedRestrictCursor == restrictCursor)) {
		// Reuse cached snapshot
		perf.Count("vterm_snapshot_cache_hit", 1)
		return compositor.NewVTermLayer(tab.CachedSnap)
	}

	// Chat post-processing below is safe with double buffering: scrolled history
	// forces a full copy, cursor sanitation touches force-dirtied cursor rows,
	// and blink clearing is idempotent for unchanged terminal cells.
	snap := tab.SnapshotBuffer.Snapshot(tab.Terminal, showCursor)
	if snap == nil {
		return nil
	}

	if isChat {
		applyScrolledChatHistoryViewLocked(tab.Terminal, snap)
		liveCursorX, liveCursorY := snap.CursorX, snap.CursorY
		appOwnsCursor := showCursor &&
			!tab.Terminal.AltScreen &&
			snap.CursorHidden &&
			hasChatOwnedCursorGlyph(
				snap,
				liveCursorX,
				liveCursorY,
				tab.stableCursorSet,
				tab.stableCursorX,
				tab.stableCursorY,
			)
		if showCursor && !tab.Terminal.AltScreen {
			if appOwnsCursor {
				snap.ShowCursor = false
			} else {
				m.learnStableChatCursorLocked(tab, snap, version, liveCursorX, liveCursorY, recentLocalInput, restrictCursor, visibleOutputActive, cursorOutputActive)
			}
		}

		// Some assistants paint a synthetic block cursor glyph in the buffer; sanitize it
		// before global blink cleanup, but skip scrollback/history views.
		if snap.ViewOffset == 0 && !appOwnsCursor {
			sanitizeChatCursorCell(snap, liveCursorX, liveCursorY)
			if snap.ShowCursor &&
				tab.stableCursorSet &&
				(tab.stableCursorX != liveCursorX || tab.stableCursorY != liveCursorY) {
				sanitizeStoredChatCursorCell(snap, tab.stableCursorX, tab.stableCursorY)
			}
		}

		// Prevent residual flicker from SGR blink attributes in assistant output.
		for y := range snap.Screen {
			row := snap.Screen[y]
			for x := range row {
				if !row[x].Style.Blink {
					continue
				}
				cell := row[x]
				cell.Style.Blink = false
				row[x] = cell
			}
		}
	}
	perf.Count("vterm_snapshot_cache_miss", 1)

	// Cache the snapshot
	tab.CachedSnap = snap
	tab.CachedVersion = version
	tab.CachedShowCursor = showCursor
	tab.cachedRecentLocalInput = recentLocalInput
	tab.cachedRestrictCursor = restrictCursor

	return compositor.NewVTermLayer(snap)
}
