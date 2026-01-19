package sidebar

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/compositor"
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

// SelectionState tracks mouse selection state
type SelectionState struct {
	Active bool
	StartX int
	StartY int
	EndX   int
	EndY   int
}

// TerminalState holds the terminal state for a worktree
type TerminalState struct {
	Terminal  *pty.Terminal
	VTerm     *vterm.VTerm
	Running   bool
	CopyMode  bool // Whether in copy/scroll mode (keys not sent to PTY)
	CopyState common.CopyState
	mu        sync.Mutex

	// Track last size to avoid unnecessary resizes
	lastWidth  int
	lastHeight int

	// PTY output buffering
	pendingOutput     []byte
	flushScheduled    bool
	lastOutputAt      time.Time
	flushPendingSince time.Time

	// Selection state
	Selection SelectionState

	// Snapshot cache for VTermLayer - avoid recreating snapshot when terminal unchanged
	cachedSnap       *compositor.VTermSnapshot
	cachedVersion    uint64
	cachedShowCursor bool

	readerActive bool
	ptyMsgCh     chan tea.Msg
	readerCancel chan struct{}
}

// TerminalModel is the Bubbletea model for the sidebar terminal section
type TerminalModel struct {
	// State per worktree
	terminals map[string]*TerminalState

	// Current worktree
	worktree *data.Worktree

	// Layout
	width           int
	height          int
	focused         bool
	offsetX         int
	offsetY         int
	showKeymapHints bool

	// Styles
	styles common.Styles
}

// NewTerminalModel creates a new sidebar terminal model
func NewTerminalModel() *TerminalModel {
	return &TerminalModel{
		terminals: make(map[string]*TerminalState),
		styles:    common.DefaultStyles(),
	}
}

// SetShowKeymapHints controls whether helper text is rendered.
func (m *TerminalModel) SetShowKeymapHints(show bool) {
	m.showKeymapHints = show
}

// SetStyles updates the component's styles (for theme changes).
func (m *TerminalModel) SetStyles(styles common.Styles) {
	m.styles = styles
}

// AddTerminalForHarness creates a terminal state without a PTY for benchmarks/tests.
func (m *TerminalModel) AddTerminalForHarness(wt *data.Worktree) {
	if wt == nil {
		return
	}
	m.worktree = wt
	wtID := string(wt.ID())
	if m.terminals[wtID] != nil {
		return
	}
	termWidth, termHeight := m.TerminalSize()
	vt := vterm.New(termWidth, termHeight)
	m.terminals[wtID] = &TerminalState{
		VTerm:      vt,
		Running:    true,
		lastWidth:  termWidth,
		lastHeight: termHeight,
	}
}

// WriteToTerminal writes bytes to the active terminal while holding the lock.
func (m *TerminalModel) WriteToTerminal(data []byte) {
	ts := m.getTerminal()
	if ts == nil || ts.VTerm == nil {
		return
	}
	ts.mu.Lock()
	ts.VTerm.Write(data)
	ts.mu.Unlock()
}

// worktreeID returns the ID of the current worktree
func (m *TerminalModel) worktreeID() string {
	if m.worktree == nil {
		return ""
	}
	return string(m.worktree.ID())
}

// getTerminal returns the terminal state for the current worktree
func (m *TerminalModel) getTerminal() *TerminalState {
	return m.terminals[m.worktreeID()]
}

