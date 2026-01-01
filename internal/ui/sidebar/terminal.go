package sidebar

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/vterm"
)

const (
	ptyFlushQuiet       = 12 * time.Millisecond
	ptyFlushMaxInterval = 50 * time.Millisecond
	ptyFlushQuietAlt    = 30 * time.Millisecond
	ptyFlushMaxAlt      = 120 * time.Millisecond
)

// TerminalState holds the terminal state for a worktree
type TerminalState struct {
	Terminal *pty.Terminal
	VTerm    *vterm.VTerm
	Running  bool
	mu       sync.Mutex

	// Track last size to avoid unnecessary resizes
	lastWidth  int
	lastHeight int

	// PTY output buffering
	pendingOutput     []byte
	flushScheduled    bool
	lastOutputAt      time.Time
	flushPendingSince time.Time
}

// terminalKeyMap holds pre-allocated key bindings for the terminal
type terminalKeyMap struct {
	ScrollUp   key.Binding
	ScrollDown key.Binding
	Home       key.Binding
	End        key.Binding
}

func newTerminalKeyMap() terminalKeyMap {
	return terminalKeyMap{
		ScrollUp:   key.NewBinding(key.WithKeys("ctrl+u")),
		ScrollDown: key.NewBinding(key.WithKeys("ctrl+d")),
		Home:       key.NewBinding(key.WithKeys("home")),
		End:        key.NewBinding(key.WithKeys("end")),
	}
}

// TerminalModel is the Bubbletea model for the sidebar terminal section
type TerminalModel struct {
	// State per worktree
	terminals map[string]*TerminalState

	// Current worktree
	worktree *data.Worktree

	// Layout
	width   int
	height  int
	focused bool

	// Styles
	styles common.Styles

	// Pre-allocated key bindings
	keys terminalKeyMap
}

// NewTerminalModel creates a new sidebar terminal model
func NewTerminalModel() *TerminalModel {
	return &TerminalModel{
		terminals: make(map[string]*TerminalState),
		styles:    common.DefaultStyles(),
		keys:      newTerminalKeyMap(),
	}
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

	if ts.VTerm != nil && (ts.VTerm.AltScreen || ts.VTerm.SyncActive()) {
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
	case tea.KeyMsg:
		if !m.focused {
			return m, nil
		}

		ts := m.getTerminal()
		if ts == nil || ts.Terminal == nil {
			return m, nil
		}

		// Handle scroll keys
		switch {
		case msg.Type == tea.KeyPgUp:
			ts.mu.Lock()
			if ts.VTerm != nil {
				ts.VTerm.ScrollView(ts.VTerm.Height / 2)
			}
			ts.mu.Unlock()
			return m, nil

		case msg.Type == tea.KeyPgDown:
			ts.mu.Lock()
			if ts.VTerm != nil {
				ts.VTerm.ScrollView(-ts.VTerm.Height / 2)
			}
			ts.mu.Unlock()
			return m, nil

		case key.Matches(msg, m.keys.ScrollUp):
			ts.mu.Lock()
			if ts.VTerm != nil {
				ts.VTerm.ScrollView(ts.VTerm.Height / 2)
			}
			ts.mu.Unlock()
			return m, nil

		case key.Matches(msg, m.keys.ScrollDown):
			ts.mu.Lock()
			if ts.VTerm != nil {
				ts.VTerm.ScrollView(-ts.VTerm.Height / 2)
			}
			ts.mu.Unlock()
			return m, nil

		case key.Matches(msg, m.keys.Home):
			ts.mu.Lock()
			if ts.VTerm != nil {
				ts.VTerm.ScrollViewToTop()
			}
			ts.mu.Unlock()
			return m, nil

		case key.Matches(msg, m.keys.End):
			ts.mu.Lock()
			if ts.VTerm != nil {
				ts.VTerm.ScrollViewToBottom()
			}
			ts.mu.Unlock()
			return m, nil

		default:
			// If scrolled, any typing goes back to live and sends key
			ts.mu.Lock()
			if ts.VTerm != nil && ts.VTerm.IsScrolled() {
				ts.VTerm.ScrollViewToBottom()
			}
			ts.mu.Unlock()

			// Send input to terminal
			input := keyToBytes(msg)
			if len(input) > 0 {
				_ = ts.Terminal.SendString(string(input))
			}
			return m, nil
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
					ts.VTerm.Write(ts.pendingOutput)
				}
				ts.mu.Unlock()
				ts.pendingOutput = ts.pendingOutput[:0]
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
			logging.Info("Sidebar PTY stopped for worktree %s: %v", msg.WorktreeID, msg.Err)
		}

	case SidebarTerminalCreated:
		cmd := m.HandleTerminalCreated(msg.WorktreeID, msg.Terminal)
		cmds = append(cmds, cmd)
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
		content := ts.VTerm.Render()
		isScrolled := ts.VTerm.IsScrolled()
		var scrollInfo string
		if isScrolled {
			offset, total := ts.VTerm.GetScrollInfo()
			scrollInfo = formatScrollPos(offset, total)
		}
		ts.mu.Unlock()

		b.WriteString(content)

		if isScrolled {
			b.WriteString("\n")
			scrollStyle := lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#1a1b26")).
				Background(lipgloss.Color("#e0af68"))
			b.WriteString(scrollStyle.Render(" SCROLL: " + scrollInfo + " "))
		}
	}

	// Help bar
	helpItems := []string{
		m.styles.HelpKey.Render("^u/d") + m.styles.HelpDesc.Render(":scroll"),
		m.styles.HelpKey.Render("^c") + m.styles.HelpDesc.Render(":interrupt"),
	}
	help := strings.Join(helpItems, "  ")

	// Pad to fill height
	contentHeight := strings.Count(b.String(), "\n") + 1
	targetHeight := m.height - 1 // Account for help
	if targetHeight < 0 {
		targetHeight = 0
	}
	if targetHeight > contentHeight {
		b.WriteString(strings.Repeat("\n", targetHeight-contentHeight))
	}
	b.WriteString(help)

	return b.String()
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

		term, err := pty.New(shell, wt.Root, nil)
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
	termWidth := m.width - 4
	termHeight := m.height - 4
	if termWidth < 10 {
		termWidth = 10
	}
	if termHeight < 3 {
		termHeight = 3
	}

	vt := vterm.New(termWidth, termHeight)
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
	return m.readPTY(wtID)
}

