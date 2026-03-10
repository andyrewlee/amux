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

// isChatInputCursorPosition reports whether the cursor is in the visible chat
// input area, using a stricter bottom-band heuristic during active output.
func isChatInputCursorPosition(snap *compositor.VTermSnapshot, x, y int, allowFullViewport bool) bool {
	if snap == nil || snap.ViewOffset != 0 {
		return false
	}
	if y < 0 || y >= len(snap.Screen) {
		return false
	}
	row := snap.Screen[y]
	if x < 0 || x >= len(row) {
		return false
	}
	if allowFullViewport {
		return true
	}
	height := len(snap.Screen)
	if height <= 4 {
		return true
	}
	band := height / 4
	if band < 2 {
		band = 2
	}
	return y >= height-band
}

// isRenderableChatCursorPosition returns whether a live terminal cursor is
// sane enough to adopt as the chat input cursor.
func isRenderableChatCursorPosition(
	snap *compositor.VTermSnapshot,
	x, y int,
	allowFullViewport bool,
	allowBlankCorner bool,
) bool {
	if !isChatInputCursorPosition(snap, x, y, allowFullViewport) {
		return false
	}
	if isSuspiciousBottomEdgeCornerCursor(snap, x, y) && !allowBlankCorner {
		return false
	}
	return true
}

// isStoredChatCursorPosition returns whether a stored chat cursor still fits in
// the current terminal viewport and remains inside the input section.
func isStoredChatCursorPosition(snap *compositor.VTermSnapshot, x, y int, allowFullViewport bool) bool {
	if snap == nil {
		return false
	}
	if snap.ViewOffset != 0 {
		return x >= 0 && y >= 0
	}
	if y < 0 || y >= len(snap.Screen) {
		return false
	}
	row := snap.Screen[y]
	if x < 0 || x >= len(row) {
		return false
	}
	return isChatInputCursorPosition(snap, x, y, allowFullViewport)
}

// isBottomEdgeCornerPosition reports whether (x,y) is either bottom-left or
// bottom-right corner of the snapshot viewport.
func isBottomEdgeCornerPosition(snap *compositor.VTermSnapshot, x, y int) bool {
	if snap == nil || len(snap.Screen) == 0 {
		return false
	}
	lastY := len(snap.Screen) - 1
	if y != lastY {
		return false
	}
	row := snap.Screen[lastY]
	if len(row) == 0 {
		return false
	}
	lastX := len(row) - 1
	return x == 0 || x == lastX
}

// isSuspiciousBottomEdgeCornerCursor reports a common PTY cursor artifact where
// cursor state lands on an empty corner cell on the bottom row.
func isSuspiciousBottomEdgeCornerCursor(snap *compositor.VTermSnapshot, x, y int) bool {
	if !isBottomEdgeCornerPosition(snap, x, y) {
		return false
	}
	row := snap.Screen[len(snap.Screen)-1]
	cell := row[x]
	return cell.Rune == 0 || cell.Rune == ' '
}

// HasRunningAgents returns whether any tab has an active agent across workspaces.
func (m *Model) HasRunningAgents() bool {
	for _, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			if tab.isClosed() {
				continue
			}
			if !m.isChatTab(tab) {
				continue
			}
			if tab.Running {
				return true
			}
		}
	}
	return false
}