// flushTiming returns the appropriate flush timing
func (m *TerminalModel) flushTiming() (time.Duration, time.Duration) {
	ts := m.getTerminal()
	if ts == nil {
		return ptyFlushQuiet, ptyFlushMaxInterval
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Only use slower Alt timing for true AltScreen mode (full-screen TUIs).
	// SyncActive (DEC 2026) already handles partial updates via screen snapshots,
	// so we don't need slower flush timing - it just makes streaming text feel laggy.
	if ts.VTerm != nil && ts.VTerm.AltScreen {
		return ptyFlushQuietAlt, ptyFlushMaxAlt
	}
	return ptyFlushQuiet, ptyFlushMaxInterval
}

// Init initializes the terminal model
func (m *TerminalModel) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (m *TerminalModel) Update(msg tea.Msg) (*TerminalModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if !m.focused {
			return m, nil
		}

		ts := m.getTerminal()
		if ts == nil {
			return m, nil
		}

		if msg.Button == tea.MouseLeft {
			termX, termY, inBounds := m.screenToTerminal(msg.X, msg.Y)

			ts.mu.Lock()
			if ts.VTerm != nil {
				ts.VTerm.ClearSelection()
			}
			if inBounds {
				ts.Selection = SelectionState{
					Active: true,
					StartX: termX,
					StartY: termY,
					EndX:   termX,
					EndY:   termY,
				}
				if ts.VTerm != nil {
					ts.VTerm.SetSelection(termX, termY, termX, termY, true, false)
				}
			} else {
				ts.Selection = SelectionState{}
			}
			ts.mu.Unlock()
		}

	case tea.MouseMotionMsg:
		if !m.focused {
			return m, nil
		}

		if msg.Button != tea.MouseLeft {
			return m, nil
		}

		ts := m.getTerminal()
		if ts == nil {
			return m, nil
		}

		termX, termY, _ := m.screenToTerminal(msg.X, msg.Y)

		ts.mu.Lock()
		if ts.Selection.Active {
			// Dimensions
			termWidth := m.width
			termHeight := m.height
			if ts.VTerm != nil {
				termWidth = ts.VTerm.Width
				termHeight = ts.VTerm.Height
			}

			// Clamp
			if termX < 0 {
				termX = 0
			}
			if termY < 0 {
				termY = 0
			}
			if termX >= termWidth {
				termX = termWidth - 1
			}
			if termY >= termHeight {
				termY = termHeight - 1
			}

			ts.Selection.EndX = termX
			ts.Selection.EndY = termY
			if ts.VTerm != nil {
				ts.VTerm.SetSelection(
					ts.Selection.StartX, ts.Selection.StartY,
					termX, termY, true, false,
				)
			}
		}
		ts.mu.Unlock()

	case tea.MouseReleaseMsg:
		if !m.focused {
			return m, nil
		}

		if msg.Button != tea.MouseLeft {
			return m, nil
		}

		ts := m.getTerminal()
		if ts == nil {
			return m, nil
		}

		ts.mu.Lock()
		if ts.Selection.Active {
			if ts.VTerm != nil {
				text := ts.VTerm.GetSelectedText(
					ts.Selection.StartX, ts.Selection.StartY,
					ts.Selection.EndX, ts.Selection.EndY,
				)
				if text != "" {
					if err := common.CopyToClipboard(text); err != nil {
						logging.Error("Failed to copy sidebar selection: %v", err)
					} else {
						logging.Info("Copied %d chars from sidebar", len(text))
					}
				}
			}
			ts.Selection.Active = false
		}
		ts.mu.Unlock()

	case tea.PasteMsg:
		if !m.focused {
			return m, nil
		}

		ts := m.getTerminal()
		if ts == nil || ts.Terminal == nil {
			return m, nil
		}

		// Handle bracketed paste - send entire content at once with escape sequences
		text := msg.Content
		bracketedText := "\x1b[200~" + text + "\x1b[201~"
		_ = ts.Terminal.SendString(bracketedText)
		logging.Debug("Sidebar terminal pasted %d bytes via bracketed paste", len(text))
		return m, nil

	case tea.KeyPressMsg:
		if !m.focused {
			return m, nil
		}

		ts := m.getTerminal()
		if ts == nil || ts.Terminal == nil {
			return m, nil
		}

		// Copy mode: handle scroll navigation without sending to PTY
		if ts.CopyMode {
			return m, m.handleCopyModeKey(ts, msg)
		}

		// PgUp/PgDown for scrollback (these don't conflict with embedded TUIs)
		switch msg.Key().Code {
		case tea.KeyPgUp:
			ts.mu.Lock()
			if ts.VTerm != nil {
				ts.VTerm.ScrollView(ts.VTerm.Height / 2)
			}
			ts.mu.Unlock()
			return m, nil

		case tea.KeyPgDown:
			ts.mu.Lock()
			if ts.VTerm != nil {
				ts.VTerm.ScrollView(-ts.VTerm.Height / 2)
			}
			ts.mu.Unlock()
			return m, nil
		}

		// If scrolled, any typing goes back to live and sends key
		ts.mu.Lock()
		if ts.VTerm != nil && ts.VTerm.IsScrolled() {
			ts.VTerm.ScrollViewToBottom()
		}
		ts.mu.Unlock()

		// Forward ALL keys to terminal (no Ctrl interceptions)
		input := common.KeyToBytes(msg)
		if len(input) > 0 {
			_ = ts.Terminal.SendString(string(input))
		}

	case messages.SidebarPTYOutput:
		wtID := msg.WorktreeID
		ts := m.terminals[wtID]
		if ts != nil {
			ts.pendingOutput = append(ts.pendingOutput, msg.Data...)
			ts.lastOutputAt = time.Now()
			if !ts.flushScheduled {
				ts.flushScheduled = true
				ts.flushPendingSince = ts.lastOutputAt
				quiet, _ := m.flushTiming()
				cmds = append(cmds, tea.Tick(quiet, func(t time.Time) tea.Msg {
					return messages.SidebarPTYFlush{WorktreeID: wtID}
				}))
			}
			// Continue reading
			cmds = append(cmds, m.readPTY(wtID))
		}

	case messages.SidebarPTYFlush:
		ts := m.terminals[msg.WorktreeID]
		if ts != nil {
			now := time.Now()
			quietFor := now.Sub(ts.lastOutputAt)
			pendingFor := time.Duration(0)
			if !ts.flushPendingSince.IsZero() {
				pendingFor = now.Sub(ts.flushPendingSince)
			}
			quiet, maxInterval := m.flushTiming()
			if quietFor < quiet && pendingFor < maxInterval {
				delay := quiet - quietFor
				if delay < time.Millisecond {
					delay = time.Millisecond
				}
				ts.flushScheduled = true
				wtID := msg.WorktreeID
				cmds = append(cmds, tea.Tick(delay, func(t time.Time) tea.Msg {
					return messages.SidebarPTYFlush{WorktreeID: wtID}
				}))
				break
			}

			ts.flushScheduled = false
			ts.flushPendingSince = time.Time{}
			if len(ts.pendingOutput) > 0 {
				ts.mu.Lock()
				if ts.VTerm != nil {
					chunkSize := len(ts.pendingOutput)
					if chunkSize > ptyFlushChunkSize {
						chunkSize = ptyFlushChunkSize
					}
					ts.VTerm.Write(ts.pendingOutput[:chunkSize])
					copy(ts.pendingOutput, ts.pendingOutput[chunkSize:])
					ts.pendingOutput = ts.pendingOutput[:len(ts.pendingOutput)-chunkSize]
				}
				ts.mu.Unlock()
				if len(ts.pendingOutput) == 0 {
					ts.pendingOutput = ts.pendingOutput[:0]
				} else {
					ts.flushScheduled = true
					ts.flushPendingSince = time.Now()
					wtID := msg.WorktreeID
					cmds = append(cmds, tea.Tick(time.Millisecond, func(t time.Time) tea.Msg {
						return messages.SidebarPTYFlush{WorktreeID: wtID}
					}))
				}
			}
		}

	case messages.SidebarPTYTick:
		ts := m.terminals[msg.WorktreeID]
		if ts != nil && ts.Running {
			cmds = append(cmds, m.readPTY(msg.WorktreeID))
		}

	case messages.SidebarPTYStopped:
		ts := m.terminals[msg.WorktreeID]
		if ts != nil {
			ts.Running = false
			m.stopPTYReader(ts)
			logging.Info("Sidebar PTY stopped for worktree %s: %v", msg.WorktreeID, msg.Err)
		}

	case SidebarTerminalCreated:
		cmd := m.HandleTerminalCreated(msg.WorktreeID, msg.Terminal)
		cmds = append(cmds, cmd)

	case messages.WorktreeDeleted:
		if msg.Worktree != nil {
			wtID := string(msg.Worktree.ID())
			if ts := m.terminals[wtID]; ts != nil {
				m.stopPTYReader(ts)
				if ts.Terminal != nil {
					ts.Terminal.Close()
				}
				delete(m.terminals, wtID)
			}
		}
	}

	return m, tea.Batch(cmds...)
}

