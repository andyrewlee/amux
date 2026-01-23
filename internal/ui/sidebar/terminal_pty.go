package sidebar

import (
	"io"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/vterm"
)

const (
	ptyFlushQuiet       = 12 * time.Millisecond
	ptyFlushMaxInterval = 50 * time.Millisecond
	ptyFlushQuietAlt    = 30 * time.Millisecond
	ptyFlushMaxAlt      = 120 * time.Millisecond
	ptyFlushChunkSize   = 32 * 1024
	ptyReadBufferSize   = 32 * 1024
	ptyReadQueueSize    = 32
	ptyFrameInterval    = time.Second / 60
	ptyMaxPendingBytes  = 256 * 1024
)

// SidebarTerminalCreated is a message for terminal creation
type SidebarTerminalCreated struct {
	WorktreeID string
	TabID      TerminalTabID
	Terminal   *pty.Terminal
}

// createTerminalTab creates a new terminal tab for the worktree
func (m *TerminalModel) createTerminalTab(wt *data.Worktree) tea.Cmd {
	wtID := string(wt.ID())
	tabID := generateTerminalTabID()

	return func() tea.Msg {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/bash"
		}

		termWidth, termHeight := m.terminalContentSize()

		env := []string{"COLORTERM=truecolor"}
		term, err := pty.NewWithSize(shell, wt.Root, env, uint16(termHeight), uint16(termWidth))
		if err != nil {
			return messages.Error{Err: err, Context: "creating sidebar terminal tab"}
		}

		return SidebarTerminalCreated{
			WorktreeID: wtID,
			TabID:      tabID,
			Terminal:   term,
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
func (m *TerminalModel) HandleTerminalCreated(wtID string, tabID TerminalTabID, term *pty.Terminal) tea.Cmd {
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

	tabs := m.tabsByWorktree[wtID]
	tab := &TerminalTab{
		ID:    tabID,
		Name:  nextTerminalName(tabs),
		State: ts,
	}
	m.tabsByWorktree[wtID] = append(tabs, tab)

	// Set as active tab (switch to new tab)
	m.activeTabByWorktree[wtID] = len(m.tabsByWorktree[wtID]) - 1

	m.refreshTerminalSize()

	// Start reading from PTY
	return m.startPTYReader(wtID, tabID)
}

func (m *TerminalModel) startPTYReader(wtID string, tabID TerminalTabID) tea.Cmd {
	tab := m.getTabByID(wtID, tabID)
	if tab == nil || tab.State == nil {
		return nil
	}
	ts := tab.State
	if ts.readerActive || ts.Terminal == nil || !ts.Running {
		return nil
	}

	if ts.readerCancel != nil {
		safeClose(ts.readerCancel)
	}
	ts.readerCancel = make(chan struct{})
	ts.ptyMsgCh = make(chan tea.Msg, ptyReadQueueSize)
	ts.readerActive = true

	term := ts.Terminal
	cancel := ts.readerCancel
	msgCh := ts.ptyMsgCh

	go runPTYReader(term, msgCh, cancel, wtID, string(tabID))
	go m.forwardPTYMsgs(msgCh)
	return nil
}

// CloseTerminal closes all terminal tabs for the given worktree
func (m *TerminalModel) CloseTerminal(wtID string) {
	tabs := m.tabsByWorktree[wtID]
	for _, tab := range tabs {
		if tab.State != nil {
			m.stopPTYReader(tab.State)
			tab.State.mu.Lock()
			if tab.State.Terminal != nil {
				tab.State.Terminal.Close()
			}
			tab.State.mu.Unlock()
		}
	}
	delete(m.tabsByWorktree, wtID)
	delete(m.activeTabByWorktree, wtID)
}

// CloseAll closes all terminals
func (m *TerminalModel) CloseAll() {
	for wtID := range m.tabsByWorktree {
		m.CloseTerminal(wtID)
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
}

func runPTYReader(term *pty.Terminal, msgCh chan tea.Msg, cancel <-chan struct{}, wtID string, tabID string) {
	if term == nil {
		close(msgCh)
		return
	}

	dataCh := make(chan []byte, ptyReadQueueSize)
	errCh := make(chan error, 1)

	go func() {
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
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			select {
			case dataCh <- chunk:
			case <-cancel:
				return
			}
		}
	}()

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
			stoppedErr = err
		case data, ok := <-dataCh:
			if !ok {
				if len(pending) > 0 {
					if !sendPTYMsg(msgCh, cancel, messages.SidebarPTYOutput{WorktreeID: wtID, TabID: tabID, Data: pending}) {
						close(msgCh)
						return
					}
				}
				if stoppedErr == nil {
					stoppedErr = io.EOF
				}
				sendPTYMsg(msgCh, cancel, messages.SidebarPTYStopped{WorktreeID: wtID, TabID: tabID, Err: stoppedErr})
				close(msgCh)
				return
			}
			pending = append(pending, data...)
			if len(pending) >= ptyMaxPendingBytes {
				if !sendPTYMsg(msgCh, cancel, messages.SidebarPTYOutput{WorktreeID: wtID, TabID: tabID, Data: pending}) {
					close(msgCh)
					return
				}
				pending = nil
			}
		case <-ticker.C:
			if len(pending) > 0 {
				if !sendPTYMsg(msgCh, cancel, messages.SidebarPTYOutput{WorktreeID: wtID, TabID: tabID, Data: pending}) {
					close(msgCh)
					return
				}
				pending = nil
			}
			if stoppedErr != nil {
				sendPTYMsg(msgCh, cancel, messages.SidebarPTYStopped{WorktreeID: wtID, TabID: tabID, Err: stoppedErr})
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
					nextOut.WorktreeID == merged.WorktreeID &&
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
