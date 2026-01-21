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
	"github.com/andyrewlee/amux/internal/ui/branchfiles"
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

// createAgentTab creates a new agent tab
func (m *Model) createAgentTab(assistant string, wt *data.Worktree) tea.Cmd {
	return func() tea.Msg {
		logging.Info("Creating agent tab: assistant=%s worktree=%s", assistant, wt.Name)

		// Calculate terminal dimensions using the same metrics as render/layout.
		tm := m.terminalMetrics()
		termWidth := tm.Width
		termHeight := tm.Height

		agent, err := m.agentManager.CreateAgent(wt, appPty.AgentType(assistant), uint16(termHeight), uint16(termWidth))
		if err != nil {
			logging.Error("Failed to create agent: %v", err)
			return messages.Error{Err: err, Context: "creating agent"}
		}

		logging.Info("Agent created, Terminal=%v", agent.Terminal != nil)

		// Create virtual terminal emulator with scrollback
		term := vterm.New(termWidth, termHeight)

		// Set up response writer for terminal queries (DSR, DA, etc.)
		if agent.Terminal != nil {
			term.SetResponseWriter(func(data []byte) {
				_ = agent.Terminal.SendString(string(data))
			})
		}

		// Create tab with unique ID
		wtID := string(wt.ID())
		displayName := nextAssistantName(assistant, m.tabsByWorktree[wtID])
		tab := &Tab{
			ID:        generateTabID(),
			Name:      displayName,
			Assistant: assistant,
			Worktree:  wt,
			Agent:     agent,
			Terminal:  term,
			Running:   true, // Agent starts running
		}

		// Set PTY size to match
		if agent.Terminal != nil {
			m.resizePTY(tab, termHeight, termWidth)
			logging.Info("Terminal size set to %dx%d", termWidth, termHeight)
		}

		// Add tab to the worktree's tab list
		m.tabsByWorktree[wtID] = append(m.tabsByWorktree[wtID], tab)
		m.activeTabByWorktree[wtID] = len(m.tabsByWorktree[wtID]) - 1

		return messages.TabCreated{Index: m.activeTabByWorktree[wtID], Name: displayName}
	}
}

// createDiffTab creates a new native diff viewer tab (no PTY)
func (m *Model) createDiffTab(change *git.Change, mode git.DiffMode, wt *data.Worktree) tea.Cmd {
	if wt == nil {
		return func() tea.Msg {
			return messages.Error{Err: fmt.Errorf("no worktree selected"), Context: "creating diff viewer"}
		}
	}

	logging.Info("Creating diff tab: path=%s mode=%d worktree=%s", change.Path, mode, wt.Name)

	// Calculate dimensions
	tm := m.terminalMetrics()
	viewerWidth := tm.Width
	viewerHeight := tm.Height

	// Create diff viewer model
	dv := diff.New(wt, change, mode, viewerWidth, viewerHeight)
	dv.SetFocused(true)

	// Create tab with unique ID
	wtID := string(wt.ID())
	displayName := fmt.Sprintf("Diff: %s", change.Path)
	if len(displayName) > 20 {
		displayName = "..." + displayName[len(displayName)-17:]
	}

	tab := &Tab{
		ID:         generateTabID(),
		Name:       displayName,
		Assistant:  "diff",
		Worktree:   wt,
		DiffViewer: dv,
	}

	// Add tab to the worktree's tab list
	m.tabsByWorktree[wtID] = append(m.tabsByWorktree[wtID], tab)
	m.activeTabByWorktree[wtID] = len(m.tabsByWorktree[wtID]) - 1

	// Return the Init command to start loading the diff
	return tea.Batch(
		dv.Init(),
		func() tea.Msg { return messages.TabCreated{Index: m.activeTabByWorktree[wtID], Name: displayName} },
	)
}

