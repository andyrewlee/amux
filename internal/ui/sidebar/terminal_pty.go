package sidebar

import (
	"fmt"
	"os"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/safego"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/vterm"
)

const (
	ptyFlushQuiet         = 12 * time.Millisecond
	ptyFlushMaxInterval   = 50 * time.Millisecond
	ptyFlushQuietAlt      = 30 * time.Millisecond
	ptyFlushMaxAlt        = 120 * time.Millisecond
	ptyFlushChunkSize     = 32 * 1024
	ptyReadBufferSize     = 32 * 1024
	ptyReadQueueSize      = 32
	ptyFrameInterval      = time.Second / 60
	ptyMaxPendingBytes    = 256 * 1024
	ptyReaderStallTimeout = 10 * time.Second
	ptyMaxBufferedBytes   = 4 * 1024 * 1024
	ptyRestartMax         = 5
	ptyRestartWindow      = time.Minute
)

// SidebarTerminalCreated is a message for terminal creation
type SidebarTerminalCreated struct {
	WorkspaceID string
	TabID       TerminalTabID
	Terminal    *pty.Terminal
	SessionName string
}

// SidebarTerminalCreateFailed is a message for terminal creation failure
type SidebarTerminalCreateFailed struct {
	WorkspaceID string
	Err         error
}

type sidebarTerminalReattachResult struct {
	WorkspaceID string
	TabID       TerminalTabID
	Terminal    *pty.Terminal
	SessionName string
}

type sidebarTerminalReattachFailed struct {
	WorkspaceID string
	TabID       TerminalTabID
	Err         error
	Stopped     bool
	Action      string
}

// createTerminalTab creates a new terminal tab for the workspace
func (m *TerminalModel) createTerminalTab(ws *data.Workspace) tea.Cmd {
	wsID := string(ws.ID())
	tabID := generateTerminalTabID()

	return func() tea.Msg {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/bash"
		}
		if err := tmux.EnsureAvailable(); err != nil {
			return SidebarTerminalCreateFailed{WorkspaceID: wsID, Err: err}
		}

		termWidth, termHeight := m.terminalContentSize()

		env := []string{"COLORTERM=truecolor"}
		sessionName := tmux.SessionName("amux", wsID, string(tabID))
		command := tmux.ClientCommand(sessionName, ws.Root, fmt.Sprintf("exec %s -l", shell))
		term, err := pty.NewWithSize(command, ws.Root, env, uint16(termHeight), uint16(termWidth))
		if err != nil {
			return SidebarTerminalCreateFailed{WorkspaceID: wsID, Err: err}
		}

		return SidebarTerminalCreated{
			WorkspaceID: wsID,
			TabID:       tabID,
			Terminal:    term,
			SessionName: sessionName,
		}
	}
}

// DetachActiveTab closes the PTY client but keeps the tmux session alive.
func (m *TerminalModel) DetachActiveTab() tea.Cmd {
	tab := m.getActiveTab()
	if tab == nil || tab.State == nil {
		return nil
	}
	m.detachState(tab.State)
	return nil
}

// ReattachActiveTab reattaches to a detached tmux session for the active terminal tab.
func (m *TerminalModel) ReattachActiveTab() tea.Cmd {
	tab := m.getActiveTab()
	if tab == nil || tab.State == nil || m.workspace == nil {
		return nil
	}
	ts := tab.State
	ts.mu.Lock()
	running := ts.Running
	sessionName := ts.SessionName
	ts.mu.Unlock()
	if running {
		return func() tea.Msg {
			return messages.Toast{Message: "Terminal is still running", Level: messages.ToastInfo}
		}
	}
	ws := m.workspace
	if sessionName == "" {
		sessionName = tmux.SessionName("amux", string(ws.ID()), string(tab.ID))
	}
	return m.attachToSession(ws, tab.ID, sessionName, "reattach")
}

// RestartActiveTab starts a fresh tmux session for the active terminal tab.
func (m *TerminalModel) RestartActiveTab() tea.Cmd {
	tab := m.getActiveTab()
	if tab == nil || tab.State == nil || m.workspace == nil {
		return nil
	}
	ts := tab.State
	ts.mu.Lock()
	running := ts.Running
	sessionName := ts.SessionName
	ts.mu.Unlock()
	if running {
		return func() tea.Msg {
			return messages.Toast{Message: "Terminal is still running", Level: messages.ToastInfo}
		}
	}
	ws := m.workspace
	if sessionName == "" {
		sessionName = tmux.SessionName("amux", string(ws.ID()), string(tab.ID))
	}
	m.detachState(ts)
	_ = tmux.KillSession(sessionName, m.getTmuxOptions())
	return m.attachToSession(ws, tab.ID, sessionName, "restart")
}

