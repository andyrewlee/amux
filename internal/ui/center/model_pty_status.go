package center

import (
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/ui/compositor"
)

// HasRunningAgents returns whether any tab has an active agent across workspaces.
func (m *Model) HasRunningAgents() bool {
	for _, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			if tab.isClosed() {
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
	now := time.Now()
	for _, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			if tab.isClosed() {
				continue
			}
			if !tab.Running {
				continue
			}
			if tab.flushScheduled || len(tab.pendingOutput) > 0 {
				return true
			}
			if !tab.lastOutputAt.IsZero() && now.Sub(tab.lastOutputAt) < 2*time.Second {
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
	// Check Running state and output state together to avoid race condition
	// Note: These fields are accessed from the main update goroutine
	if !tab.Running {
		return false
	}
	// Check buffered output or recent output timestamp
	if tab.flushScheduled || len(tab.pendingOutput) > 0 {
		return true
	}
	return !tab.lastOutputAt.IsZero() && time.Since(tab.lastOutputAt) < 2*time.Second
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

// GetRunningWorkspaceRoots returns all workspace root paths with running agents.
// This includes agents that are running but idle (waiting at prompt).
func (m *Model) GetRunningWorkspaceRoots() []string {
	var running []string
	for _, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
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
// This creates a snapshot of the terminal state while holding the lock,
// so the returned layer can be safely used for rendering without locks.
// Uses snapshot caching to avoid recreating when terminal state unchanged.
func (m *Model) TerminalLayer() *compositor.VTermLayer {
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

	// Check if we can reuse the cached snapshot
	version := tab.Terminal.Version()
	showCursor := m.focused
	if tab.cachedSnap != nil &&
		tab.cachedVersion == version &&
		tab.cachedShowCursor == showCursor {
		// Reuse cached snapshot
		return compositor.NewVTermLayer(tab.cachedSnap)
	}

	// Create new snapshot while holding the lock, reusing cached lines when possible.
	snap := compositor.NewVTermSnapshotWithCache(tab.Terminal, showCursor, tab.cachedSnap)
	if snap == nil {
		return nil
	}

	// Cache the snapshot
	tab.cachedSnap = snap
	tab.cachedVersion = version
	tab.cachedShowCursor = showCursor

	return compositor.NewVTermLayer(snap)
}
