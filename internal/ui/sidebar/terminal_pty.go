package sidebar

import (
	"io"
	"os"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/safego"
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
}

// SidebarTerminalCreateFailed is a message for terminal creation failure
type SidebarTerminalCreateFailed struct {
	WorkspaceID string
	Err         error
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

		termWidth, termHeight := m.terminalContentSize()

		env := []string{"COLORTERM=truecolor"}
		term, err := pty.NewWithSize(shell, ws.Root, env, uint16(termHeight), uint16(termWidth))
		if err != nil {
			return SidebarTerminalCreateFailed{WorkspaceID: wsID, Err: err}
		}

		return SidebarTerminalCreated{
			WorkspaceID: wsID,
			TabID:       tabID,
			Terminal:    term,
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
func (m *TerminalModel) HandleTerminalCreated(wsID string, tabID TerminalTabID, term *pty.Terminal) tea.Cmd {
	termWidth, termHeight := m.terminalContentSize()

	vt := vterm.New(termWidth, termHeight)
	vt.SetResponseWriter(func(data []byte) {
		_, _ = term.Write(data)
	})
	_ = term.SetSize(uint16(termHeight), uint16(termWidth))

	ts := &TerminalState{
		Terminal:   term,
		VTerm:      vt,
		Running:    true,
		lastWidth:  termWidth,
		lastHeight: termHeight,
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

func runPTYReader(term *pty.Terminal, msgCh chan tea.Msg, cancel <-chan struct{}, wsID string, tabID string, heartbeat *int64) {
	// Ensure msgCh is always closed even if we panic, so forwardPTYMsgs doesn't block forever.
	// The inner recover() catches double-close panics from existing close(msgCh) calls.
	defer func() {
		defer func() { _ = recover() }()
		close(msgCh)
	}()

	if term == nil {
		return
	}
	beat := func() {
		if heartbeat != nil {
			atomic.StoreInt64(heartbeat, time.Now().UnixNano())
		}
	}
	beat()

	dataCh := make(chan []byte, ptyReadQueueSize)
	errCh := make(chan error, 1)

	safego.Go("sidebar.pty_read_loop", func() {
		buf := make([]byte, ptyReadBufferSize)
		for {
			n, err := term.Read(buf)
			if err != nil {
				select {
				case errCh <- err:
				default:
				}
				close(dataCh)
				return
			}
			if n == 0 {
				continue
			}
			beat()
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			select {
			case dataCh <- chunk:
			case <-cancel:
				return
			}
		}
	})

	ticker := time.NewTicker(ptyFrameInterval)
	defer ticker.Stop()

	var pending []byte
	var stoppedErr error

	for {
		select {
		case <-cancel:
			close(msgCh)
			return
		case err := <-errCh:
			beat()
			stoppedErr = err
		case data, ok := <-dataCh:
			beat()
			if !ok {
				if len(pending) > 0 {
					if !sendPTYMsg(msgCh, cancel, messages.SidebarPTYOutput{WorkspaceID: wsID, TabID: tabID, Data: pending}) {
						close(msgCh)
						return
					}
				}
				if stoppedErr == nil {
					stoppedErr = io.EOF
				}
				sendPTYMsg(msgCh, cancel, messages.SidebarPTYStopped{WorkspaceID: wsID, TabID: tabID, Err: stoppedErr})
				close(msgCh)
				return
			}
			pending = append(pending, data...)
			if len(pending) >= ptyMaxPendingBytes {
				if !sendPTYMsg(msgCh, cancel, messages.SidebarPTYOutput{WorkspaceID: wsID, TabID: tabID, Data: pending}) {
					close(msgCh)
					return
				}
				pending = nil
			}
		case <-ticker.C:
			beat()
			if len(pending) > 0 {
				if !sendPTYMsg(msgCh, cancel, messages.SidebarPTYOutput{WorkspaceID: wsID, TabID: tabID, Data: pending}) {
					close(msgCh)
					return
				}
				pending = nil
			}
			if stoppedErr != nil {
				sendPTYMsg(msgCh, cancel, messages.SidebarPTYStopped{WorkspaceID: wsID, TabID: tabID, Err: stoppedErr})
				close(msgCh)
				return
			}
		}
	}
}

func sendPTYMsg(msgCh chan tea.Msg, cancel <-chan struct{}, msg tea.Msg) bool {
	if msgCh == nil {
		return false
	}
	select {
	case <-cancel:
		return false
	case msgCh <- msg:
		return true
	}
}

func (m *TerminalModel) forwardPTYMsgs(msgCh <-chan tea.Msg) {
	for msg := range msgCh {
		if msg == nil {
			continue
		}
		out, ok := msg.(messages.SidebarPTYOutput)
		if !ok {
			if m.msgSink != nil {
				m.msgSink(msg)
			}
			continue
		}

		merged := out
		for {
			select {
			case next, ok := <-msgCh:
				if !ok {
					if m.msgSink != nil && len(merged.Data) > 0 {
						m.msgSink(merged)
					}
					return
				}
				if next == nil {
					continue
				}
				if nextOut, ok := next.(messages.SidebarPTYOutput); ok &&
					nextOut.WorkspaceID == merged.WorkspaceID &&
					nextOut.TabID == merged.TabID {
					merged.Data = append(merged.Data, nextOut.Data...)
					if len(merged.Data) >= ptyMaxPendingBytes {
						if m.msgSink != nil && len(merged.Data) > 0 {
							m.msgSink(merged)
						}
						merged.Data = nil
					}
					continue
				}
				if m.msgSink != nil && len(merged.Data) > 0 {
					m.msgSink(merged)
				}
				if m.msgSink != nil {
					m.msgSink(next)
				}
				goto nextMsg
			default:
				if m.msgSink != nil && len(merged.Data) > 0 {
					m.msgSink(merged)
				}
				goto nextMsg
			}
		}
	nextMsg:
	}
}

// SendToTerminal sends a string directly to the current terminal
func (m *TerminalModel) SendToTerminal(s string) {
	ts := m.getTerminal()
	if ts != nil && ts.Terminal != nil {
		_ = ts.Terminal.SendString(s)
	}
}
