package center

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/vterm"
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
	if m.config == nil || m.config.Assistants == nil {
		return func() tea.Msg {
			return messages.Toast{
				Message: "Tab cannot be detached",
				Level:   messages.ToastInfo,
			}
		}
	}
	if _, ok := m.config.Assistants[tab.Assistant]; !ok {
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
	return func() tea.Msg {
		return messages.TabDetached{Index: index}
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

// ReattachActiveTab reattaches to a detached tmux session.
func (m *Model) ReattachActiveTab() tea.Cmd {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return nil
	}
	tab := tabs[activeIdx]
	if tab == nil || tab.Workspace == nil {
		return nil
	}
	if m.config == nil || m.config.Assistants == nil {
		return func() tea.Msg {
			return messages.Toast{
				Message: "Tab cannot be reattached",
				Level:   messages.ToastInfo,
			}
		}
	}
	if _, ok := m.config.Assistants[tab.Assistant]; !ok {
		return func() tea.Msg {
			return messages.Toast{
				Message: "Only assistant tabs can be reattached",
				Level:   messages.ToastInfo,
			}
		}
	}
	tab.mu.Lock()
	detached := tab.Detached
	sessionName := tab.SessionName
	tab.mu.Unlock()
	if !detached {
		return nil
	}
	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height
	if sessionName == "" {
		sessionName = tmux.SessionName("amux", string(tab.Workspace.ID()), string(tab.ID))
	}
	assistant := tab.Assistant
	ws := tab.Workspace
	tabID := tab.ID
	opts := m.getTmuxOptions()
	return func() tea.Msg {
		state, err := tmux.SessionStateFor(sessionName, opts)
		if err != nil {
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Err:         err,
				Action:      "reattach",
			}
		}
		if !state.Exists || !state.HasLivePane {
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Err:         fmt.Errorf("tmux session ended"),
				Stopped:     true,
				Action:      "reattach",
			}
		}
		tags := tmux.SessionTags{
			WorkspaceID: string(ws.ID()),
			TabID:       string(tabID),
			Type:        "agent",
			Assistant:   assistant,
		}
		agent, err := m.agentManager.CreateAgentWithTags(ws, appPty.AgentType(assistant), sessionName, uint16(termHeight), uint16(termWidth), tags)
		if err != nil {
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Err:         err,
				Action:      "reattach",
			}
		}
		// Best-effort capture of existing scrollback from the tmux pane.
		scrollback, _ := tmux.CapturePane(sessionName, opts)
		return ptyTabReattachResult{
			WorkspaceID:       string(ws.ID()),
			TabID:             tabID,
			Agent:             agent,
			Rows:              termHeight,
			Cols:              termWidth,
			ScrollbackCapture: scrollback,
		}
	}
}

