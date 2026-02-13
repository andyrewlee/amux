package center

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// closeCurrentTab closes the current tab
func (m *Model) closeCurrentTab() tea.Cmd {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()

	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return nil
	}

	return m.closeTabAt(activeIdx)
}

func (m *Model) closeTabAt(index int) tea.Cmd {
	tabs := m.getTabs()
	if len(tabs) == 0 || index < 0 || index >= len(tabs) {
		return nil
	}

	tab := tabs[index]
	tab.markClosing()

	// Capture session info before cleanup for async kill
	sessionName := tab.SessionName
	tmuxOpts := m.getTmuxOptions()

	m.stopPTYReader(tab)

	// Close agent
	if tab.Agent != nil {
		_ = m.agentManager.CloseAgent(tab.Agent)
	}

	tab.mu.Lock()
	if tab.ptyTraceFile != nil {
		_ = tab.ptyTraceFile.Close()
		tab.ptyTraceFile = nil
		tab.ptyTraceClosed = true
	}
	// Clean up viewers and release memory
	// Note: tab.Agent is intentionally NOT niled here to avoid racing with
	// tab_actor which reads it without locking. The agent is already closed
	// via CloseAgent() above; leaving the pointer intact is safe.
	tab.DiffViewer = nil
	tab.Terminal = nil
	tab.cachedSnap = nil
	tab.Workspace = nil
	tab.Running = false
	tab.pendingOutput = nil
	tab.mu.Unlock()
	tab.markClosed()

	// Remove from tabs
	m.removeTab(index)

	// Adjust active tab
	tabs = m.getTabs() // Get updated tabs
	activeIdx := m.getActiveTabIdx()
	if index == activeIdx {
		if activeIdx >= len(tabs) && activeIdx > 0 {
			m.setActiveTabIdx(activeIdx - 1)
		}
	} else if index < activeIdx {
		m.setActiveTabIdx(activeIdx - 1)
	}

	closedCmd := func() tea.Msg {
		return messages.TabClosed{Index: index}
	}

	// Kill tmux session asynchronously to avoid blocking the UI
	if sessionName != "" {
		killCmd := func() tea.Msg {
			_ = tmux.KillSession(sessionName, tmuxOpts)
			return nil
		}
		return tea.Batch(closedCmd, killCmd)
	}

	return closedCmd
}

// hasActiveAgent returns whether there's an active agent
func (m *Model) hasActiveAgent() bool {
	tabs := m.getTabs()
	return len(tabs) > 0 && m.getActiveTabIdx() < len(tabs)
}

// nextTab switches to the next tab
func (m *Model) nextTab() {
	tabs := m.getTabs()
	if len(tabs) > 0 {
		m.setActiveTabIdx((m.getActiveTabIdx() + 1) % len(tabs))
	}
}

// prevTab switches to the previous tab
func (m *Model) prevTab() {
	tabs := m.getTabs()
	if len(tabs) > 0 {
		idx := m.getActiveTabIdx() - 1
		if idx < 0 {
			idx = len(tabs) - 1
		}
		m.setActiveTabIdx(idx)
	}
}

func (m *Model) reattachActiveTabIfDetached() tea.Cmd {
	activeIdx := m.getActiveTabIdx()
	tabs := m.getTabs()
	if len(tabs) == 0 || activeIdx < 0 || activeIdx >= len(tabs) {
		return nil
	}
	tab := tabs[activeIdx]
	if tab == nil || tab.isClosed() {
		return nil
	}

	tab.mu.Lock()
	detached := tab.Detached
	reattachInFlight := tab.reattachInFlight
	hasDiffViewer := tab.DiffViewer != nil
	assistant := tab.Assistant
	tab.mu.Unlock()
	if !detached || reattachInFlight || hasDiffViewer {
		return nil
	}

	isChatAssistant := false
	if m != nil && m.config != nil && len(m.config.Assistants) > 0 {
		_, isChatAssistant = m.config.Assistants[assistant]
	} else {
		switch assistant {
		case "claude", "codex", "gemini", "amp", "opencode", "droid", "cursor":
			isChatAssistant = true
		}
	}
	if !isChatAssistant {
		return nil
	}
	return m.ReattachActiveTab()
}

// ReattachActiveTabIfDetached attempts reattach only when the active tab is a
// detached assistant/chat tab. It is safe to call from automatic UI flows.
func (m *Model) ReattachActiveTabIfDetached() tea.Cmd {
	return m.reattachActiveTabIfDetached()
}