// View renders the terminal section
func (m *TerminalModel) View() string {
	var b strings.Builder

	ts := m.getTerminal()
	if ts == nil || ts.VTerm == nil {
		// Show placeholder
		placeholder := m.styles.Muted.Render("No terminal")
		b.WriteString(placeholder)
	} else {
		ts.mu.Lock()
		ts.VTerm.ShowCursor = m.focused && !ts.CopyMode
		// Use VTerm.Render() directly - it uses dirty line caching and delta styles
		content := ts.VTerm.Render()
		isScrolled := ts.VTerm.IsScrolled()
		var scrollInfo string
		if isScrolled {
			offset, total := ts.VTerm.GetScrollInfo()
			scrollInfo = formatScrollPos(offset, total)
		}
		ts.mu.Unlock()

		b.WriteString(content)

		if ts.CopyMode {
			b.WriteString("\n")
			modeStyle := lipgloss.NewStyle().
				Bold(true).
				Foreground(common.ColorBackground).
				Background(common.ColorWarning)
			b.WriteString(modeStyle.Render(" COPY MODE (q/Esc exit • j/k/↑/↓ line • PgUp/PgDn/Ctrl+u/d half • g/G top/bottom) "))
		} else if isScrolled {
			b.WriteString("\n")
			scrollStyle := lipgloss.NewStyle().
				Bold(true).
				Foreground(common.ColorBackground).
				Background(common.ColorInfo)
			b.WriteString(scrollStyle.Render(" SCROLL: " + scrollInfo + " "))
		}
	}

	// Help bar
	contentWidth := m.width
	if contentWidth < 1 {
		contentWidth = 1
	}
	helpLines := m.helpLines(contentWidth)
	if !m.showKeymapHints {
		helpLines = nil
	}

	// Pad to fill height
	contentHeight := strings.Count(b.String(), "\n") + 1
	targetHeight := m.height - len(helpLines) // Account for help
	if targetHeight < 0 {
		targetHeight = 0
	}
	if targetHeight > contentHeight {
		b.WriteString(strings.Repeat("\n", targetHeight-contentHeight))
	}
	b.WriteString(strings.Join(helpLines, "\n"))

	// Ensure output doesn't exceed m.height lines
	result := b.String()
	if m.height > 0 {
		lines := strings.Split(result, "\n")
		if len(lines) > m.height {
			lines = lines[:m.height]
			result = strings.Join(lines, "\n")
		}
	}
	return result
}