// RestartActiveTab restarts a stopped or detached agent tab by creating a fresh tmux client.
func (m *Model) RestartActiveTab() tea.Cmd {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return nil
	}
	tab := tabs[activeIdx]
	if tab == nil || tab.Workspace == nil {
		return nil
	}
	if m.config == nil || m.config.Assistants == nil {
		return nil
	}
	if _, ok := m.config.Assistants[tab.Assistant]; !ok {
		return nil
	}
	tab.mu.Lock()
	running := tab.Running
	sessionName := tab.SessionName
	if sessionName == "" && tab.Agent != nil {
		sessionName = tab.Agent.Session
	}
	tab.mu.Unlock()
	if running {
		return func() tea.Msg {
			return messages.Toast{
				Message: "Tab is still running",
				Level:   messages.ToastInfo,
			}
		}
	}
	ws := tab.Workspace
	tabID := tab.ID
	if sessionName == "" {
		sessionName = tmux.SessionName("amux", string(ws.ID()), string(tabID))
	}
	m.stopPTYReader(tab)
	var existingAgent *appPty.Agent
	tab.mu.Lock()
	existingAgent = tab.Agent
	tab.Agent = nil
	tab.mu.Unlock()
	if existingAgent != nil {
		_ = m.agentManager.CloseAgent(existingAgent)
	}
	tmuxOpts := m.getTmuxOptions()

	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height
	assistant := tab.Assistant

	return func() tea.Msg {
		// KillSession is synchronous: it calls cmd.Run() which blocks until the
		// tmux server processes the kill and returns. By the time it completes,
		// the session is fully removed from tmux's perspective.
		// The subsequent CreateAgentWithTags uses `new-session -Ads` which is
		// atomic (attach-if-exists, create-if-not), providing an additional
		// safety net in the unlikely event of cleanup lag.
		_ = tmux.KillSession(sessionName, tmuxOpts)

		tags := tmux.SessionTags{
			WorkspaceID: string(ws.ID()),
			TabID:       string(tabID),
			Type:        "agent",
			Assistant:   assistant,
			CreatedAt:   time.Now().Unix(),
		}
		agent, err := m.agentManager.CreateAgentWithTags(ws, appPty.AgentType(assistant), sessionName, uint16(termHeight), uint16(termWidth), tags)
		if err != nil {
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Err:         err,
				Stopped:     true,
				Action:      "restart",
			}
		}
		// Best-effort capture of scrollback (empty for fresh sessions, which is fine).
		scrollback, _ := tmux.CapturePane(sessionName, tmuxOpts)
		return ptyTabReattachResult{
			WorkspaceID:       string(ws.ID()),
			TabID:             tabID,
			Agent:             agent,
			Rows:              termHeight,
			Cols:              termWidth,
			ScrollbackCapture: scrollback,
		}
	}
}

func (m *Model) tabSelectionChangedCmd() tea.Cmd {
	wsID := m.workspaceID()
	if wsID == "" {
		return nil
	}
	return func() tea.Msg {
		return messages.TabSelectionChanged{
			WorkspaceID: wsID,
			ActiveIndex: m.getActiveTabIdx(),
		}
	}
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
		cmds = append(cmds, m.createAgentTabWithSession(tab.Assistant, ws, tab.SessionName, tab.Name, false))
	}
	if restoreCount > 0 {
		desired := lastBeforeActive
		if desired < 0 {
			desired = 0
		}
		if desired >= restoreCount {
			desired = restoreCount - 1
		}
		m.activeTabByWorkspace[wsID] = desired
	}
	return safeBatch(cmds...)
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
		cmds = append(cmds, m.createAgentTabWithSession(tab.Assistant, ws, sessionName, tab.Name, false))
	}
	return safeBatch(cmds...)
}

func (m *Model) addDetachedTab(ws *data.Workspace, info data.TabInfo) {
	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height
	if termWidth < 1 {
		termWidth = 80
	}
	if termHeight < 1 {
		termHeight = 24
	}
	displayName := strings.TrimSpace(info.Name)
	if displayName == "" {
		displayName = strings.TrimSpace(info.Assistant)
	}
	if displayName == "" {
		displayName = "Terminal"
	}
	term := vterm.New(termWidth, termHeight)
	term.AllowAltScreenScrollback = true
	tab := &Tab{
		ID:          generateTabID(),
		Name:        displayName,
		Assistant:   info.Assistant,
		Workspace:   ws,
		SessionName: info.SessionName,
		Detached:    true,
		Running:     false,
		Terminal:    term,
	}
	wsID := string(ws.ID())
	m.tabsByWorkspace[wsID] = append(m.tabsByWorkspace[wsID], tab)
}

// safeBatch wraps commands in a batch, handling nil commands gracefully.
func safeBatch(cmds ...tea.Cmd) tea.Cmd {
	var valid []tea.Cmd
	for _, cmd := range cmds {
		if cmd != nil {
			valid = append(valid, cmd)
		}
	}
	if len(valid) == 0 {
		return nil
	}
	if len(valid) == 1 {
		return valid[0]
	}
	return tea.Batch(valid...)
}
