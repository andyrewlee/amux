package center

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/diff"
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
	Rows        int
	Cols        int
}

func truncateDisplayName(name string) string {
	if len(name) > 20 {
		return "..." + name[len(name)-17:]
	}
	return name
}

// createAgentTab creates a new agent tab
func (m *Model) createAgentTab(assistant string, ws *data.Workspace) tea.Cmd {
	if ws == nil {
		return func() tea.Msg {
			return messages.Error{Err: fmt.Errorf("no workspace selected"), Context: "creating agent"}
		}
	}

	// Calculate terminal dimensions using the same metrics as render/layout.
	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height

	return func() tea.Msg {
		logging.Info("Creating agent tab: assistant=%s workspace=%s", assistant, ws.Name)

		agent, err := m.agentManager.CreateAgent(ws, appPty.AgentType(assistant), uint16(termHeight), uint16(termWidth))
		if err != nil {
			logging.Error("Failed to create agent: %v", err)
			return messages.Error{Err: err, Context: "creating agent"}
		}

		logging.Info("Agent created, Terminal=%v", agent.Terminal != nil)

		return ptyTabCreateResult{
			Workspace: ws,
			Assistant: assistant,
			Agent:     agent,
			Rows:      termHeight,
			Cols:      termWidth,
		}
	}
}

// createVimTab creates a new tab that opens a file in vim
func (m *Model) createVimTab(filePath string, ws *data.Workspace) tea.Cmd {
	if ws == nil {
		return func() tea.Msg {
			return messages.Error{Err: fmt.Errorf("no workspace selected"), Context: "creating vim viewer"}
		}
	}

	// Calculate terminal dimensions using the same metrics as render/layout.
	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height

	return func() tea.Msg {
		logging.Info("Creating vim tab: file=%s workspace=%s", filePath, ws.Name)

		// Escape filename for shell
		escapedFile := "'" + strings.ReplaceAll(filePath, "'", "'\\''") + "'"
		cmd := fmt.Sprintf("vim -- %s", escapedFile)

		agent, err := m.agentManager.CreateViewer(ws, cmd, uint16(termHeight), uint16(termWidth))
		if err != nil {
			logging.Error("Failed to create vim viewer: %v", err)
			return messages.Error{Err: err, Context: "creating vim viewer"}
		}

		logging.Info("Vim viewer created, Terminal=%v", agent.Terminal != nil)

		// Use filename for display (truncate if needed)
		fileName := filePath
		if idx := strings.LastIndex(filePath, "/"); idx >= 0 {
			fileName = fileName[idx+1:]
		}
		displayName := truncateDisplayName(fileName)

		return ptyTabCreateResult{
			Workspace:   ws,
			Assistant:   "vim",
			DisplayName: displayName,
			Agent:       agent,
			Rows:        termHeight,
			Cols:        termWidth,
		}
	}
}

// createDiffTab creates a new native diff viewer tab (no PTY)
func (m *Model) createDiffTab(change *git.Change, mode git.DiffMode, ws *data.Workspace) tea.Cmd {
	if ws == nil {
		return func() tea.Msg {
			return messages.Error{Err: fmt.Errorf("no workspace selected"), Context: "creating diff viewer"}
		}
	}

	logging.Info("Creating diff tab: path=%s mode=%d workspace=%s", change.Path, mode, ws.Name)

	// Calculate dimensions
	tm := m.terminalMetrics()
	viewerWidth := tm.Width
	viewerHeight := tm.Height

	// Create diff viewer model
	dv := diff.New(ws, change, mode, viewerWidth, viewerHeight)
	dv.SetFocused(true)

	// Create tab with unique ID
	wsID := string(ws.ID())
	displayName := fmt.Sprintf("Diff: %s", change.Path)
	if len(displayName) > 20 {
		displayName = "..." + displayName[len(displayName)-17:]
	}

	tab := &Tab{
		ID:         generateTabID(),
		Name:       displayName,
		Assistant:  "diff",
		Workspace:  ws,
		DiffViewer: dv,
	}

	// Add tab to the workspace's tab list
	m.tabsByWorkspace[wsID] = append(m.tabsByWorkspace[wsID], tab)
	m.activeTabByWorkspace[wsID] = len(m.tabsByWorkspace[wsID]) - 1
	m.noteTabsChanged()

	// Return the Init command to start loading the diff
	return common.SafeBatch(
		dv.Init(),
		func() tea.Msg { return messages.TabCreated{Index: m.activeTabByWorkspace[wsID], Name: displayName} },
	)
}