// HasActiveAgents returns whether any tab has emitted output recently.
// This is used to drive UI activity indicators without relying on process liveness alone.
func (m *Model) HasActiveAgents() bool {
	for _, tabs := range m.tabsByWorkspace {
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
	if tab.lastOutputAt.IsZero() || now.Sub(tab.lastOutputAt) >= tabActiveWindow {
		return false
	}
	// Ignore output that is still attributable to the user's own recent edit.
	// That prevents wrapped multiline prompts from becoming cursor-restricted
	// after the short local-input window expires.
	if !tab.lastUserInputAt.IsZero() &&
		!tab.lastOutputAt.Before(tab.lastUserInputAt) &&
		tab.lastOutputAt.Sub(tab.lastUserInputAt) <= localInputEchoSuppressWindow &&
		(tab.lastVisibleOutput.IsZero() || !tab.lastVisibleOutput.After(tab.lastUserInputAt)) {
		return false
	}
	return true
}

// HasActiveAgentsInWorkspace returns whether any tab in a workspace is actively outputting.
func (m *Model) HasActiveAgentsInWorkspace(wsID string) bool {
	for _, tab := range m.tabsByWorkspace[wsID] {
		if m.IsTabActive(tab) {
			return true
		}
	}
	return false
}

// GetActiveWorkspaceRoots returns all workspace root paths with active agents.
func (m *Model) GetActiveWorkspaceRoots() []string {
	var active []string
	for wsID, tabs := range m.tabsByWorkspace {
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
	for wsID := range m.tabsByWorkspace {
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
	for _, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			if !m.isChatTab(tab) {
				continue
			}
			if tab.Running && tab.Workspace != nil {
				running = append(running, tab.Workspace.Root)
				break // Only need one per workspace
			}
		}
	}
	return running
}

// StartPTYReaders starts reading from all PTYs across all workspaces
func (m *Model) StartPTYReaders() tea.Cmd {
	if m.isTabActorReady() {
		lastBeat := atomic.LoadInt64(&m.tabActorHeartbeat)
		if lastBeat > 0 && time.Since(time.Unix(0, lastBeat)) > tabActorStallTimeout {
			logging.Warn("tab actor stalled; clearing readiness for restart")
			atomic.StoreUint32(&m.tabActorReady, 0)
		}
	}
	for wtID, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			if tab == nil || tab.isClosed() {
				continue
			}
			tab.mu.Lock()
			readerActive := tab.readerActive
			tab.mu.Unlock()
			if readerActive {
				lastBeat := atomic.LoadInt64(&tab.ptyHeartbeat)
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
	if tab.cachedSnap != nil &&
		tab.cachedVersion == version &&
		tab.cachedShowCursor == showCursor &&
		(!isChat ||
			(tab.cachedRecentLocalInput == recentLocalInput &&
				tab.cachedRestrictCursor == restrictCursor)) {
		// Reuse cached snapshot
		perf.Count("vterm_snapshot_cache_hit", 1)
		return compositor.NewVTermLayer(tab.cachedSnap)
	}

	// Create new snapshot while holding the lock.
	// Do not pass the previous snapshot for reuse: NewVTermSnapshotWithCache
	// mutates the provided snapshot/rows in-place, which can mutate a snapshot
	// already handed to a previously returned layer.
	snap := compositor.NewVTermSnapshot(tab.Terminal, showCursor)
	if snap == nil {
		return nil
	}

	if isChat {
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
				snap.CursorHidden = false
				trustFullViewport := !restrictCursor
				if restrictCursor {
					tab.lastRestrictedVersion = version
					if cursorOutputActive && !visibleOutputActive && tab.stableCursorSet {
						tab.pendingIdleCursorRelearn = true
					}
				}
				versionChangedFromStable := version != tab.stableCursorVersion
				idleSameVersionRelearn := trustFullViewport &&
					tab.stableCursorSet &&
					!recentLocalInput &&
					version == tab.lastRestrictedVersion &&
					(tab.pendingIdleCursorRelearn || tab.stableCursorVersion == 0) &&
					hasChatCursorContextNearPosition(snap, liveCursorY)
				plausibleInitialCursor := isPlausibleInitialChatCursor(snap, liveCursorX, liveCursorY)
				initialFullViewportLearn := !tab.stableCursorSet && plausibleInitialCursor
				learnFullViewport := trustFullViewport &&
					(initialFullViewportLearn ||
						(tab.stableCursorSet && recentLocalInput) ||
						idleSameVersionRelearn ||
						(versionChangedFromStable && version != tab.lastRestrictedVersion))
				liveCursorVisible := isChatInputCursorPosition(snap, liveCursorX, liveCursorY, trustFullViewport)
				liveCursorDisplayable := liveCursorVisible &&
					(!restrictCursor || !isSuspiciousBottomEdgeCornerCursor(snap, liveCursorX, liveCursorY)) &&
					(tab.stableCursorSet || !trustFullViewport || plausibleInitialCursor)
				liveCursorRenderable := isRenderableChatCursorPosition(
					snap,
					liveCursorX,
					liveCursorY,
					learnFullViewport,
					recentLocalInput,
				)
				storedCursorInViewport := tab.stableCursorSet &&
					isStoredChatCursorPosition(snap, tab.stableCursorX, tab.stableCursorY, true)
				storedCursorVisible := tab.stableCursorSet &&
					isStoredChatCursorPosition(snap, tab.stableCursorX, tab.stableCursorY, trustFullViewport)
				// Stable chat cursor learning lives in the render path because it
				// depends on the fully materialized snapshot and the current cursor
				// trust policy. The cache key above prevents repeated View passes
				// from churning this state when those inputs have not changed.
				if liveCursorRenderable {
					tab.stableCursorSet = true
					tab.stableCursorX = liveCursorX
					tab.stableCursorY = liveCursorY
					tab.stableCursorVersion = version
					tab.pendingIdleCursorRelearn = false
				} else if tab.stableCursorSet &&
					tab.stableCursorVersion == 0 &&
					trustFullViewport &&
					storedCursorVisible {
					tab.stableCursorVersion = version
				} else if tab.stableCursorSet &&
					!storedCursorInViewport {
					tab.stableCursorSet = false
					tab.stableCursorVersion = 0
					tab.pendingIdleCursorRelearn = false
				}

				switch {
				case tab.stableCursorSet:
					snap.CursorX = tab.stableCursorX
					snap.CursorY = tab.stableCursorY
				case liveCursorDisplayable:
					// Leave the live cursor in place until we learn a stable input-band position.
				default:
					snap.ShowCursor = false
				}
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
	tab.cachedSnap = snap
	tab.cachedVersion = version
	tab.cachedShowCursor = showCursor
	tab.cachedRecentLocalInput = recentLocalInput
	tab.cachedRestrictCursor = restrictCursor

	return compositor.NewVTermLayer(snap)
}
