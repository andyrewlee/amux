package center

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// detachTab is the core implementation for detaching a tab (closes PTY, keeps tmux session).
func (m *Model) detachTab(tab *Tab, index int) tea.Cmd {
	if tab == nil {
		return nil
	}
	if tab.DiffViewer != nil {
		return func() tea.Msg {
			return messages.Toast{
				Message: "Diff tabs cannot be detached",
				Level:   messages.ToastInfo,
			}
		}
	}
	if !m.isChatTab(tab) {
		return func() tea.Msg {
			return messages.Toast{
				Message: "Only assistant tabs can be detached",
				Level:   messages.ToastInfo,
			}
		}
	}
	tab.mu.Lock()
	alreadyDetached := tab.Detached
	hasAgent := tab.Agent != nil
	tab.mu.Unlock()
	if alreadyDetached && !hasAgent {
		return nil
	}
	m.stopPTYReader(tab)
	tab.mu.Lock()
	tab.Running = false
	tab.Detached = true
	tab.reattachInFlight = false
	tab.pendingOutput = nil
	if tab.Agent != nil && tab.SessionName == "" {
		tab.SessionName = tab.Agent.Session
	}
	agent := tab.Agent
	tab.Agent = nil
	tab.mu.Unlock()
	if agent != nil {
		_ = m.agentManager.CloseAgent(agent)
	}
	workspaceID := ""
	if tab.Workspace != nil {
		workspaceID = string(tab.Workspace.ID())
	}
	return func() tea.Msg {
		return messages.TabDetached{WorkspaceID: workspaceID, Index: index}
	}
}

func (m *Model) detachTabAt(index int) tea.Cmd {
	tabs := m.getTabs()
	if len(tabs) == 0 || index < 0 || index >= len(tabs) {
		return nil
	}
	return m.detachTab(tabs[index], index)
}

// DetachTabByID closes the PTY client for a specific tab and keeps the tmux session alive.
func (m *Model) DetachTabByID(wsID string, tabID TabID) tea.Cmd {
	if wsID == "" {
		return nil
	}
	tabs := m.tabsByWorkspace[wsID]
	for idx, tab := range tabs {
		if tab == nil || tab.isClosed() || tab.ID != tabID {
			continue
		}
		return m.detachTab(tab, idx)
	}
	return nil
}

// DetachActiveTab closes the PTY client but keeps the tmux session alive.
func (m *Model) DetachActiveTab() tea.Cmd {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return nil
	}
	return m.detachTabAt(activeIdx)
}

func (m *Model) tabSelectionChangedCmd(changed bool) tea.Cmd {
	if !changed {
		return nil
	}
	wsID := m.workspaceID()
	if wsID == "" {
		return nil
	}
	return common.SafeBatch(
		func() tea.Msg {
			return messages.TabSelectionChanged{
				WorkspaceID: wsID,
				ActiveIndex: m.getActiveTabIdx(),
			}
		},
		m.flushActiveTabBacklogCmd(),
		m.autoReattachActiveTabOnSelection(),
	)
}

func (m *Model) flushActiveTabBacklogCmd() tea.Cmd {
	wsID := m.workspaceID()
	if wsID == "" {
		return nil
	}
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if activeIdx < 0 || activeIdx >= len(tabs) {
		return nil
	}
	tab := tabs[activeIdx]
	if tab == nil || tab.isClosed() || len(tab.pendingOutput) == 0 {
		return nil
	}
	tabID := tab.ID
	return func() tea.Msg {
		return PTYFlush{WorkspaceID: wsID, TabID: tabID}
	}
}

func (m *Model) autoReattachActiveTabOnSelection() tea.Cmd {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx < 0 || activeIdx >= len(tabs) {
		return nil
	}
	tab := tabs[activeIdx]
	if tab == nil {
		return nil
	}
	tab.mu.Lock()
	detached := tab.Detached
	tab.mu.Unlock()
	if !detached {
		return nil
	}
	return m.ReattachActiveTab()
}

// RestoreTabsFromWorkspace recreates tabs from persisted workspace metadata.
// Only agent tabs with known assistants are restored.
func (m *Model) RestoreTabsFromWorkspace(ws *data.Workspace) tea.Cmd {
	if ws == nil || len(ws.OpenTabs) == 0 {
		return nil
	}
	wsID := string(ws.ID())
	if len(m.tabsByWorkspace[wsID]) > 0 {
		return nil
	}

	var cmds []tea.Cmd
	restoreCount := 0
	lastBeforeActive := -1
	activeIdx := ws.ActiveTabIndex
	for i, tab := range ws.OpenTabs {
		if tab.Assistant == "" {
			continue
		}
		if m.config == nil || m.config.Assistants == nil {
			continue
		}
		if _, ok := m.config.Assistants[tab.Assistant]; !ok {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(tab.Status))
		if status == "stopped" {
			continue
		}
		if i <= activeIdx {
			lastBeforeActive = restoreCount
		}
		if status == "detached" {
			m.addDetachedTab(ws, tab)
			restoreCount++
			continue
		}
		restoreCount++
		tabID, sessionName := m.addPlaceholderTab(ws, tab)
		cmds = append(cmds, m.reattachToSession(ws, tabID, tab.Assistant, sessionName))
	}
	if restoreCount > 0 {
		desired := lastBeforeActive
		if desired < 0 {
			desired = 0
		}
		if desired >= restoreCount {
			desired = restoreCount - 1
		}
		m.setActiveTabIdxForWorkspace(wsID, desired)
	}
	return common.SafeBatch(cmds...)
}

// AddTabsFromWorkspace adds new tabs without resetting existing UI state.
func (m *Model) AddTabsFromWorkspace(ws *data.Workspace, tabs []data.TabInfo) tea.Cmd {
	if ws == nil || len(tabs) == 0 {
		return nil
	}
	if m.config == nil || m.config.Assistants == nil {
		return nil
	}
	wsID := string(ws.ID())
	existing := make(map[string]struct{}, len(m.tabsByWorkspace[wsID]))
	for _, tab := range m.tabsByWorkspace[wsID] {
		if tab == nil || tab.isClosed() {
			continue
		}
		sessionName := strings.TrimSpace(tab.SessionName)
		if sessionName == "" && tab.Agent != nil {
			sessionName = strings.TrimSpace(tab.Agent.Session)
		}
		if sessionName != "" {
			existing[sessionName] = struct{}{}
		}
	}

	var cmds []tea.Cmd
	for _, tab := range tabs {
		if tab.Assistant == "" {
			continue
		}
		if _, ok := m.config.Assistants[tab.Assistant]; !ok {
			continue
		}
		sessionName := strings.TrimSpace(tab.SessionName)
		if sessionName != "" {
			if _, ok := existing[sessionName]; ok {
				continue
			}
			existing[sessionName] = struct{}{}
		}
		status := strings.ToLower(strings.TrimSpace(tab.Status))
		if status == "stopped" {
			continue
		}
		if status == "detached" {
			m.addDetachedTab(ws, tab)
			continue
		}
		tabID, sn := m.addPlaceholderTab(ws, tab)
		cmds = append(cmds, m.reattachToSession(ws, tabID, tab.Assistant, sn))
	}
	return common.SafeBatch(cmds...)
}