// readPTY reads from the PTY for the given worktree
func (m *TerminalModel) readPTY(wtID string) tea.Cmd {
	ts := m.terminals[wtID]
	if ts == nil || ts.Terminal == nil || !ts.Running {
		return nil
	}

	term := ts.Terminal
	return func() tea.Msg {
		buf := make([]byte, 4096)
		n, err := term.Read(buf)
		if err != nil {
			return messages.SidebarPTYStopped{WorktreeID: wtID, Err: err}
		}
		if n > 0 {
			return messages.SidebarPTYOutput{WorktreeID: wtID, Data: buf[:n]}
		}
		return messages.SidebarPTYTick{WorktreeID: wtID}
	}
}

// CloseTerminal closes the terminal for the given worktree
func (m *TerminalModel) CloseTerminal(wtID string) {
	ts := m.terminals[wtID]
	if ts != nil {
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

// formatScrollPos formats scroll position for display
func formatScrollPos(offset, total int) string {
	if total == 0 {
		return "0/0"
	}
	return fmt.Sprintf("%d/%d lines up", offset, total)
}

// keyToBytes converts a key message to bytes for the terminal
func keyToBytes(msg tea.KeyMsg) []byte {
	switch msg.Type {
	case tea.KeyEnter:
		return []byte{'\r'}
	case tea.KeyBackspace:
		return []byte{0x7f}
	case tea.KeyTab:
		return []byte{'\t'}
	case tea.KeyShiftTab:
		return []byte{0x1b, '[', 'Z'}
	case tea.KeySpace:
		return []byte{' '}
	case tea.KeyEsc:
		return []byte{0x1b}
	case tea.KeyUp:
		return []byte{0x1b, '[', 'A'}
	case tea.KeyDown:
		return []byte{0x1b, '[', 'B'}
	case tea.KeyRight:
		return []byte{0x1b, '[', 'C'}
	case tea.KeyLeft:
		return []byte{0x1b, '[', 'D'}
	case tea.KeyHome:
		return []byte{0x1b, '[', 'H'}
	case tea.KeyEnd:
		return []byte{0x1b, '[', 'F'}
	case tea.KeyDelete:
		return []byte{0x1b, '[', '3', '~'}
	case tea.KeyPgUp:
		return []byte{0x1b, '[', '5', '~'}
	case tea.KeyPgDown:
		return []byte{0x1b, '[', '6', '~'}
	case tea.KeyCtrlA:
		return []byte{0x01}
	case tea.KeyCtrlB:
		return []byte{0x02}
	case tea.KeyCtrlC:
		return []byte{0x03}
	case tea.KeyCtrlD:
		return []byte{0x04}
	case tea.KeyCtrlE:
		return []byte{0x05}
	case tea.KeyCtrlF:
		return []byte{0x06}
	case tea.KeyCtrlG:
		return []byte{0x07}
	case tea.KeyCtrlH:
		return []byte{0x08}
	case tea.KeyCtrlJ:
		return []byte{0x0a}
	case tea.KeyCtrlK:
		return []byte{0x0b}
	case tea.KeyCtrlL:
		return []byte{0x0c}
	case tea.KeyCtrlN:
		return []byte{0x0e}
	case tea.KeyCtrlO:
		return []byte{0x0f}
	case tea.KeyCtrlP:
		return []byte{0x10}
	case tea.KeyCtrlR:
		return []byte{0x12}
	case tea.KeyCtrlS:
		return []byte{0x13}
	case tea.KeyCtrlT:
		return []byte{0x14}
	case tea.KeyCtrlU:
		return []byte{0x15}
	case tea.KeyCtrlV:
		return []byte{0x16}
	case tea.KeyCtrlW:
		return []byte{0x17}
	case tea.KeyCtrlX:
		return []byte{0x18}
	case tea.KeyCtrlY:
		return []byte{0x19}
	case tea.KeyCtrlZ:
		return []byte{0x1a}
	case tea.KeyRunes:
		return []byte(string(msg.Runes))
	default:
		s := msg.String()
		if len(s) == 1 {
			return []byte(s)
		}
		return nil
	}
}