func (m *TerminalModel) attachToSession(ws *data.Workspace, tabID TerminalTabID, sessionName, action string) tea.Cmd {
	if ws == nil {
		return nil
	}
	return func() tea.Msg {
		if err := tmux.EnsureAvailable(); err != nil {
			return sidebarTerminalReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Err:         err,
				Action:      action,
			}
		}
		opts := m.getTmuxOptions()
		if action == "reattach" {
			state, err := tmux.SessionStateFor(sessionName, opts)
			if err != nil {
				return sidebarTerminalReattachFailed{
					WorkspaceID: string(ws.ID()),
					TabID:       tabID,
					Err:         err,
					Action:      action,
				}
			}
			if !state.Exists || !state.HasLivePane {
				return sidebarTerminalReattachFailed{
					WorkspaceID: string(ws.ID()),
					TabID:       tabID,
					Err:         fmt.Errorf("tmux session ended"),
					Stopped:     true,
					Action:      action,
				}
			}
		}

		termWidth, termHeight := m.terminalContentSize()
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/bash"
		}
		env := []string{"COLORTERM=truecolor"}
		command := tmux.ClientCommand(sessionName, ws.Root, fmt.Sprintf("exec %s -l", shell))
		term, err := pty.NewWithSize(command, ws.Root, env, uint16(termHeight), uint16(termWidth))
		if err != nil {
			return sidebarTerminalReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Err:         err,
				Action:      action,
			}
		}
		return sidebarTerminalReattachResult{
			WorkspaceID: string(ws.ID()),
			TabID:       tabID,
			Terminal:    term,
			SessionName: sessionName,
		}
	}
}

// terminalContentSize returns the terminal content dimensions (excluding tab bar)
func (m *TerminalModel) terminalContentSize() (int, int) {
	termWidth, termHeight, _ := m.terminalViewportSize()
	if termWidth < 10 {
		termWidth = 10
	}
	if termHeight < 3 {
		termHeight = 3
	}
	return termWidth, termHeight
}

// HandleTerminalCreated handles the terminal tab creation message
func (m *TerminalModel) HandleTerminalCreated(wsID string, tabID TerminalTabID, term *pty.Terminal, sessionName string) tea.Cmd {
	termWidth, termHeight := m.terminalContentSize()

	vt := vterm.New(termWidth, termHeight)
	vt.SetResponseWriter(func(data []byte) {
		_, _ = term.Write(data)
	})
	_ = term.SetSize(uint16(termHeight), uint16(termWidth))

	ts := &TerminalState{
		Terminal:    term,
		VTerm:       vt,
		Running:     true,
		Detached:    false,
		SessionName: sessionName,
		lastWidth:   termWidth,
		lastHeight:  termHeight,
	}

	tabs := m.tabsByWorkspace[wsID]
	tab := &TerminalTab{
		ID:    tabID,
		Name:  nextTerminalName(tabs),
		State: ts,
	}
	m.tabsByWorkspace[wsID] = append(tabs, tab)

	// Clear pending creation flag now that tab exists
	delete(m.pendingCreation, wsID)

	// Set as active tab (switch to new tab)
	m.activeTabByWorkspace[wsID] = len(m.tabsByWorkspace[wsID]) - 1

	m.refreshTerminalSize()

	// Start reading from PTY
	return m.startPTYReader(wsID, tabID)
}