// TerminalLayer returns a VTermLayer for the active worktree terminal.
func (m *TerminalModel) TerminalLayer() *compositor.VTermLayer {
	ts := m.getTerminal()
	if ts == nil {
		return nil
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.VTerm == nil {
		return nil
	}

	version := ts.VTerm.Version()
	showCursor := m.focused && !ts.CopyMode
	if ts.cachedSnap != nil && ts.cachedVersion == version && ts.cachedShowCursor == showCursor {
		return compositor.NewVTermLayer(ts.cachedSnap)
	}

	snap := compositor.NewVTermSnapshotWithCache(ts.VTerm, showCursor, ts.cachedSnap)
	if snap == nil {
		return nil
	}

	ts.cachedSnap = snap
	ts.cachedVersion = version
	ts.cachedShowCursor = showCursor
	return compositor.NewVTermLayer(snap)
}

// StatusLine returns the status line for the active terminal.
func (m *TerminalModel) StatusLine() string {
	ts := m.getTerminal()
	if ts == nil || ts.VTerm == nil {
		return ""
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.CopyMode {
		modeStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(common.ColorBackground).
			Background(common.ColorWarning)
		return modeStyle.Render(" COPY MODE (q/Esc exit • j/k/↑/↓ line • PgUp/PgDn/Ctrl+u/d half • g/G top/bottom) ")
	}
	if ts.VTerm.IsScrolled() {
		offset, total := ts.VTerm.GetScrollInfo()
		scrollStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(common.ColorBackground).
			Background(common.ColorInfo)
		return scrollStyle.Render(" SCROLL: " + formatScrollPos(offset, total) + " ")
	}
	return ""
}

// HelpLines returns the help lines for the given width, respecting visibility.
func (m *TerminalModel) HelpLines(width int) []string {
	if !m.showKeymapHints {
		return nil
	}
	if width < 1 {
		width = 1
	}
	return m.helpLines(width)
}

// TerminalOrigin returns the absolute origin for terminal rendering.
func (m *TerminalModel) TerminalOrigin() (int, int) {
	return m.offsetX, m.offsetY
}

// TerminalSize returns the terminal render size.
func (m *TerminalModel) TerminalSize() (int, int) {
	width := m.width
	height := m.height - 1
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	return width, height
}

func (m *TerminalModel) helpItem(key, desc string) string {
	return common.RenderHelpItem(m.styles, key, desc)
}

func (m *TerminalModel) helpLines(contentWidth int) []string {
	items := []string{}

	ts := m.getTerminal()
	hasTerm := ts != nil && ts.VTerm != nil
	if hasTerm {
		items = append(items,
			m.helpItem("C-Spc [", "copy"),
			m.helpItem("PgUp", "half up"),
			m.helpItem("PgDn", "half down"),
		)
		if ts.CopyMode {
			items = append(items,
				m.helpItem("g", "top"),
				m.helpItem("G", "bottom"),
				m.helpItem("Space/v", "select"),
				m.helpItem("y/Enter", "copy"),
				m.helpItem("C-v", "rect"),
				m.helpItem("/?", "search"),
				m.helpItem("n/N", "next/prev"),
				m.helpItem("w/b/e", "word"),
				m.helpItem("H/M/L", "top/mid/bot"),
			)
		}
	}

	return common.WrapHelpItems(items, contentWidth)
}

// SetSize sets the terminal section size
func (m *TerminalModel) SetSize(width, height int) {
	m.width = width
	m.height = height

	// Calculate actual terminal dimensions
	termWidth := width
	termHeight := height - 1
	if termWidth < 10 {
		termWidth = 10
	}
	if termHeight < 3 {
		termHeight = 3
	}

	// Resize all terminal vtems only if size changed
	for _, ts := range m.terminals {
		ts.mu.Lock()
		if ts.VTerm != nil && (ts.lastWidth != termWidth || ts.lastHeight != termHeight) {
			ts.lastWidth = termWidth
			ts.lastHeight = termHeight
			ts.VTerm.Resize(termWidth, termHeight)
			if ts.Terminal != nil {
				_ = ts.Terminal.SetSize(uint16(termHeight), uint16(termWidth))
			}
		}
		ts.mu.Unlock()
	}
}

// Focus sets focus state
func (m *TerminalModel) Focus() {
	m.focused = true
}

// Blur removes focus
func (m *TerminalModel) Blur() {
	m.focused = false
}

// Focused returns whether the terminal is focused
func (m *TerminalModel) Focused() bool {
	return m.focused
}

// SetWorktree sets the active worktree and creates terminal if needed
func (m *TerminalModel) SetWorktree(wt *data.Worktree) tea.Cmd {
	m.worktree = wt
	if wt == nil {
		return nil
	}

	wtID := string(wt.ID())
	if m.terminals[wtID] != nil {
		// Terminal already exists for this worktree
		return nil
	}

	// Create terminal
	return m.createTerminal(wt)
}

// createTerminal creates a new terminal for the worktree
func (m *TerminalModel) createTerminal(wt *data.Worktree) tea.Cmd {
	return func() tea.Msg {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/bash"
		}

		termWidth := m.width
		termHeight := m.height - 1
		if termWidth < 10 {
			termWidth = 10
		}
		if termHeight < 3 {
			termHeight = 3
		}

		env := []string{"COLORTERM=truecolor"}
		term, err := pty.NewWithSize(shell, wt.Root, env, uint16(termHeight), uint16(termWidth))
		if err != nil {
			return messages.Error{Err: err, Context: "creating sidebar terminal"}
		}

		wtID := string(wt.ID())
		return SidebarTerminalCreated{
			WorktreeID: wtID,
			Terminal:   term,
		}
	}
}

// SidebarTerminalCreated is a message for terminal creation
type SidebarTerminalCreated struct {
	WorktreeID string
	Terminal   *pty.Terminal
}

// HandleTerminalCreated handles the terminal creation message
func (m *TerminalModel) HandleTerminalCreated(wtID string, term *pty.Terminal) tea.Cmd {
	termWidth := m.width
	termHeight := m.height - 1
	if termWidth < 10 {
		termWidth = 10
	}
	if termHeight < 3 {
		termHeight = 3
	}

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
	m.terminals[wtID] = ts

	// Start reading from PTY
	return m.startPTYReader(wtID)
}

// readPTY reads from the PTY for the given worktree
func (m *TerminalModel) readPTY(wtID string) tea.Cmd {
	ts := m.terminals[wtID]
	if ts == nil || ts.Terminal == nil || !ts.Running {
		return nil
	}
	ch := ts.ptyMsgCh
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return messages.SidebarPTYStopped{WorktreeID: wtID, Err: io.EOF}
		}
		return msg
	}
}

