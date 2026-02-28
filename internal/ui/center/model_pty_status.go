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
	detached := tab.Detached
	running := tab.Running
	lastVisibleOutput := tab.lastVisibleOutput
	tab.mu.Unlock()
	if detached || !running {
		return false
	}
	// Only count visible screen changes as activity.
	return !lastVisibleOutput.IsZero() && time.Since(lastVisibleOutput) < tabActiveWindow
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

func (m *Model) isChatTab(tab *Tab) bool {
	if tab == nil {
		return false
	}
	if tab.DiffViewer != nil {
		return false
	}
	if m != nil && m.config != nil && len(m.config.Assistants) > 0 {
		_, ok := m.config.Assistants[tab.Assistant]
		return ok
	}
	switch tab.Assistant {
	case "claude", "codex", "gemini", "amp", "opencode", "droid", "cline", "cursor", "pi":
		return true
	default:
		return false
	}
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
// This creates a snapshot of the terminal state while holding the lock,
// so the returned layer can be safely used for rendering without locks.
// Uses snapshot caching to avoid recreating when terminal state unchanged.
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

	// Cache key: (version, showCursor). Chat-tab status is deliberately excluded
	// because isChatTab is stable for the lifetime of a tab, and
	// applyTerminalCursorPolicyLocked above ensures IgnoreCursorVisibilityControls
	// is set before the terminal produces version-bumping output.
	version := tab.Terminal.Version()
	showCursor := m.focused
	if !cursorOwner {
		showCursor = false
	}
	if tab.cachedSnap != nil &&
		tab.cachedVersion == version &&
		tab.cachedShowCursor == showCursor {
		// Reuse cached snapshot
		perf.Count("vterm_snapshot_cache_hit", 1)
		return compositor.NewVTermLayer(tab.cachedSnap)
	}

	// Create new snapshot while holding the lock, reusing cached lines when possible.
	snap := compositor.NewVTermSnapshotWithCache(tab.Terminal, showCursor, tab.cachedSnap)
	if snap == nil {
		return nil
	}
	// Keep the cursor steady in coding-agent tabs. Some assistants emit
	// frequent DECTCEM hide/show toggles while streaming output, which causes
	// visible flicker if we honor terminal-driven cursor hiding.
	// These mutations are applied before caching (lines below). On subsequent
	// cache hits the already-normalized snapshot is returned directly, so the
	// transforms are effectively idempotent.
	if m.isChatTab(tab) {
		snap.CursorHidden = false
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
		// Some assistants also paint a synthetic block cursor glyph in the
		// buffer itself. Normalize the active cursor cell so our own steady
		// cursor paint is the only cursor indicator.
		if snap.ViewOffset == 0 &&
			snap.CursorY >= 0 && snap.CursorY < len(snap.Screen) {
			row := snap.Screen[snap.CursorY]
			if snap.CursorX >= 0 && snap.CursorX < len(row) {
				cell := row[snap.CursorX]
				if cell.Rune == '█' {
					cell.Rune = ' '
					if cell.Width <= 0 {
						cell.Width = 1
					}
				}
				cell.Style.Blink = false
				row[snap.CursorX] = cell
			}
		}
	}
	perf.Count("vterm_snapshot_cache_miss", 1)

	// Cache the snapshot
	tab.cachedSnap = snap
	tab.cachedVersion = version
	tab.cachedShowCursor = showCursor

	return compositor.NewVTermLayer(snap)
}