// createViewerTabLegacy creates a PTY-based viewer tab (for backwards compatibility)
// This is kept for cases where PTY-based viewing is still needed
//
//nolint:unused
func (m *Model) createViewerTabLegacy(file string, statusCode string, ws *data.Workspace) tea.Cmd {
	if ws == nil {
		return func() tea.Msg {
			return messages.Error{Err: fmt.Errorf("no workspace selected"), Context: "creating viewer"}
		}
	}

	// Calculate terminal dimensions using the same metrics as render/layout.
	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height

	return func() tea.Msg {
		logging.Info("Creating viewer tab: file=%s statusCode=%s workspace=%s", file, statusCode, ws.Name)

		// Escape filename for shell
		escapedFile := "'" + strings.ReplaceAll(file, "'", "'\\''") + "'"

		var cmd string
		if statusCode == "??" {
			// Untracked file: show full content prefixed by + to indicate additions.
			cmd = fmt.Sprintf("awk '{print \"\\033[32m+ \" $0 \"\\033[0m\"}' %s | less -R", escapedFile)
		} else if len(statusCode) >= 1 && statusCode[0] != ' ' {
			// Staged change: show index diff (covers new files with status A).
			cmd = fmt.Sprintf("git diff --cached --color=always -- %s | less -R", escapedFile)
		} else {
			// Unstaged change: show working tree diff.
			cmd = fmt.Sprintf("git diff --color=always -- %s | less -R", escapedFile)
		}

		agent, err := m.agentManager.CreateViewer(ws, cmd, uint16(termHeight), uint16(termWidth))
		if err != nil {
			logging.Error("Failed to create viewer: %v", err)
			return messages.Error{Err: err, Context: "creating viewer"}
		}

		logging.Info("Viewer created, Terminal=%v", agent.Terminal != nil)

		displayName := truncateDisplayName(fmt.Sprintf("Diff: %s", file))

		return ptyTabCreateResult{
			Workspace:   ws,
			Assistant:   "viewer", // Use a generic type for styling
			DisplayName: displayName,
			Agent:       agent,
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

	// Create tab with unique ID
	tabID := generateTabID()
	tab := &Tab{
		ID:           tabID,
		Name:         displayName,
		Assistant:    msg.Assistant,
		Workspace:    msg.Workspace,
		Agent:        msg.Agent,
		Terminal:     term,
		Running:      true, // Agent/viewer starts running
		monitorDirty: true,
	}

	// Set up response writer for terminal queries (DSR, DA, etc.)
	if msg.Agent.Terminal != nil {
		term.SetResponseWriter(func(data []byte) {
			if len(data) == 0 {
				return
			}
			if m.isTabActorReady() {
				response := append([]byte(nil), data...)
				if !m.sendTabEvent(tabEvent{
					tab:         tab,
					workspaceID: string(msg.Workspace.ID()),
					tabID:       tabID,
					kind:        tabEventSendResponse,
					response:    response,
				}) {
					if err := msg.Agent.Terminal.SendString(string(response)); err != nil {
						logging.Warn("Response write failed for tab %s: %v", tabID, err)
						tab.mu.Lock()
						tab.Running = false
						tab.mu.Unlock()
					}
				}
				return
			}
			if err := msg.Agent.Terminal.SendString(string(data)); err != nil {
				logging.Warn("Response write failed for tab %s: %v", tabID, err)
				tab.mu.Lock()
				tab.Running = false
				tab.mu.Unlock()
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
	m.activeTabByWorkspace[wsID] = len(m.tabsByWorkspace[wsID]) - 1
	m.noteTabsChanged()

	return func() tea.Msg {
		return messages.TabCreated{Index: m.activeTabByWorkspace[wsID], Name: displayName}
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
	// Clean up viewers
	tab.DiffViewer = nil
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

	return func() tea.Msg {
		return messages.TabClosed{Index: index}
	}
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
	if tab.Agent != nil && tab.Agent.Terminal != nil {
		if err := tab.Agent.Terminal.SendString(s); err != nil {
			logging.Warn("SendToTerminal failed for tab %s: %v", tab.ID, err)
			tab.mu.Lock()
			tab.Running = false
			tab.mu.Unlock()
		}
	}
}

// GetTabsInfo returns information about current tabs for persistence
func (m *Model) GetTabsInfo() ([]data.TabInfo, int) {
	var result []data.TabInfo
	tabs := m.getTabs()
	for _, tab := range tabs {
		result = append(result, data.TabInfo{
			Assistant: tab.Assistant,
			Name:      tab.Name,
		})
	}
	return result, m.getActiveTabIdx()
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
