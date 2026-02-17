package sidebar

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/vterm"
)

// SetWorkspace sets the active workspace and creates terminal tab if needed
func (m *TerminalModel) SetWorkspace(ws *data.Workspace) tea.Cmd {
	m.setWorkspace(ws)
	if ws == nil {
		m.refreshTerminalSize()
		return nil
	}

	wsID := m.workspaceID()
	if len(m.tabsByWorkspace[wsID]) > 0 {
		// Tabs already exist for this workspace
		m.refreshTerminalSize()
		return nil
	}
	if m.pendingCreation[wsID] {
		// Creation already in progress
		return nil
	}

	// Create first terminal tab
	m.pendingCreation[wsID] = true
	return m.createTerminalTab(ws)
}

// SetWorkspacePreview sets the active workspace without creating tabs.
func (m *TerminalModel) SetWorkspacePreview(ws *data.Workspace) {
	m.setWorkspace(ws)
}

// EnsureTerminalTab creates a terminal tab if none exists for the current workspace.
// Used for lazy initialization when the terminal pane is focused.
func (m *TerminalModel) EnsureTerminalTab() tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	if len(m.getTabs()) > 0 {
		return nil
	}
	wsID := m.workspaceID()
	if m.pendingCreation[wsID] {
		return nil
	}
	m.pendingCreation[wsID] = true
	return m.createTerminalTab(m.workspace)
}

// CreateNewTab creates a new terminal tab for the current workspace and returns a command
func (m *TerminalModel) CreateNewTab() tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	return m.createTerminalTab(m.workspace)
}

// CloseActiveTab closes the active terminal tab
func (m *TerminalModel) CloseActiveTab() tea.Cmd {
	tabs := m.getTabs()
	if len(tabs) == 0 {
		return nil
	}

	wsID := m.workspaceID()
	idx := m.getActiveTabIdx()
	if idx < 0 || idx >= len(tabs) {
		return nil
	}

	tab := tabs[idx]
	sessionName := ""
	opts := m.getTmuxOptions()

	// Close PTY and cleanup
	if tab.State != nil {
		m.stopPTYReader(tab.State)
		tab.State.mu.Lock()
		sessionName = tab.State.SessionName
		if tab.State.Terminal != nil {
			tab.State.Terminal.Close()
		}
		tab.State.Running = false
		tab.State.ptyRestartBackoff = 0
		tab.State.mu.Unlock()
	}

	// Remove tab from slice
	m.tabsByWorkspace[wsID] = append(tabs[:idx], tabs[idx+1:]...)

	// Adjust active index
	newLen := len(m.tabsByWorkspace[wsID])
	if newLen == 0 {
		m.activeTabByWorkspace[wsID] = 0
	} else if idx >= newLen {
		m.activeTabByWorkspace[wsID] = newLen - 1
	}

	m.refreshTerminalSize()
	if sessionName == "" {
		return nil
	}
	return closeSessionIfUnattached(sessionName, opts)
}

func closeSessionIfUnattached(sessionName string, opts tmux.Options) tea.Cmd {
	return func() tea.Msg {
		if sessionName == "" {
			return nil
		}
		deadline := time.Now().Add(1200 * time.Millisecond)
		for {
			hasClients, err := tmux.SessionHasClients(sessionName, opts)
			if err != nil {
				return nil
			}
			if !hasClients {
				_ = tmux.KillSession(sessionName, opts)
				return nil
			}
			if time.Now().After(deadline) {
				// Shared or still-attached session; keep it alive.
				return nil
			}
			time.Sleep(75 * time.Millisecond)
		}
	}
}

// AddTerminalForHarness creates a terminal state without a PTY for benchmarks/tests.
func (m *TerminalModel) AddTerminalForHarness(ws *data.Workspace) {
	if ws == nil {
		return
	}
	m.setWorkspace(ws)
	wsID := m.workspaceID()
	if len(m.tabsByWorkspace[wsID]) > 0 {
		return
	}
	termWidth, termHeight := m.TerminalSize()
	vt := vterm.New(termWidth, termHeight)
	vt.AllowAltScreenScrollback = true
	tab := &TerminalTab{
		ID:   generateTerminalTabID(),
		Name: "Terminal 1",
		State: &TerminalState{
			VTerm:      vt,
			Running:    true,
			lastWidth:  termWidth,
			lastHeight: termHeight,
		},
	}
	m.tabsByWorkspace[wsID] = []*TerminalTab{tab}
	m.activeTabByWorkspace[wsID] = 0
}

// WriteToTerminal writes bytes to the active terminal while holding the lock.
func (m *TerminalModel) WriteToTerminal(data []byte) {
	ts := m.getTerminal()
	if ts == nil {
		return
	}
	ts.mu.Lock()
	vt := ts.VTerm
	if vt != nil {
		vt.Write(data)
	}
	ts.mu.Unlock()
}
