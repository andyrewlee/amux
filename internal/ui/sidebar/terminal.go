package sidebar

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/atotto/clipboard"
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
	ptyFlushChunkSize   = 32 * 1024
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

	// Selection state
	Selection SelectionState
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
	offsetX int
	offsetY int

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
	case tea.MouseMsg:
		if !m.focused {
			return m, nil
		}

		ts := m.getTerminal()
		if ts == nil {
			return m, nil
		}

		termX, termY, inBounds := m.screenToTerminal(msg.X, msg.Y)

		switch msg.Action {
		case tea.MouseActionPress:
			if msg.Button == tea.MouseButtonLeft {
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
						ts.VTerm.SetSelection(termX, termY, termX, termY, true)
					}
				} else {
					ts.Selection = SelectionState{}
				}
				ts.mu.Unlock()
			}

		case tea.MouseActionMotion:
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
						termX, termY, true,
					)
				}
			}
			ts.mu.Unlock()

		case tea.MouseActionRelease:
			if msg.Button == tea.MouseButtonLeft {
				ts.mu.Lock()
				if ts.Selection.Active {
					if ts.VTerm != nil {
						text := ts.VTerm.GetSelectedText(
							ts.Selection.StartX, ts.Selection.StartY,
							ts.Selection.EndX, ts.Selection.EndY,
						)
						if text != "" {
							if err := clipboard.WriteAll(text); err != nil {
								logging.Error("Failed to copy sidebar selection: %v", err)
							} else {
								logging.Info("Copied %d chars from sidebar", len(text))
							}
						}
					}
					ts.Selection.Active = false
				}
				ts.mu.Unlock()
			}
		}

	case tea.KeyMsg:
		if !m.focused {
			return m, nil
		}

		ts := m.getTerminal()
		if ts == nil || ts.Terminal == nil {
			return m, nil
		}

		// Handle bracketed paste - send entire content at once with escape sequences
		// This is much faster than processing character by character
		if msg.Paste && msg.Type == tea.KeyRunes {
			text := string(msg.Runes)
			bracketedText := "\x1b[200~" + text + "\x1b[201~"
			_ = ts.Terminal.SendString(bracketedText)
			logging.Debug("Sidebar terminal pasted %d bytes via bracketed paste", len(text))
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
			input := common.KeyToBytes(msg)
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
