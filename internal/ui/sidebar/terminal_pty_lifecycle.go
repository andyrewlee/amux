package sidebar

import (
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/safego"
	"github.com/andyrewlee/amux/internal/vterm"
)

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

	ts := &TerminalState{
		Terminal:    term,
		VTerm:       nil, // set below
		Running:     true,
		Detached:    false,
		SessionName: sessionName,
		lastWidth:   termWidth,
		lastHeight:  termHeight,
	}

	vt := vterm.New(termWidth, termHeight)
	vt.AllowAltScreenScrollback = true
	// Capture term directly — the response writer is replaced on reattach,
	// so the captured reference stays valid. Acquiring ts.mu here would
	// deadlock because VTerm.Write() (called under ts.mu) triggers this
	// callback synchronously.
	vt.SetResponseWriter(func(data []byte) {
		if term != nil {
			_, _ = term.Write(data)
		}
	})
	ts.VTerm = vt
	_ = term.SetSize(uint16(termHeight), uint16(termWidth))

	// ts already initialized above, just need tabs lookup
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
			ts.Detached = true
			ts.mu.Unlock()
		}
	}
}
