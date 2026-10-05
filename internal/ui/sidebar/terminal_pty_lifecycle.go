package sidebar

import (
	"io"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
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

func (m *TerminalModel) createTerminalStateForTab(wsID string, tabID TerminalTabID, term *pty.Terminal, sessionName string) *TerminalState {
	return m.createTerminalStateForTabWithSizeAndRefresh(wsID, tabID, term, sessionName, 0, 0, true, true)
}

var setTerminalSizeFn = func(term *pty.Terminal, rows, cols uint16) error {
	return term.SetSize(rows, cols)
}

func (m *TerminalModel) createTerminalStateForTabWithSizeAndRefresh(
	wsID string,
	tabID TerminalTabID,
	term *pty.Terminal,
	sessionName string,
	termWidth int,
	termHeight int,
	refresh bool,
	resizeTerminal bool,
) *TerminalState {
	if termWidth <= 0 || termHeight <= 0 {
		termWidth, termHeight = m.terminalContentSize()
	}

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
	// Query replies join the input FIFO rather than writing to term directly:
	// this callback fires inside VTerm.Write under ts.mu, so a stalled PTY
	// would wedge every handler on the mutex (see the input queue contract).
	// It consults ts.input.writer dynamically under that same hold, so the
	// response writer never needs re-installing on terminal replacement.
	vt.SetResponseWriter(func(data []byte) {
		ts.admitQueryReplyLocked(data)
	})
	ts.VTerm = vt
	if term != nil && resizeTerminal {
		if ptyRows, ptyCols, ok := pty.WinsizeFromInts(termHeight, termWidth); ok {
			if err := setTerminalSizeFn(term, ptyRows, ptyCols); err != nil {
				logging.Debug("Initial terminal resize failed: %v", err)
			}
		}
	}

	// ts already initialized above, just need tabs lookup
	tabs := m.tabs.ByWorkspace[wsID]
	tab := &TerminalTab{
		ID:    tabID,
		Name:  nextTerminalName(tabs),
		State: ts,
	}
	m.tabs.ByWorkspace[wsID] = append(tabs, tab)

	// Clear pending creation flag now that tab exists
	delete(m.pendingCreation, wsID)

	// Set as active tab (switch to new tab)
	m.tabs.ActiveByWorkspace[wsID] = len(m.tabs.ByWorkspace[wsID]) - 1

	if refresh {
		m.refreshTerminalSize()
	}

	return ts
}

// HandleTerminalCreated handles the terminal tab creation message
func (m *TerminalModel) HandleTerminalCreated(wsID string, tabID TerminalTabID, term *pty.Terminal, sessionName string) tea.Cmd {
	if m.createTerminalStateForTab(wsID, tabID, term, sessionName) == nil {
		return nil
	}
	return m.startPTYReader(wsID, tabID)
}

func (m *TerminalModel) startPTYReader(wsID string, tabID TerminalTabID) tea.Cmd {
	tab := m.getTabByID(wsID, tabID)
	if tab == nil || tab.State == nil {
		return nil
	}
	ts := tab.State
	ts.State.StartReader(&ts.mu, ptyio.StartReaderOptionsFor(
		ptyio.ReaderNamespace{
			LabelPrefix:     "sidebar",
			ReadQueueSize:   ptyReadQueueSize,
			MaxPendingBytes: ptyMaxPendingBytes,
		},
		func() io.Reader {
			if ts.Terminal == nil || !ts.Running {
				return nil
			}
			return ts.Terminal
		},
		ptyio.PTYMsgFactory{
			Output: func(gen uint64, data []byte) tea.Msg {
				return messages.SidebarPTYOutput{WorkspaceID: wsID, TabID: string(tabID), Gen: gen, Data: data}
			},
			Stopped: func(gen uint64, err error) tea.Msg {
				return messages.SidebarPTYStopped{WorkspaceID: wsID, TabID: string(tabID), Gen: gen, Err: err}
			},
		},
		m.forwardPTYMsgs,
	))
	return nil
}

// StartPTYReaders ensures PTY readers are running for all tabs.
func (m *TerminalModel) StartPTYReaders() tea.Cmd {
	for wsID, tabs := range m.tabs.ByWorkspace {
		for _, tab := range tabs {
			if tab == nil {
				continue
			}
			if ts := tab.State; ts != nil && ts.State.ReaderStalled(&ts.mu, ptyReaderStallTimeout) {
				logging.Warn("Sidebar PTY reader stalled for workspace %s tab %s; restarting", wsID, tab.ID)
				m.stopPTYReader(ts)
			}
			_ = m.startPTYReader(wsID, tab.ID)
		}
	}
	return nil
}

// CloseTerminal closes all terminal tabs for the given workspace
func (m *TerminalModel) CloseTerminal(wsID string) {
	tabs := m.tabs.ByWorkspace[wsID]
	for _, tab := range tabs {
		m.teardownTabState(tab.State, "workspace close")
	}
	m.tabs.DeleteWorkspace(wsID)
	delete(m.pendingCreation, wsID)
	delete(m.lastActiveAt, wsID)
}

// CloseAll closes all terminals
func (m *TerminalModel) CloseAll() {
	for wsID := range m.tabs.ByWorkspace {
		m.CloseTerminal(wsID)
	}
}

func (m *TerminalModel) stopPTYReader(ts *TerminalState) {
	if ts == nil {
		return
	}
	ts.State.StopReader(&ts.mu)
}

// teardownTabState stops a tab's PTY reader and closes its terminal — the
// shared teardown behind tab close, workspace close, and workspace deletion.
// It returns the tab's session name so callers can kill the tmux session.
func (m *TerminalModel) teardownTabState(ts *TerminalState, reason string) (sessionName string) {
	if ts == nil {
		return ""
	}
	m.stopPTYReader(ts)
	ts.mu.Lock()
	sessionName = ts.SessionName
	if ts.Terminal != nil {
		closeTerminalForSidebar(ts.Terminal, reason)
	}
	// An attach in flight for this tab must not apply after teardown: bump
	// the epoch so its late result is rejected and its client closed.
	ts.invalidateReattachLocked()
	// The tab is going away: the writer stream's queued requests are
	// obsolete — stop discards them and fences the epoch.
	retiredWriter := ts.stopSidebarWriterLocked()
	// Same for the input binding: queued keypresses belong to the dead
	// terminal — retire fences them and any late failure report.
	retiredInput := ts.retireSidebarInputWriterLocked()
	ts.Running = false
	ts.RestartBackoff = 0
	ts.pendingBufferedBytes = 0
	ts.mu.Unlock()
	joinSidebarWriter(retiredWriter)
	// The terminal was closed under ts.mu above, so a send blocked inside
	// it has already unblocked — safe to join here.
	joinSidebarInputWriter(retiredInput)
	return sessionName
}

func closeTerminalForSidebar(term *pty.Terminal, reason string) {
	if term == nil {
		return
	}
	if err := term.Close(); err != nil {
		logging.Warn("Sidebar terminal close failed during %s: %v", reason, err)
	}
}

func (m *TerminalModel) detachState(ts *TerminalState, userInitiated bool) {
	if ts == nil {
		return
	}
	m.stopPTYReader(ts)
	ts.mu.Lock()
	// Invalidate any in-flight attach so its late outcome cannot resurrect a
	// detached tab; a user detach is a decision the attach must not reverse.
	ts.invalidateReattachLocked()
	// The detached VTerm keeps its history, so requests already accepted by
	// the stream finish in order here — the same bounded drain the fallback
	// performs — before the stream buffers reset.
	clip := drainSidebarWriterQueueLocked(ts)
	retiredWriter := ts.stopSidebarWriterLocked()
	retiredInput := ts.retireSidebarInputWriterLocked()
	term := ts.Terminal
	ts.Terminal = nil
	ts.Running = false
	ts.Detached = true
	ts.UserDetached = userInitiated
	ts.PendingOutput = nil
	ts.pendingBufferedBytes = 0
	ts.NoiseTrailing = nil
	ts.mu.Unlock()
	joinSidebarWriter(retiredWriter)
	if term != nil {
		closeTerminalForSidebar(term, "detach")
	}
	// A send blocked inside the just-closed terminal unblocks only now —
	// join the retired writer after the close, never under ts.mu.
	joinSidebarInputWriter(retiredInput)
	if clip != nil {
		m.drainSidebarClipboard(clip)
	}
}

// SendToTerminal queues a string for the current terminal's input writer —
// same admission contract as paste/key, so a stalled PTY cannot park the
// caller on SendString. A write failure detaches via SidebarInputFailed.
func (m *TerminalModel) SendToTerminal(s string) {
	ts := m.getTerminal()
	if ts == nil || ts.Terminal == nil {
		return
	}
	tab := m.getActiveTab()
	var tabID TerminalTabID
	if tab != nil {
		tabID = tab.ID
	}
	if res := m.admitSidebarInput(ts, tabID, s, "direct send"); res == sidebarInputRejectedFull {
		m.surfaceSidebarInputRejection(ts, tabID, "input queue full")
	}
}