func (m *Model) tabSelectionCommand() tea.Cmd {
	return common.SafeBatch(
		m.tabSelectionChangedCmd(),
		m.reattachActiveTabIfDetached(),
	)
}

// Public wrappers for prefix mode commands

// NextTab switches to the next tab (public wrapper)
func (m *Model) NextTab() tea.Cmd {
	m.nextTab()
	return m.tabSelectionCommand()
}

// PrevTab switches to the previous tab (public wrapper)
func (m *Model) PrevTab() tea.Cmd {
	m.prevTab()
	return m.tabSelectionCommand()
}

// CloseActiveTab closes the current tab (public wrapper)
func (m *Model) CloseActiveTab() tea.Cmd {
	return m.closeCurrentTab()
}

// SelectTab switches to a specific tab by index (0-indexed)
func (m *Model) SelectTab(index int) tea.Cmd {
	tabs := m.getTabs()
	if index >= 0 && index < len(tabs) {
		m.setActiveTabIdx(index)
		return m.tabSelectionCommand()
	}
	return nil
}

// SendToTerminal sends a string directly to the active terminal
func (m *Model) SendToTerminal(s string) {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return
	}
	tab := tabs[activeIdx]
	if tab.isClosed() {
		return
	}
	tab.mu.Lock()
	agent := tab.Agent
	tab.mu.Unlock()
	if agent != nil && agent.Terminal != nil {
		if err := agent.Terminal.SendString(s); err != nil {
			logging.Warn("SendToTerminal failed for tab %s: %v", tab.ID, err)
			tab.mu.Lock()
			tab.Running = false
			tab.Detached = true
			tab.mu.Unlock()
		}
	}
}

// GetTabsInfo returns information about current tabs for persistence
func (m *Model) GetTabsInfo() ([]data.TabInfo, int) {
	var result []data.TabInfo
	tabs := m.getTabs()
	for _, tab := range tabs {
		if tab == nil {
			continue
		}
		tab.mu.Lock()
		running := tab.Running
		detached := tab.Detached
		sessionName := tab.SessionName
		if sessionName == "" && tab.Agent != nil {
			sessionName = tab.Agent.Session
		}
		tab.mu.Unlock()
		status := "stopped"
		if detached {
			status = "detached"
		} else if running {
			status = "running"
		}
		result = append(result, data.TabInfo{
			Assistant:   tab.Assistant,
			Name:        tab.Name,
			SessionName: sessionName,
			Status:      status,
			CreatedAt:   tab.createdAt,
		})
	}
	return result, m.getActiveTabIdx()
}

// GetTabsInfoForWorkspace returns tab information for a specific workspace ID.
func (m *Model) GetTabsInfoForWorkspace(wsID string) ([]data.TabInfo, int) {
	var result []data.TabInfo
	tabs := m.tabsByWorkspace[wsID]
	for _, tab := range tabs {
		if tab == nil {
			continue
		}
		tab.mu.Lock()
		running := tab.Running
		detached := tab.Detached
		sessionName := tab.SessionName
		if sessionName == "" && tab.Agent != nil {
			sessionName = tab.Agent.Session
		}
		tab.mu.Unlock()
		status := "stopped"
		if detached {
			status = "detached"
		} else if running {
			status = "running"
		}
		result = append(result, data.TabInfo{
			Assistant:   tab.Assistant,
			Name:        tab.Name,
			SessionName: sessionName,
			Status:      status,
			CreatedAt:   tab.createdAt,
		})
	}
	return result, m.activeTabByWorkspace[wsID]
}

// HasWorkspaceState reports whether the model has tab state for a workspace.
// True means tabs were explicitly managed (even if currently empty).
func (m *Model) HasWorkspaceState(wsID string) bool {
	_, ok := m.tabsByWorkspace[wsID]
	return ok
}

// HasDiffViewer returns true if the active tab has a diff viewer.
func (m *Model) HasDiffViewer() bool {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return false
	}
	tab := tabs[activeIdx]
	if tab.isClosed() {
		return false
	}
	tab.mu.Lock()
	defer tab.mu.Unlock()
	return tab.DiffViewer != nil
}

// CloseAllTabs is deprecated - tabs now persist per-workspace
// This is kept for compatibility but does nothing
func (m *Model) CloseAllTabs() {
	// No-op: tabs now persist per-workspace and are not closed when switching
}
