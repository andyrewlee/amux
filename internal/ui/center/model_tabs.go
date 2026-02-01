package center

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/vterm"
)

func nextAssistantName(assistant string, tabs []*Tab) string {
	assistant = strings.TrimSpace(assistant)
	if assistant == "" {
		return ""
	}

	used := make(map[string]struct{})
	for _, tab := range tabs {
		if tab == nil || tab.Assistant != assistant {
			continue
		}
		name := strings.TrimSpace(tab.Name)
		if name == "" {
			name = assistant
		}
		used[name] = struct{}{}
	}

	if _, ok := used[assistant]; !ok {
		return assistant
	}

	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s %d", assistant, i)
		if _, ok := used[candidate]; !ok {
			return candidate
		}
	}
}

type ptyTabCreateResult struct {
	Workspace   *data.Workspace
	Assistant   string
	DisplayName string
	Agent       *appPty.Agent
	TabID       TabID
	Activate    bool
	Rows        int
	Cols        int
}

type ptyTabReattachResult struct {
	WorkspaceID string
	TabID       TabID
	Agent       *appPty.Agent
	Rows        int
	Cols        int
}

type ptyTabReattachFailed struct {
	WorkspaceID string
	TabID       TabID
	Err         error
	Stopped     bool
	Action      string
}

func truncateDisplayName(name string) string {
	if len(name) > 20 {
		return "..." + name[len(name)-17:]
	}
	return name
}

// createAgentTab creates a new agent tab
func (m *Model) createAgentTab(assistant string, ws *data.Workspace) tea.Cmd {
	return m.createAgentTabWithSession(assistant, ws, "", "", true)
}

func (m *Model) createAgentTabWithSession(assistant string, ws *data.Workspace, sessionName string, displayName string, activate bool) tea.Cmd {
	if ws == nil {
		return func() tea.Msg {
			return messages.Error{Err: fmt.Errorf("no workspace selected"), Context: "creating agent"}
		}
	}

	// Calculate terminal dimensions using the same metrics as render/layout.
	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height
	tabID := generateTabID()
	if sessionName == "" {
		sessionName = tmux.SessionName("amux", string(ws.ID()), string(tabID))
	}

	return func() tea.Msg {
		logging.Info("Creating agent tab: assistant=%s workspace=%s", assistant, ws.Name)

		tags := tmux.SessionTags{
			WorkspaceID: string(ws.ID()),
			TabID:       string(tabID),
			Type:        "agent",
			Assistant:   assistant,
			CreatedAt:   time.Now().Unix(),
		}
		agent, err := m.agentManager.CreateAgentWithTags(ws, appPty.AgentType(assistant), sessionName, uint16(termHeight), uint16(termWidth), tags)
		if err != nil {
			logging.Error("Failed to create agent: %v", err)
			return messages.Error{Err: err, Context: "creating agent"}
		}

		logging.Info("Agent created, Terminal=%v", agent.Terminal != nil)

		return ptyTabCreateResult{
			Workspace:   ws,
			Assistant:   assistant,
			Agent:       agent,
			TabID:       tabID,
			DisplayName: displayName,
			Activate:    activate,
			Rows:        termHeight,
			Cols:        termWidth,
		}
	}
}

func (m *Model) handlePtyTabCreated(msg ptyTabCreateResult) tea.Cmd {
	if msg.Workspace == nil || msg.Agent == nil {
		return func() tea.Msg {
			return messages.Error{Err: fmt.Errorf("missing workspace or agent"), Context: "creating terminal tab"}
		}
	}

	rows := msg.Rows
	cols := msg.Cols
	if rows <= 0 || cols <= 0 {
		tm := m.terminalMetrics()
		rows = tm.Height
		cols = tm.Width
	}

	displayName := strings.TrimSpace(msg.DisplayName)
	if displayName == "" {
		wsID := string(msg.Workspace.ID())
		displayName = nextAssistantName(msg.Assistant, m.tabsByWorkspace[wsID])
	}
	if displayName == "" {
		displayName = "Terminal"
	}

	// Create virtual terminal emulator with scrollback
	term := vterm.New(cols, rows)
	term.AllowAltScreenScrollback = true

	// Create tab with unique ID (pre-generated if provided)
	tabID := msg.TabID
	if tabID == "" {
		tabID = generateTabID()
	}
	tab := &Tab{
		ID:           tabID,
		Name:         displayName,
		Assistant:    msg.Assistant,
		Workspace:    msg.Workspace,
		Agent:        msg.Agent,
		SessionName:  msg.Agent.Session,
		Terminal:     term,
		Running:      true, // Agent/viewer starts running
		monitorDirty: true,
	}

	// Set up response writer for terminal queries (DSR, DA, etc.)
	if msg.Agent.Terminal != nil {
		agentTerm := msg.Agent.Terminal
		workspaceID := string(msg.Workspace.ID())
		term.SetResponseWriter(func(data []byte) {
			if len(data) == 0 || agentTerm == nil {
				return
			}
			if m.isTabActorReady() {
				response := append([]byte(nil), data...)
				if !m.sendTabEvent(tabEvent{
					tab:         tab,
					workspaceID: workspaceID,
					tabID:       tabID,
					kind:        tabEventSendResponse,
					response:    response,
				}) {
					if err := agentTerm.SendString(string(response)); err != nil {
						logging.Warn("Response write failed for tab %s: %v", tabID, err)
						if m.msgSink != nil {
							m.msgSink(TabInputFailed{TabID: tabID, WorkspaceID: workspaceID, Err: err})
						}
					}
				}
				return
			}
			if err := agentTerm.SendString(string(data)); err != nil {
				logging.Warn("Response write failed for tab %s: %v", tabID, err)
				if m.msgSink != nil {
					m.msgSink(TabInputFailed{TabID: tabID, WorkspaceID: workspaceID, Err: err})
				}
			}
		})
	}

	// Set PTY size to match
	if msg.Agent.Terminal != nil {
		m.resizePTY(tab, rows, cols)
	}

	// Add tab to the workspace's tab list
	wsID := string(msg.Workspace.ID())
	m.tabsByWorkspace[wsID] = append(m.tabsByWorkspace[wsID], tab)
	createdIdx := len(m.tabsByWorkspace[wsID]) - 1
	if msg.Activate {
		m.activeTabByWorkspace[wsID] = createdIdx
	}
	m.noteTabsChanged()

	return func() tea.Msg {
		return messages.TabCreated{Index: createdIdx, Name: displayName}
	}
}

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

// Public wrappers for prefix mode commands

// NextTab switches to the next tab (public wrapper)
func (m *Model) NextTab() {
	m.nextTab()
}

// PrevTab switches to the previous tab (public wrapper)
func (m *Model) PrevTab() {
	m.prevTab()
}

// CloseActiveTab closes the current tab (public wrapper)
func (m *Model) CloseActiveTab() tea.Cmd {
	return m.closeCurrentTab()
}

// SelectTab switches to a specific tab by index (0-indexed)
func (m *Model) SelectTab(index int) {
	tabs := m.getTabs()
	if index >= 0 && index < len(tabs) {
		m.setActiveTabIdx(index)
	}
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
		})
	}
	return result, m.activeTabByWorkspace[wsID]
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