// createViewerTabLegacy creates a PTY-based viewer tab (for backwards compatibility)
// This is kept for cases where PTY-based viewing is still needed
//
//nolint:unused
func (m *Model) createViewerTabLegacy(file string, statusCode string, wt *data.Worktree) tea.Cmd {
	if wt == nil {
		return func() tea.Msg {
			return messages.Error{Err: fmt.Errorf("no worktree selected"), Context: "creating viewer"}
		}
	}
	return func() tea.Msg {
		logging.Info("Creating viewer tab: file=%s statusCode=%s worktree=%s", file, statusCode, wt.Name)

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

		// Calculate terminal dimensions using the same metrics as render/layout.
		tm := m.terminalMetrics()
		termWidth := tm.Width
		termHeight := tm.Height

		agent, err := m.agentManager.CreateViewer(wt, cmd, uint16(termHeight), uint16(termWidth))
		if err != nil {
			logging.Error("Failed to create viewer: %v", err)
			return messages.Error{Err: err, Context: "creating viewer"}
		}

		logging.Info("Viewer created, Terminal=%v", agent.Terminal != nil)

		// Create virtual terminal emulator with scrollback
		term := vterm.New(termWidth, termHeight)

		// Set up response writer for terminal queries (DSR, DA, etc.)
		if agent.Terminal != nil {
			term.SetResponseWriter(func(data []byte) {
				_ = agent.Terminal.SendString(string(data))
			})
		}

		// Create tab with unique ID
		wtID := string(wt.ID())
		displayName := fmt.Sprintf("Diff: %s", file)
		if len(displayName) > 20 {
			displayName = "..." + displayName[len(displayName)-17:]
		}

		tab := &Tab{
			ID:        generateTabID(),
			Name:      displayName,
			Assistant: "viewer", // Use a generic type for styling
			Worktree:  wt,
			Agent:     agent,
			Terminal:  term,
			Running:   true,
		}

		// Set PTY size to match
		if agent.Terminal != nil {
			m.resizePTY(tab, termHeight, termWidth)
		}

		// Add tab to the worktree's tab list
		m.tabsByWorktree[wtID] = append(m.tabsByWorktree[wtID], tab)
		m.activeTabByWorktree[wtID] = len(m.tabsByWorktree[wtID]) - 1

		return messages.TabCreated{Index: m.activeTabByWorktree[wtID], Name: displayName}
	}
}

// createBranchFilesTab creates a tab with the branch files view (replaces commit viewer)
func (m *Model) createBranchFilesTab(wt *data.Worktree) tea.Cmd {
	if wt == nil {
		return func() tea.Msg {
			return messages.Error{Err: fmt.Errorf("no worktree selected"), Context: "creating branch files view"}
		}
	}

	logging.Info("Creating branch files tab: worktree=%s", wt.Name)

	// Calculate dimensions
	tm := m.terminalMetrics()
	viewerWidth := tm.Width
	viewerHeight := tm.Height

	// Create branch files model
	bf := branchfiles.New(wt, viewerWidth, viewerHeight)
	bf.SetFocused(true)

	// Create tab with unique ID
	wtID := string(wt.ID())
	displayName := "Files Changed"

	tab := &Tab{
		ID:          generateTabID(),
		Name:        displayName,
		Assistant:   "branchfiles",
		Worktree:    wt,
		BranchFiles: bf,
	}

	// Add tab to the worktree's tab list
	m.tabsByWorktree[wtID] = append(m.tabsByWorktree[wtID], tab)
	m.activeTabByWorktree[wtID] = len(m.tabsByWorktree[wtID]) - 1

	// Return the Init command to start loading files
	return tea.Batch(
		bf.Init(),
		func() tea.Msg { return messages.TabCreated{Index: m.activeTabByWorktree[wtID], Name: displayName} },
	)
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
	tab.BranchFiles = nil
	tab.mu.Unlock()

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

// EnterCopyMode enters copy/scroll mode for the active tab
func (m *Model) EnterCopyMode() {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return
	}
	tab := tabs[activeIdx]
	tab.CopyMode = true
	tab.mu.Lock()
	if tab.Terminal != nil {
		tab.CopyState = common.InitCopyState(tab.Terminal)
	}
	tab.mu.Unlock()
}

// ExitCopyMode exits copy/scroll mode for the active tab
func (m *Model) ExitCopyMode() {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return
	}
	tab := tabs[activeIdx]
	tab.CopyMode = false
	tab.mu.Lock()
	if tab.Terminal != nil {
		tab.Terminal.ClearSelection()
		tab.Terminal.ScrollViewToBottom()
	}
	tab.CopyState = common.CopyState{}
	tab.mu.Unlock()
}

// CopyModeActive returns whether the active tab is in copy mode
func (m *Model) CopyModeActive() bool {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return false
	}
	return tabs[activeIdx].CopyMode
}

// SendToTerminal sends a string directly to the active terminal
func (m *Model) SendToTerminal(s string) {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return
	}
	tab := tabs[activeIdx]
	if tab.Agent != nil && tab.Agent.Terminal != nil {
		_ = tab.Agent.Terminal.SendString(s)
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

// HasBranchFiles returns true if the active tab has a branch files viewer.
func (m *Model) HasBranchFiles() bool {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return false
	}
	tab := tabs[activeIdx]
	tab.mu.Lock()
	defer tab.mu.Unlock()
	return tab.BranchFiles != nil
}

// HasDiffViewer returns true if the active tab has a diff viewer.
func (m *Model) HasDiffViewer() bool {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return false
	}
	tab := tabs[activeIdx]
	tab.mu.Lock()
	defer tab.mu.Unlock()
	return tab.DiffViewer != nil
}

// CloseAllTabs is deprecated - tabs now persist per-worktree
// This is kept for compatibility but does nothing
func (m *Model) CloseAllTabs() {
	// No-op: tabs now persist per-worktree and are not closed when switching
}