func (m *TerminalModel) startPTYReader(wtID string) tea.Cmd {
	ts := m.terminals[wtID]
	if ts == nil || ts.readerActive || ts.Terminal == nil || !ts.Running {
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

	go runPTYReader(term, msgCh, cancel, wtID)

	return m.readPTY(wtID)
}

// CloseTerminal closes the terminal for the given worktree
func (m *TerminalModel) CloseTerminal(wtID string) {
	ts := m.terminals[wtID]
	if ts != nil {
		m.stopPTYReader(ts)
		ts.mu.Lock()
		if ts.Terminal != nil {
			ts.Terminal.Close()
		}
		ts.mu.Unlock()
		delete(m.terminals, wtID)
	}
}

// CloseAll closes all terminals
func (m *TerminalModel) CloseAll() {
	for wtID := range m.terminals {
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

func runPTYReader(term *pty.Terminal, msgCh chan tea.Msg, cancel <-chan struct{}, wtID string) {
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
					if !sendPTYMsg(msgCh, cancel, messages.SidebarPTYOutput{WorktreeID: wtID, Data: pending}) {
						close(msgCh)
						return
					}
				}
				if stoppedErr == nil {
					stoppedErr = io.EOF
				}
				sendPTYMsg(msgCh, cancel, messages.SidebarPTYStopped{WorktreeID: wtID, Err: stoppedErr})
				close(msgCh)
				return
			}
			pending = append(pending, data...)
			if len(pending) >= ptyMaxPendingBytes {
				if !sendPTYMsg(msgCh, cancel, messages.SidebarPTYOutput{WorktreeID: wtID, Data: pending}) {
					close(msgCh)
					return
				}
				pending = nil
			}
		case <-ticker.C:
			if len(pending) > 0 {
				if !sendPTYMsg(msgCh, cancel, messages.SidebarPTYOutput{WorktreeID: wtID, Data: pending}) {
					close(msgCh)
					return
				}
				pending = nil
			}
			if stoppedErr != nil {
				sendPTYMsg(msgCh, cancel, messages.SidebarPTYStopped{WorktreeID: wtID, Err: stoppedErr})
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

// SetOffset sets the absolute screen coordinates where the terminal starts
func (m *TerminalModel) SetOffset(x, y int) {
	m.offsetX = x
	m.offsetY = y
}

// screenToTerminal converts screen coordinates to terminal coordinates
func (m *TerminalModel) screenToTerminal(screenX, screenY int) (termX, termY int, inBounds bool) {
	termX = screenX - m.offsetX
	termY = screenY - m.offsetY

	// Check bounds
	// Terminal width/height are set in SetSize (minus 1 for help bar usually?)
	// Actually SetSize sets m.width and m.height for the whole pane section.
	// The VTerm is resized to m.width, m.height-1.

	ts := m.getTerminal()
	if ts != nil && ts.VTerm != nil {
		inBounds = termX >= 0 && termX < ts.VTerm.Width && termY >= 0 && termY < ts.VTerm.Height
	} else {
		// Fallback if no terminal
		width := m.width
		height := m.height - 1
		inBounds = termX >= 0 && termX < width && termY >= 0 && termY < height
	}
	return
}

// formatScrollPos formats scroll position for display
func formatScrollPos(offset, total int) string {
	if total == 0 {
		return "0/0"
	}
	return fmt.Sprintf("%d/%d lines up", offset, total)
}

// SendToTerminal sends a string directly to the current terminal
func (m *TerminalModel) SendToTerminal(s string) {
	ts := m.getTerminal()
	if ts != nil && ts.Terminal != nil {
		_ = ts.Terminal.SendString(s)
	}
}

// EnterCopyMode enters copy/scroll mode for the current terminal
func (m *TerminalModel) EnterCopyMode() {
	ts := m.getTerminal()
	if ts != nil {
		ts.CopyMode = true
		ts.mu.Lock()
		if ts.VTerm != nil {
			ts.CopyState = common.InitCopyState(ts.VTerm)
		}
		ts.mu.Unlock()
	}
}

// ExitCopyMode exits copy/scroll mode for the current terminal
func (m *TerminalModel) ExitCopyMode() {
	ts := m.getTerminal()
	if ts != nil {
		ts.CopyMode = false
		ts.mu.Lock()
		if ts.VTerm != nil {
			ts.VTerm.ClearSelection()
			ts.VTerm.ScrollViewToBottom()
		}
		ts.CopyState = common.CopyState{}
		ts.mu.Unlock()
	}
}

// CopyModeActive returns whether the current terminal is in copy mode
func (m *TerminalModel) CopyModeActive() bool {
	ts := m.getTerminal()
	return ts != nil && ts.CopyMode
}

// handleCopyModeKey handles keys while in copy mode (scroll navigation)
func (m *TerminalModel) handleCopyModeKey(ts *TerminalState, msg tea.KeyPressMsg) tea.Cmd {
	var copyText string
	var didCopy bool

	ts.mu.Lock()
	term := ts.VTerm
	if term == nil {
		ts.mu.Unlock()
		return nil
	}

	k := msg.Key()
	if ts.CopyState.SearchActive {
		switch {
		case k.Code == tea.KeyEsc || k.Code == tea.KeyEscape:
			common.CancelSearch(&ts.CopyState)
		case k.Code == tea.KeyEnter:
			common.ExecuteSearch(term, &ts.CopyState)
		case k.Code == tea.KeyBackspace:
			common.BackspaceSearchQuery(&ts.CopyState)
		default:
			if k.Text != "" && (k.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper)) == 0 {
				common.AppendSearchQuery(&ts.CopyState, k.Text)
			}
		}
		ts.mu.Unlock()
		return nil
	}

	switch {
	// Exit copy mode
	case k.Code == tea.KeyEsc || k.Code == tea.KeyEscape:
		fallthrough
	case msg.String() == "q":
		ts.CopyMode = false
		ts.CopyState = common.CopyState{}
		term.ClearSelection()
		term.ScrollViewToBottom()
		ts.mu.Unlock()
		return nil

	// Copy selection
	case k.Code == tea.KeyEnter || msg.String() == "y":
		copyText = common.CopySelectionText(term, &ts.CopyState)
		if copyText != "" {
			ts.CopyMode = false
			ts.CopyState = common.CopyState{}
			term.ClearSelection()
			term.ScrollViewToBottom()
			didCopy = true
		}
		ts.mu.Unlock()
		if didCopy {
			if err := common.CopyToClipboard(copyText); err != nil {
				logging.Error("Failed to copy sidebar selection: %v", err)
			} else {
				logging.Info("Copied %d chars from sidebar", len(copyText))
			}
		}
		return nil

	// Toggle selection
	case msg.String() == " " || msg.String() == "v":
		common.ToggleCopySelection(&ts.CopyState)
		common.SyncCopyState(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	// Toggle rectangle selection
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+v"))):
		common.ToggleRectangle(&ts.CopyState)
		common.SyncCopyState(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	// Search
	case msg.String() == "/":
		common.StartSearch(&ts.CopyState, false)
		ts.mu.Unlock()
		return nil

	case msg.String() == "?":
		common.StartSearch(&ts.CopyState, true)
		ts.mu.Unlock()
		return nil

	case msg.String() == "n":
		common.RepeatSearch(term, &ts.CopyState, ts.CopyState.LastSearchBackward)
		ts.mu.Unlock()
		return nil

	case msg.String() == "N":
		common.RepeatSearch(term, &ts.CopyState, !ts.CopyState.LastSearchBackward)
		ts.mu.Unlock()
		return nil

	// Move left/right
	case msg.String() == "h":
		fallthrough
	case k.Code == tea.KeyLeft:
		ts.CopyState.CursorX--
		common.SyncCopyState(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	case msg.String() == "l":
		fallthrough
	case k.Code == tea.KeyRight:
		ts.CopyState.CursorX++
		common.SyncCopyState(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	// Move up/down
	case msg.String() == "k":
		fallthrough
	case k.Code == tea.KeyUp:
		ts.CopyState.CursorLine--
		common.SyncCopyState(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	case msg.String() == "j":
		fallthrough
	case k.Code == tea.KeyDown:
		ts.CopyState.CursorLine++
		common.SyncCopyState(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	// Word motions
	case msg.String() == "w":
		common.MoveWordForward(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	case msg.String() == "b":
		common.MoveWordBackward(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	case msg.String() == "e":
		common.MoveWordEnd(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	// Scroll up/down half page
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+u"))):
		fallthrough
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+b"))):
		fallthrough
	case k.Code == tea.KeyPgUp:
		delta := term.Height / 2
		if delta < 1 {
			delta = 1
		}
		ts.CopyState.CursorLine -= delta
		common.SyncCopyState(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+d"))):
		fallthrough
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+f"))):
		fallthrough
	case k.Code == tea.KeyPgDown:
		delta := term.Height / 2
		if delta < 1 {
			delta = 1
		}
		ts.CopyState.CursorLine += delta
		common.SyncCopyState(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	// Scroll to top/bottom
	case msg.String() == "g":
		ts.CopyState.CursorLine = 0
		common.SyncCopyState(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	case msg.String() == "G":
		total := term.TotalLines()
		if total > 0 {
			ts.CopyState.CursorLine = total - 1
		}
		common.SyncCopyState(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	// Move to top/middle/bottom of view
	case msg.String() == "H":
		start, end, _ := term.VisibleLineRange()
		if end > start {
			ts.CopyState.CursorLine = start
			common.SyncCopyState(term, &ts.CopyState)
		}
		ts.mu.Unlock()
		return nil

	case msg.String() == "M":
		start, end, _ := term.VisibleLineRange()
		if end > start {
			ts.CopyState.CursorLine = start + (end-start)/2
			common.SyncCopyState(term, &ts.CopyState)
		}
		ts.mu.Unlock()
		return nil

	case msg.String() == "L":
		start, end, _ := term.VisibleLineRange()
		if end > start {
			ts.CopyState.CursorLine = end - 1
			common.SyncCopyState(term, &ts.CopyState)
		}
		ts.mu.Unlock()
		return nil

	// Line start/end
	case msg.String() == "0":
		fallthrough
	case k.Code == tea.KeyHome:
		ts.CopyState.CursorX = 0
		common.SyncCopyState(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil

	case msg.String() == "$":
		fallthrough
	case k.Code == tea.KeyEnd:
		ts.CopyState.CursorX = term.Width - 1
		common.SyncCopyState(term, &ts.CopyState)
		ts.mu.Unlock()
		return nil
	}

	ts.mu.Unlock()
	// Ignore other keys in copy mode
	return nil
}