func (m *TerminalModel) startPTYReader(wsID string, tabID TerminalTabID) tea.Cmd {
	tab := m.getTabByID(wsID, tabID)
	if tab == nil || tab.State == nil {
		return nil
	}
	ts := tab.State
	ts.mu.Lock()
	if ts.readerActive {
		if ts.ptyMsgCh == nil || ts.readerCancel == nil {
			ts.readerActive = false
		} else {
			ts.mu.Unlock()
			return nil
		}
	}
	if ts.Terminal == nil || !ts.Running {
		ts.readerActive = false
		ts.mu.Unlock()
		return nil
	}

	if ts.readerCancel != nil {
		safeClose(ts.readerCancel)
	}
	ts.readerCancel = make(chan struct{})
	ts.ptyMsgCh = make(chan tea.Msg, ptyReadQueueSize)
	ts.readerActive = true
	ts.ptyRestartBackoff = 0
	atomic.StoreInt64(&ts.ptyHeartbeat, time.Now().UnixNano())

	term := ts.Terminal
	cancel := ts.readerCancel
	msgCh := ts.ptyMsgCh
	ts.mu.Unlock()

	safego.Go("sidebar.pty_reader", func() {
		defer m.markPTYReaderStopped(ts)
		runPTYReader(term, msgCh, cancel, wsID, string(tabID), &ts.ptyHeartbeat)
	})
	safego.Go("sidebar.pty_forward", func() {
		m.forwardPTYMsgs(msgCh)
	})
	return nil
}

// StartPTYReaders ensures PTY readers are running for all tabs.
func (m *TerminalModel) StartPTYReaders() tea.Cmd {
	for wsID, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			if tab == nil {
				continue
			}
			ts := tab.State
			if ts != nil {
				ts.mu.Lock()
				readerActive := ts.readerActive
				ts.mu.Unlock()
				if readerActive {
					lastBeat := atomic.LoadInt64(&ts.ptyHeartbeat)
					if lastBeat > 0 && time.Since(time.Unix(0, lastBeat)) > ptyReaderStallTimeout {
						logging.Warn("Sidebar PTY reader stalled for workspace %s tab %s; restarting", wsID, tab.ID)
						m.stopPTYReader(ts)
					}
				}
			}
			_ = m.startPTYReader(wsID, tab.ID)
		}
	}
	return nil
}

// CloseTerminal closes all terminal tabs for the given workspace
func (m *TerminalModel) CloseTerminal(wsID string) {
	tabs := m.tabsByWorkspace[wsID]
	for _, tab := range tabs {
		if tab.State != nil {
			m.stopPTYReader(tab.State)
			tab.State.mu.Lock()
			if tab.State.Terminal != nil {
				tab.State.Terminal.Close()
			}
			tab.State.Running = false
			tab.State.ptyRestartBackoff = 0
			tab.State.mu.Unlock()
		}
	}
	delete(m.tabsByWorkspace, wsID)
	delete(m.activeTabByWorkspace, wsID)
	delete(m.pendingCreation, wsID)
}

// CloseAll closes all terminals
func (m *TerminalModel) CloseAll() {
	for wsID := range m.tabsByWorkspace {
		m.CloseTerminal(wsID)
	}
}

func safeClose(ch chan struct{}) {
	defer func() {
		_ = recover()
	}()
	close(ch)
}

func (m *TerminalModel) stopPTYReader(ts *TerminalState) {
	if ts == nil {
		return
	}
	ts.mu.Lock()
	if ts.readerCancel != nil {
		safeClose(ts.readerCancel)
		ts.readerCancel = nil
	}
	ts.readerActive = false
	ts.ptyMsgCh = nil
	ts.mu.Unlock()
	atomic.StoreInt64(&ts.ptyHeartbeat, 0)
}

func (m *TerminalModel) detachState(ts *TerminalState) {
	if ts == nil {
		return
	}
	m.stopPTYReader(ts)
	ts.mu.Lock()
	term := ts.Terminal
	ts.Terminal = nil
	ts.Running = false
	ts.Detached = true
	ts.pendingOutput = nil
	ts.mu.Unlock()
	if term != nil {
		term.Close()
	}
}

func (m *TerminalModel) markPTYReaderStopped(ts *TerminalState) {
	if ts == nil {
		return
	}
	ts.mu.Lock()
	ts.readerActive = false
	ts.ptyMsgCh = nil
	ts.mu.Unlock()
	atomic.StoreInt64(&ts.ptyHeartbeat, 0)
}

// SendToTerminal sends a string directly to the current terminal
func (m *TerminalModel) SendToTerminal(s string) {
	ts := m.getTerminal()
	if ts != nil && ts.Terminal != nil {
		if err := ts.Terminal.SendString(s); err != nil {
			logging.Warn("Sidebar SendToTerminal failed: %v", err)
			ts.mu.Lock()
			ts.Running = false
			ts.mu.Unlock()
		}
	}
}
