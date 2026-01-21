package sidebar

import (
	"strings"
	"sync"
	"time"

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
		return m.handleMouseClick(msg)

	case tea.MouseMotionMsg:
		return m.handleMouseMotion(msg)

	case tea.MouseReleaseMsg:
		return m.handleMouseRelease(msg)

	case tea.MouseWheelMsg:
		if !m.focused {
			return m, nil
		}
		ts := m.getTerminal()
		if ts == nil || ts.VTerm == nil {
			return m, nil
		}
		ts.mu.Lock()
		delta := common.ScrollDeltaForHeight(ts.VTerm.Height, 8) // ~12.5% of viewport
		if msg.Button == tea.MouseWheelUp {
			ts.VTerm.ScrollView(delta)
		} else if msg.Button == tea.MouseWheelDown {
			ts.VTerm.ScrollView(-delta)
		}
		ts.mu.Unlock()
		return m, nil

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
