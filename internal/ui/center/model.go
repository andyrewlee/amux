package center

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/compositor"
	"github.com/andyrewlee/amux/internal/vterm"
)

// TabID is a unique identifier for a tab that survives slice reordering
type TabID string

// tabIDCounter is used to generate unique tab IDs
var tabIDCounter uint64

// generateTabID creates a new unique tab ID
func generateTabID() TabID {
	id := atomic.AddUint64(&tabIDCounter, 1)
	return TabID(fmt.Sprintf("tab-%d", id))
}

// formatScrollPos formats the scroll position for display
func formatScrollPos(offset, total int) string {
	if total == 0 {
		return "0/0"
	}
	return fmt.Sprintf("%d/%d lines up", offset, total)
}

func nextAssistantName(assistant string, tabs []*Tab) string {
	assistant = strings.TrimSpace(assistant)
	if assistant == "" {
		return ""
	}

	used := make(map[string]struct{})
	for _, tab := range tabs {
		if tab == nil || tab.Assistant != assistant {
			continue
		}
		name := strings.TrimSpace(tab.Name)
		if name == "" {
			name = assistant
		}
		used[name] = struct{}{}
	}

	if _, ok := used[assistant]; !ok {
		return assistant
	}

	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s %d", assistant, i)
		if _, ok := used[candidate]; !ok {
			return candidate
		}
	}
}

// SelectionState tracks mouse selection state for copy/paste
type SelectionState struct {
	Active bool // Selection in progress?
	StartX int  // Start column (terminal coordinates)
	StartY int  // Start row (terminal coordinates, relative to visible area)
	EndX   int  // End column
	EndY   int  // End row
}

// Tab represents a single tab in the center pane
type Tab struct {
	ID           TabID // Unique identifier that survives slice reordering
	Name         string
	Assistant    string
	Worktree     *data.Worktree
	Agent        *appPty.Agent
	Terminal     *vterm.VTerm // Virtual terminal emulator with scrollback
	mu           sync.Mutex   // Protects Terminal
	Running      bool         // Whether the agent is actively running
	readerActive bool         // Guard to ensure only one PTY read loop per tab
	CopyMode     bool         // Whether the tab is in copy/scroll mode (keys not sent to PTY)
	// Buffer PTY output to avoid rendering partial screen updates.
	pendingOutput     []byte
	flushScheduled    bool
	lastOutputAt      time.Time
	flushPendingSince time.Time
	// Mouse selection state
	Selection SelectionState
}

// MonitorSnapshot captures a tab display for the monitor grid.
type MonitorSnapshot struct {
	Worktree  *data.Worktree
	Assistant string
	Name      string
	Running   bool
	Rendered  string
}

// MonitorTab describes a tab for the monitor grid.
type MonitorTab struct {
	ID        TabID
	Worktree  *data.Worktree
	Assistant string
	Name      string
	Running   bool
}

// TabSize defines a desired size for a tab.
type TabSize struct {
	ID     TabID
	Width  int
	Height int
}

// MonitorTabSnapshot captures a monitor tab with its visible screen.
type MonitorTabSnapshot struct {
	MonitorTab
	Screen     [][]vterm.Cell
	CursorX    int
	CursorY    int
	ViewOffset int
	Width      int
	Height     int
}

// HandleMonitorInput forwards input to a specific tab while in monitor view.
func (m *Model) HandleMonitorInput(tabID TabID, msg tea.KeyMsg) tea.Cmd {
	tab := m.getTabByIDGlobal(tabID)
	if tab == nil || tab.Agent == nil || tab.Agent.Terminal == nil {
		return nil
	}

	// Handle bracketed paste - send entire content at once with escape sequences
	if msg.Paste && msg.Type == tea.KeyRunes {
		text := string(msg.Runes)
		bracketedText := "\x1b[200~" + text + "\x1b[201~"
		_ = tab.Agent.Terminal.SendString(bracketedText)
		return nil
	}

	switch {
	case msg.Type == tea.KeyPgUp:
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.ScrollView(tab.Terminal.Height / 2)
		}
		tab.mu.Unlock()
		return nil

	case msg.Type == tea.KeyPgDown:
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.ScrollView(-tab.Terminal.Height / 2)
		}
		tab.mu.Unlock()
		return nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+u"))):
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.ScrollView(tab.Terminal.Height / 2)
		}
		tab.mu.Unlock()
		return nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+d"))):
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.ScrollView(-tab.Terminal.Height / 2)
		}
		tab.mu.Unlock()
		return nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("home"))):
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.ScrollViewToTop()
		}
		tab.mu.Unlock()
		return nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("end"))):
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.ScrollViewToBottom()
		}
		tab.mu.Unlock()
		return nil
	}

	// If scrolled, any typing goes back to live and sends key.
	tab.mu.Lock()
	if tab.Terminal != nil && tab.Terminal.IsScrolled() {
		tab.Terminal.ScrollViewToBottom()
	}
	tab.mu.Unlock()

	input := common.KeyToBytes(msg)
	if len(input) > 0 {
		_ = tab.Agent.Terminal.SendString(string(input))
	}
	return nil
}

const (
	ptyFlushQuiet       = 4 * time.Millisecond
	ptyFlushMaxInterval = 16 * time.Millisecond
	ptyFlushQuietAlt    = 8 * time.Millisecond
	ptyFlushMaxAlt      = 32 * time.Millisecond
	// Inactive tabs still need to advance their terminal state, but can flush less frequently.
	ptyFlushInactiveMultiplier = 4
	ptyFlushChunkSize          = 32 * 1024
)

// Model is the Bubbletea model for the center pane
type Model struct {
	// State
	worktree            *data.Worktree
	tabsByWorktree      map[string][]*Tab // tabs per worktree ID
	activeTabByWorktree map[string]int    // active tab index per worktree
	focused             bool
	agentManager        *appPty.AgentManager
	monitor             MonitorModel
	terminalCanvas      *compositor.Canvas

	// Layout
	width   int
	height  int
	offsetX int // X offset from screen left (dashboard width)

	// Config
	config *config.Config
	styles common.Styles
}

// New creates a new center pane model
func New(cfg *config.Config) *Model {
	return &Model{
		tabsByWorktree:      make(map[string][]*Tab),
		activeTabByWorktree: make(map[string]int),
		config:              cfg,
		agentManager:        appPty.NewAgentManager(cfg),
		styles:              common.DefaultStyles(),
	}
}

// worktreeID returns the ID of the current worktree, or empty string
func (m *Model) worktreeID() string {
	if m.worktree == nil {
		return ""
	}
	return string(m.worktree.ID())
}

// getTabs returns the tabs for the current worktree
func (m *Model) getTabs() []*Tab {
	return m.tabsByWorktree[m.worktreeID()]
}

// getTabByID returns the tab with the given ID, or nil if not found
func (m *Model) getTabByID(wtID string, tabID TabID) *Tab {
	for _, tab := range m.tabsByWorktree[wtID] {
		if tab.ID == tabID {
			return tab
		}
	}
	return nil
}

// getActiveTabIdx returns the active tab index for the current worktree
func (m *Model) getActiveTabIdx() int {
	return m.activeTabByWorktree[m.worktreeID()]
}

// setActiveTabIdx sets the active tab index for the current worktree
func (m *Model) setActiveTabIdx(idx int) {
	m.activeTabByWorktree[m.worktreeID()] = idx
}

func (m *Model) isActiveTab(wtID string, tabID TabID) bool {
	if m.worktree == nil || wtID != m.worktreeID() {
		return false
	}
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if activeIdx < 0 || activeIdx >= len(tabs) {
		return false
	}
	return tabs[activeIdx].ID == tabID
}

func (m *Model) flushTiming(tab *Tab, active bool) (time.Duration, time.Duration) {
	quiet := ptyFlushQuiet
	maxInterval := ptyFlushMaxInterval

	tab.mu.Lock()
	// Only use slower Alt timing for true AltScreen mode (full-screen TUIs like vim).
	// SyncActive (DEC 2026) already handles partial updates via screen snapshots,
	// so we don't need slower flush timing - it just makes streaming text feel laggy.
	if tab.Terminal != nil && tab.Terminal.AltScreen {
		quiet = ptyFlushQuietAlt
		maxInterval = ptyFlushMaxAlt
	}
	tab.mu.Unlock()

	if !active {
		quiet *= ptyFlushInactiveMultiplier
		maxInterval *= ptyFlushInactiveMultiplier
		if maxInterval < quiet {
			maxInterval = quiet
		}
	}

	return quiet, maxInterval
}

// removeTab removes a tab at index from the current worktree
func (m *Model) removeTab(idx int) {
	wtID := m.worktreeID()
	tabs := m.tabsByWorktree[wtID]
	if idx >= 0 && idx < len(tabs) {
		m.tabsByWorktree[wtID] = append(tabs[:idx], tabs[idx+1:]...)
	}
}

// Init initializes the center pane
func (m *Model) Init() tea.Cmd {
	return nil
}

// PTYOutput is a message containing PTY output data
type PTYOutput struct {
	WorktreeID string
	TabID      TabID
	Data       []byte
}

// PTYTick triggers a PTY read
type PTYTick struct {
	WorktreeID string
	TabID      TabID
}

// PTYFlush applies buffered PTY output for a tab.
type PTYFlush struct {
	WorktreeID string
	TabID      TabID
}

// PTYStopped signals that the PTY read loop has stopped (terminal closed or error)
type PTYStopped struct {
	WorktreeID string
	TabID      TabID
	Err        error
}

// Update handles messages
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.MouseMsg:
		// Handle mouse events for text selection
		if !m.focused || !m.hasActiveAgent() {
			return m, nil
		}

		tabs := m.getTabs()
		activeIdx := m.getActiveTabIdx()
		if activeIdx >= len(tabs) {
			return m, nil
		}
		tab := tabs[activeIdx]

		// Convert screen coordinates to terminal coordinates
		termX, termY, inBounds := m.screenToTerminal(msg.X, msg.Y)

		switch msg.Action {
		case tea.MouseActionPress:
			if msg.Button == tea.MouseButtonLeft {
				tab.mu.Lock()
				// Clear any existing selection first
				if tab.Terminal != nil {
					tab.Terminal.ClearSelection()
				}

				if inBounds {
					// Start new selection
					tab.Selection = SelectionState{
						Active: true,
						StartX: termX,
						StartY: termY,
						EndX:   termX,
						EndY:   termY,
					}
					if tab.Terminal != nil {
						tab.Terminal.SetSelection(termX, termY, termX, termY, true)
					}
					logging.Debug("Selection started at (%d, %d)", termX, termY)
				} else {
					// Clicked outside terminal content, just clear selection
					tab.Selection = SelectionState{}
				}
				tab.mu.Unlock()
			}

		case tea.MouseActionMotion:
			// Update selection while dragging
			tab.mu.Lock()
			if tab.Selection.Active {
				termWidth := m.width - 4
				termHeight := m.height - 6
				if termWidth < 10 {
					termWidth = 80
				}
				if termHeight < 5 {
					termHeight = 24
				}

				// Clamp to terminal bounds
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

				tab.Selection.EndX = termX
				tab.Selection.EndY = termY
				if tab.Terminal != nil {
					tab.Terminal.SetSelection(
						tab.Selection.StartX, tab.Selection.StartY,
						termX, termY, true,
					)
				}
			}
			tab.mu.Unlock()

		case tea.MouseActionRelease:
			if msg.Button == tea.MouseButtonLeft {
				tab.mu.Lock()
				if tab.Selection.Active {
					// Extract selected text and copy to clipboard
					if tab.Terminal != nil {
						text := tab.Terminal.GetSelectedText(
							tab.Selection.StartX, tab.Selection.StartY,
							tab.Selection.EndX, tab.Selection.EndY,
						)
						if text != "" {
							if err := clipboard.WriteAll(text); err != nil {
								logging.Error("Failed to copy to clipboard: %v", err)
							} else {
								logging.Info("Copied %d chars to clipboard", len(text))
							}
						}
						// Keep selection visible - don't clear it
						// Selection will be cleared when user clicks again or types
					}
					// Mark selection as no longer being dragged, but keep it visible
					tab.Selection.Active = false
				}
				tab.mu.Unlock()
			}
		}
		return m, tea.Batch(cmds...)

	case tea.KeyMsg:
		tabs := m.getTabs()
		activeIdx := m.getActiveTabIdx()
		logging.Debug("Center received key: %s, focused=%v, hasTabs=%v, numTabs=%d",
			msg.String(), m.focused, m.hasActiveAgent(), len(tabs))

		// Clear any selection when user types
		if len(tabs) > 0 && activeIdx < len(tabs) {
			tab := tabs[activeIdx]
			tab.mu.Lock()
			if tab.Terminal != nil {
				tab.Terminal.ClearSelection()
			}
			tab.Selection = SelectionState{}
			tab.mu.Unlock()
		}

		if !m.focused {
			logging.Debug("Center not focused, ignoring key")
			return m, nil
		}

		// When we have an active agent, handle keys
		if m.hasActiveAgent() {
			tab := tabs[activeIdx]
			logging.Debug("Has active agent, Agent=%v, Terminal=%v, CopyMode=%v", tab.Agent != nil, tab.Agent != nil && tab.Agent.Terminal != nil, tab.CopyMode)

			// Copy mode: handle scroll navigation without sending to PTY
			if tab.CopyMode {
				return m, m.handleCopyModeKey(tab, msg)
			}

			if tab.Agent != nil && tab.Agent.Terminal != nil {
				// Handle bracketed paste - send entire content at once with escape sequences
				if msg.Paste && msg.Type == tea.KeyRunes {
					text := string(msg.Runes)
					bracketedText := "\x1b[200~" + text + "\x1b[201~"
					_ = tab.Agent.Terminal.SendString(bracketedText)
					logging.Debug("Pasted %d bytes via bracketed paste", len(text))
					return m, nil
				}

				// Handle Ctrl+[ as Escape (some terminals report it this way)
				if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+["))) {
					_ = tab.Agent.Terminal.SendString("\x1b")
					return m, nil
				}

				// PgUp/PgDown for scrollback (these don't conflict with embedded TUIs)
				switch msg.Type {
				case tea.KeyPgUp:
					tab.mu.Lock()
					if tab.Terminal != nil {
						tab.Terminal.ScrollView(tab.Terminal.Height / 2)
					}
					tab.mu.Unlock()
					return m, nil

				case tea.KeyPgDown:
					tab.mu.Lock()
					if tab.Terminal != nil {
						tab.Terminal.ScrollView(-tab.Terminal.Height / 2)
					}
					tab.mu.Unlock()
					return m, nil
				}

				// If scrolled, any typing goes back to live and sends key
				tab.mu.Lock()
				if tab.Terminal != nil && tab.Terminal.IsScrolled() {
					tab.Terminal.ScrollViewToBottom()
				}
				tab.mu.Unlock()

				// Forward ALL keys to terminal (no Ctrl interceptions)
				input := common.KeyToBytes(msg)
				if len(input) > 0 {
					logging.Debug("Sending to terminal: %q (len=%d)", input, len(input))
					_ = tab.Agent.Terminal.SendString(string(input))
				} else {
					logging.Debug("keyToBytes returned empty for: %s", msg.String())
				}
				return m, nil
			}
		}

	case messages.LaunchAgent:
		return m, m.createAgentTab(msg.Assistant, msg.Worktree)

	case PTYOutput:
		tab := m.getTabByID(msg.WorktreeID, msg.TabID)
		if tab != nil {
			tab.pendingOutput = append(tab.pendingOutput, msg.Data...)
			tab.lastOutputAt = time.Now()
			if !tab.flushScheduled {
				tab.flushScheduled = true
				tab.flushPendingSince = tab.lastOutputAt
				quiet, _ := m.flushTiming(tab, m.isActiveTab(msg.WorktreeID, msg.TabID))
				tabID := msg.TabID // Capture for closure
				cmds = append(cmds, tea.Tick(quiet, func(t time.Time) tea.Msg {
					return PTYFlush{WorktreeID: msg.WorktreeID, TabID: tabID}
				}))
			}
			// Continue reading
			cmds = append(cmds, m.readPTYForTab(msg.WorktreeID, msg.TabID))
		}
		// If tab is nil, it was closed - silently drop the message and don't reschedule

	case PTYFlush:
		tab := m.getTabByID(msg.WorktreeID, msg.TabID)
		if tab != nil {
			now := time.Now()
			quietFor := now.Sub(tab.lastOutputAt)
			pendingFor := time.Duration(0)
			if !tab.flushPendingSince.IsZero() {
				pendingFor = now.Sub(tab.flushPendingSince)
			}
			quiet, maxInterval := m.flushTiming(tab, m.isActiveTab(msg.WorktreeID, msg.TabID))
			if quietFor < quiet && pendingFor < maxInterval {
				delay := quiet - quietFor
				if delay < time.Millisecond {
					delay = time.Millisecond
				}
				tabID := msg.TabID
				tab.flushScheduled = true
				cmds = append(cmds, tea.Tick(delay, func(t time.Time) tea.Msg {
					return PTYFlush{WorktreeID: msg.WorktreeID, TabID: tabID}
				}))
				break
			}

			tab.flushScheduled = false
			tab.flushPendingSince = time.Time{}
			if len(tab.pendingOutput) > 0 {
				tab.mu.Lock()
				if tab.Terminal != nil {
					chunkSize := len(tab.pendingOutput)
					if chunkSize > ptyFlushChunkSize {
						chunkSize = ptyFlushChunkSize
					}
					tab.Terminal.Write(tab.pendingOutput[:chunkSize])
					copy(tab.pendingOutput, tab.pendingOutput[chunkSize:])
					tab.pendingOutput = tab.pendingOutput[:len(tab.pendingOutput)-chunkSize]
				}
				tab.mu.Unlock()
				if len(tab.pendingOutput) == 0 {
					tab.pendingOutput = tab.pendingOutput[:0]
				} else {
					tab.flushScheduled = true
					tab.flushPendingSince = time.Now()
					tabID := msg.TabID
					cmds = append(cmds, tea.Tick(time.Millisecond, func(t time.Time) tea.Msg {
						return PTYFlush{WorktreeID: msg.WorktreeID, TabID: tabID}
					}))
				}
			}
		}

	case PTYTick:
		tab := m.getTabByID(msg.WorktreeID, msg.TabID)
		if tab != nil {
			cmds = append(cmds, m.readPTYForTab(msg.WorktreeID, msg.TabID))
		}
		// If tab is nil, it was closed - stop polling

	case PTYStopped:
		// Terminal closed - mark tab as not running, but keep it visible
		tab := m.getTabByID(msg.WorktreeID, msg.TabID)
		if tab != nil {
			tab.Running = false
			tab.readerActive = false
			logging.Info("PTY stopped for tab %s: %v", msg.TabID, msg.Err)
		}
		// Do NOT schedule another read - the loop is done
	}

	return m, tea.Batch(cmds...)
}

// View renders the center pane
func (m *Model) View() string {
	var b strings.Builder

	// Tab bar
	b.WriteString(m.renderTabBar())
	b.WriteString("\n")

	// Content
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 {
		b.WriteString(m.renderEmpty())
	} else if activeIdx < len(tabs) {
		tab := tabs[activeIdx]
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.ShowCursor = m.focused
			termWidth := tab.Terminal.Width
			termHeight := tab.Terminal.Height
			b.WriteString(m.renderTerminalCanvas(tab.Terminal, termWidth, termHeight, m.focused))

			// Show scroll indicator if scrolled
			if tab.CopyMode {
				modeStyle := lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("#1a1b26")).
					Background(lipgloss.Color("#ff9e64"))
				indicator := modeStyle.Render(" COPY MODE (q/Esc to exit) ")
				b.WriteString("\n" + indicator)
			} else if tab.Terminal.IsScrolled() {
				offset, total := tab.Terminal.GetScrollInfo()
				scrollStyle := lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("#1a1b26")).
					Background(lipgloss.Color("#e0af68"))
				indicator := scrollStyle.Render(" SCROLL: " + formatScrollPos(offset, total) + " ")
				b.WriteString("\n" + indicator)
			}
		}
		tab.mu.Unlock()
	}

	// Help bar with styled keys (prefix mode)
	helpItems := []string{
		m.styles.HelpKey.Render("C-Spc") + m.styles.HelpDesc.Render(":prefix"),
		m.styles.HelpKey.Render("+c") + m.styles.HelpDesc.Render(":new"),
		m.styles.HelpKey.Render("+x") + m.styles.HelpDesc.Render(":close"),
		m.styles.HelpKey.Render("+n/p") + m.styles.HelpDesc.Render(":tabs"),
		m.styles.HelpKey.Render("PgUp/Dn") + m.styles.HelpDesc.Render(":scroll"),
	}
	help := strings.Join(helpItems, "  ")
	// Pad to the inner pane height (border excluded), reserving the help line.
	contentHeight := strings.Count(b.String(), "\n") + 1
	innerHeight := m.height - 2
	if innerHeight < 0 {
		innerHeight = 0
	}
	targetHeight := innerHeight - 1 // help line
	if targetHeight < 0 {
		targetHeight = 0
	}
	if targetHeight > contentHeight {
		b.WriteString(strings.Repeat("\n", targetHeight-contentHeight))
	}
	b.WriteString(help)

	// Apply pane styling
	style := m.styles.Pane
	if m.focused {
		style = m.styles.FocusedPane
	}

	return style.Width(m.width - 2).Render(b.String())
}

// renderTabBar renders the tab bar with activity indicators
func (m *Model) renderTabBar() string {
	currentTabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()

	if len(currentTabs) == 0 {
		return m.styles.Muted.Render("No agents")
	}

	var renderedTabs []string
	for i, tab := range currentTabs {
		name := tab.Name
		if name == "" {
			name = tab.Assistant
		}

		// Add status indicator
		var indicator string
		if tab.Running {
			indicator = common.Icons.Running + " "
		} else {
			indicator = common.Icons.Idle + " "
		}

		// Get agent-specific color
		var agentStyle lipgloss.Style
		switch tab.Assistant {
		case "claude":
			agentStyle = m.styles.AgentClaude
		case "codex":
			agentStyle = m.styles.AgentCodex
		case "gemini":
			agentStyle = m.styles.AgentGemini
		case "amp":
			agentStyle = m.styles.AgentAmp
		case "opencode":
			agentStyle = m.styles.AgentOpencode
		default:
			agentStyle = m.styles.AgentTerm
		}

		// Build tab content with agent-colored indicator
		content := agentStyle.Render(indicator) + name

		if i == activeIdx {
			// Active tab gets highlight border
			renderedTabs = append(renderedTabs, m.styles.ActiveTab.Render(content))
		} else {
			// Inactive tab
			renderedTabs = append(renderedTabs, m.styles.Tab.Render(content))
		}
	}

	// Add the plus button with matching border style
	plusBtn := m.styles.TabPlus.Render("[+]")
	renderedTabs = append(renderedTabs, plusBtn)

	// Join tabs horizontally at the bottom so borders align
	return lipgloss.JoinHorizontal(lipgloss.Bottom, renderedTabs...)
}

// renderEmpty renders the empty state
func (m *Model) renderEmpty() string {
	var b strings.Builder
	b.WriteString("\n\n")
	b.WriteString(m.styles.Title.Render("No agents running"))
	b.WriteString("\n\n")
	b.WriteString(m.styles.Muted.Render("Press "))
	b.WriteString(m.styles.HelpKey.Render("C-Space c"))
	b.WriteString(m.styles.Muted.Render(" to launch an agent"))
	return b.String()
}

// createAgentTab creates a new agent tab
func (m *Model) createAgentTab(assistant string, wt *data.Worktree) tea.Cmd {
	return func() tea.Msg {
		logging.Info("Creating agent tab: assistant=%s worktree=%s", assistant, wt.Name)
		agent, err := m.agentManager.CreateAgent(wt, appPty.AgentType(assistant))
		if err != nil {
			logging.Error("Failed to create agent: %v", err)
			return messages.Error{Err: err, Context: "creating agent"}
		}

		logging.Info("Agent created, Terminal=%v", agent.Terminal != nil)

		// Calculate terminal dimensions
		termWidth := m.width - 4
		termHeight := m.height - 6
		if termWidth < 10 {
			termWidth = 80
		}
		if termHeight < 5 {
			termHeight = 24
		}

		// Create virtual terminal emulator with scrollback
		term := vterm.New(termWidth, termHeight)

		// Set up response writer for terminal queries (DSR, DA, etc.)
		if agent.Terminal != nil {
			term.SetResponseWriter(func(data []byte) {
				_ = agent.Terminal.SendString(string(data))
			})
		}

		// Create tab with unique ID
		wtID := string(wt.ID())
		displayName := nextAssistantName(assistant, m.tabsByWorktree[wtID])
		tab := &Tab{
			ID:        generateTabID(),
			Name:      displayName,
			Assistant: assistant,
			Worktree:  wt,
			Agent:     agent,
			Terminal:  term,
			Running:   true, // Agent starts running
		}

		// Set PTY size to match
		if agent.Terminal != nil {
			_ = agent.Terminal.SetSize(uint16(termHeight), uint16(termWidth))
			logging.Info("Terminal size set to %dx%d", termWidth, termHeight)
		}

		// Add tab to the worktree's tab list
		m.tabsByWorktree[wtID] = append(m.tabsByWorktree[wtID], tab)
		m.activeTabByWorktree[wtID] = len(m.tabsByWorktree[wtID]) - 1

		return messages.TabCreated{Index: m.activeTabByWorktree[wtID], Name: displayName}
	}
}

// readPTYForTab reads from the PTY for a tab in a specific worktree
func (m *Model) readPTYForTab(wtID string, tabID TabID) tea.Cmd {
	tab := m.getTabByID(wtID, tabID)
	if tab == nil {
		// Tab no longer exists, stop the read loop
		return nil
	}

	if tab.Agent == nil || tab.Agent.Terminal == nil {
		tab.readerActive = false
		return nil
	}

	// Check if terminal is already closed before starting read
	if tab.Agent.Terminal.IsClosed() {
		tab.readerActive = false
		return nil
	}

	return func() tea.Msg {
		buf := make([]byte, 4096)
		n, err := tab.Agent.Terminal.Read(buf)
		if err != nil {
			// PTY closed or error - stop the read loop entirely
			return PTYStopped{WorktreeID: wtID, TabID: tabID, Err: err}
		}
		if n > 0 {
			return PTYOutput{WorktreeID: wtID, TabID: tabID, Data: buf[:n]}
		}
		// No data but no error - continue polling
		return PTYTick{WorktreeID: wtID, TabID: tabID}
	}
}

func (m *Model) startPTYReader(wtID string, tab *Tab) tea.Cmd {
	if tab == nil || tab.readerActive {
		return nil
	}
	if tab.Agent == nil || tab.Agent.Terminal == nil || tab.Agent.Terminal.IsClosed() {
		tab.readerActive = false
		return nil
	}
	tab.readerActive = true
	return m.readPTYForTab(wtID, tab.ID)
}

// closeCurrentTab closes the current tab
func (m *Model) closeCurrentTab() tea.Cmd {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()

	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return nil
	}

	tab := tabs[activeIdx]
	index := activeIdx

	// Close agent
	if tab.Agent != nil {
		_ = m.agentManager.CloseAgent(tab.Agent)
	}

	// Remove from tabs
	m.removeTab(index)

	// Adjust active tab
	tabs = m.getTabs() // Get updated tabs
	if activeIdx >= len(tabs) && activeIdx > 0 {
		m.setActiveTabIdx(activeIdx - 1)
	}

	return func() tea.Msg {
		return messages.TabClosed{Index: index}
	}
}

// hasActiveAgent returns whether there's an active agent
func (m *Model) hasActiveAgent() bool {
	tabs := m.getTabs()
	return len(tabs) > 0 && m.getActiveTabIdx() < len(tabs)
}

// nextTab switches to the next tab
func (m *Model) nextTab() {
	tabs := m.getTabs()
	if len(tabs) > 0 {
		m.setActiveTabIdx((m.getActiveTabIdx() + 1) % len(tabs))
	}
}

// prevTab switches to the previous tab
func (m *Model) prevTab() {
	tabs := m.getTabs()
	if len(tabs) > 0 {
		idx := m.getActiveTabIdx() - 1
		if idx < 0 {
			idx = len(tabs) - 1
		}
		m.setActiveTabIdx(idx)
	}
}

// Public wrappers for prefix mode commands

// NextTab switches to the next tab (public wrapper)
func (m *Model) NextTab() {
	m.nextTab()
}

// PrevTab switches to the previous tab (public wrapper)
func (m *Model) PrevTab() {
	m.prevTab()
}

// CloseActiveTab closes the current tab (public wrapper)
func (m *Model) CloseActiveTab() tea.Cmd {
	return m.closeCurrentTab()
}

// SelectTab switches to a specific tab by index (0-indexed)
func (m *Model) SelectTab(index int) {
	tabs := m.getTabs()
	if index >= 0 && index < len(tabs) {
		m.setActiveTabIdx(index)
	}
}

// EnterCopyMode enters copy/scroll mode for the active tab
func (m *Model) EnterCopyMode() {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return
	}
	tab := tabs[activeIdx]
	tab.CopyMode = true
}

// ExitCopyMode exits copy/scroll mode for the active tab
func (m *Model) ExitCopyMode() {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return
	}
	tab := tabs[activeIdx]
	tab.CopyMode = false
	tab.mu.Lock()
	if tab.Terminal != nil {
		tab.Terminal.ScrollViewToBottom()
	}
	tab.mu.Unlock()
}

// CopyModeActive returns whether the active tab is in copy mode
func (m *Model) CopyModeActive() bool {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return false
	}
	return tabs[activeIdx].CopyMode
}

// handleCopyModeKey handles keys while in copy mode (scroll navigation)
func (m *Model) handleCopyModeKey(tab *Tab, msg tea.KeyMsg) tea.Cmd {
	switch {
	// Exit copy mode
	case msg.Type == tea.KeyEsc:
		fallthrough
	case msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == 'q':
		tab.CopyMode = false
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.ScrollViewToBottom()
		}
		tab.mu.Unlock()
		return nil

	// Scroll up one line
	case msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == 'k':
		fallthrough
	case msg.Type == tea.KeyUp:
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.ScrollView(1)
		}
		tab.mu.Unlock()
		return nil

	// Scroll down one line
	case msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == 'j':
		fallthrough
	case msg.Type == tea.KeyDown:
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.ScrollView(-1)
		}
		tab.mu.Unlock()
		return nil

	// Scroll up half page
	case msg.Type == tea.KeyPgUp:
		fallthrough
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+u"))):
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.ScrollView(tab.Terminal.Height / 2)
		}
		tab.mu.Unlock()
		return nil

	// Scroll down half page
	case msg.Type == tea.KeyPgDown:
		fallthrough
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+d"))):
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.ScrollView(-tab.Terminal.Height / 2)
		}
		tab.mu.Unlock()
		return nil

	// Scroll to top
	case msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == 'g':
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.ScrollViewToTop()
		}
		tab.mu.Unlock()
		return nil

	// Scroll to bottom
	case msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == 'G':
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.ScrollViewToBottom()
		}
		tab.mu.Unlock()
		return nil
	}

	// Ignore other keys in copy mode (don't forward to PTY)
	return nil
}

// SendToTerminal sends a string directly to the active terminal
func (m *Model) SendToTerminal(s string) {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return
	}
	tab := tabs[activeIdx]
	if tab.Agent != nil && tab.Agent.Terminal != nil {
		_ = tab.Agent.Terminal.SendString(s)
	}
}

// SetSize sets the center pane size
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height

	termWidth := width - 4
	termHeight := height - 6
	if termWidth < 10 {
		termWidth = 80
	}
	if termHeight < 5 {
		termHeight = 24
	}

	// Update all terminals across all worktrees
	for _, tabs := range m.tabsByWorktree {
		for _, tab := range tabs {
			tab.mu.Lock()
			if tab.Terminal != nil {
				tab.Terminal.Resize(termWidth, termHeight)
			}
			tab.mu.Unlock()
			if tab.Agent != nil && tab.Agent.Terminal != nil {
				_ = tab.Agent.Terminal.SetSize(uint16(termHeight), uint16(termWidth))
			}
		}
	}
}

// SetOffset sets the X offset of the pane from screen left (for mouse coordinate conversion)
func (m *Model) SetOffset(x int) {
	m.offsetX = x
}

// screenToTerminal converts screen coordinates to terminal coordinates
// Returns the terminal X, Y and whether the coordinates are within the terminal content area
func (m *Model) screenToTerminal(screenX, screenY int) (termX, termY int, inBounds bool) {
	// Calculate content area offsets within the pane:
	// - Border: 1 on each side
	// - Padding: 1 on left
	// - Tab bar: 3 lines at top (border + content + border)
	borderTop := 1
	borderLeft := 1
	paddingLeft := 1
	tabBarHeight := 3

	// X offset: border + padding
	contentStartX := m.offsetX + borderLeft + paddingLeft
	// Y offset: border + tab bar
	contentStartY := borderTop + tabBarHeight

	// Terminal dimensions
	termWidth := m.width - 4
	termHeight := m.height - 6
	if termWidth < 10 {
		termWidth = 80
	}
	if termHeight < 5 {
		termHeight = 24
	}

	// Convert screen coordinates to terminal coordinates
	termX = screenX - contentStartX
	termY = screenY - contentStartY

	// Check bounds
	inBounds = termX >= 0 && termX < termWidth && termY >= 0 && termY < termHeight
	return
}

// Focus sets the focus state
func (m *Model) Focus() {
	m.focused = true
}

// Blur removes focus
func (m *Model) Blur() {
	m.focused = false
}

// Focused returns whether the center pane is focused
func (m *Model) Focused() bool {
	return m.focused
}

// SetWorktree sets the active worktree
func (m *Model) SetWorktree(wt *data.Worktree) {
	m.worktree = wt
}

// HasTabs returns whether there are any tabs for the current worktree
func (m *Model) HasTabs() bool {
	return len(m.getTabs()) > 0
}

// StartPTYReaders starts reading from all PTYs across all worktrees
func (m *Model) StartPTYReaders() tea.Cmd {
	var cmds []tea.Cmd
	for wtID, tabs := range m.tabsByWorktree {
		for _, tab := range tabs {
			if cmd := m.startPTYReader(wtID, tab); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	return tea.Batch(cmds...)
}

// Close cleans up all resources
func (m *Model) Close() {
	m.agentManager.CloseAll()
}

// GetTabsInfo returns information about current tabs for persistence
func (m *Model) GetTabsInfo() ([]data.TabInfo, int) {
	var result []data.TabInfo
	tabs := m.getTabs()
	for _, tab := range tabs {
		result = append(result, data.TabInfo{
			Assistant: tab.Assistant,
			Name:      tab.Name,
		})
	}
	return result, m.getActiveTabIdx()
}

// MonitorSnapshots returns a snapshot of each tab for the monitor grid.
func (m *Model) MonitorSnapshots() []MonitorSnapshot {
	tabs := m.monitorTabs()
	snapshots := make([]MonitorSnapshot, 0, len(tabs))
	for _, tab := range tabs {
		rendered := ""
		tab.mu.Lock()
		if tab.Terminal != nil {
			rendered = tab.Terminal.Render()
		}
		tab.mu.Unlock()
		snapshots = append(snapshots, MonitorSnapshot{
			Worktree:  tab.Worktree,
			Assistant: tab.Assistant,
			Name:      tab.Name,
			Running:   tab.Running,
			Rendered:  rendered,
		})
	}
	return snapshots
}

// MonitorTabs returns all tabs in a stable order for the monitor grid.
func (m *Model) MonitorTabs() []MonitorTab {
	tabs := m.monitorTabs()
	out := make([]MonitorTab, 0, len(tabs))
	for _, tab := range tabs {
		out = append(out, MonitorTab{
			ID:        tab.ID,
			Worktree:  tab.Worktree,
			Assistant: tab.Assistant,
			Name:      tab.Name,
			Running:   tab.Running,
		})
	}
	return out
}

// MonitorTabSnapshots returns monitor tabs with their visible screens.
func (m *Model) MonitorTabSnapshots() []MonitorTabSnapshot {
	tabs := m.monitorTabs()
	snapshots := make([]MonitorTabSnapshot, 0, len(tabs))
	for _, tab := range tabs {
		snap := MonitorTabSnapshot{
			MonitorTab: MonitorTab{
				ID:        tab.ID,
				Worktree:  tab.Worktree,
				Assistant: tab.Assistant,
				Name:      tab.Name,
				Running:   tab.Running,
			},
		}
		tab.mu.Lock()
		if tab.Terminal != nil {
			snap.Screen = tab.Terminal.VisibleScreenWithSelection()
			snap.CursorX = tab.Terminal.CursorX
			snap.CursorY = tab.Terminal.CursorY
			snap.ViewOffset = tab.Terminal.ViewOffset
			snap.Width = tab.Terminal.Width
			snap.Height = tab.Terminal.Height
		}
		tab.mu.Unlock()
		snapshots = append(snapshots, snap)
	}
	return snapshots
}

// ResizeTabs resizes the given tabs to the desired sizes.
func (m *Model) ResizeTabs(sizes []TabSize) {
	for _, size := range sizes {
		if size.Width < 1 || size.Height < 1 {
			continue
		}
		tab := m.getTabByIDGlobal(size.ID)
		if tab == nil {
			continue
		}
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.Resize(size.Width, size.Height)
		}
		if tab.Agent != nil && tab.Agent.Terminal != nil {
			_ = tab.Agent.Terminal.SetSize(uint16(size.Height), uint16(size.Width))
		}
		tab.mu.Unlock()
	}
}

func (m *Model) monitorTabs() []*Tab {
	type monitorGroup struct {
		key  string
		tabs []*Tab
	}

	groups := make([]monitorGroup, 0, len(m.tabsByWorktree))
	for wtID, worktreeTabs := range m.tabsByWorktree {
		if len(worktreeTabs) == 0 {
			continue
		}
		key := wtID
		for _, tab := range worktreeTabs {
			if tab != nil && tab.Worktree != nil {
				key = tab.Worktree.Repo + "::" + tab.Worktree.Name
				break
			}
		}
		groups = append(groups, monitorGroup{key: key, tabs: worktreeTabs})
	}

	sort.Slice(groups, func(i, j int) bool {
		return groups[i].key < groups[j].key
	})

	var tabs []*Tab
	for _, group := range groups {
		for _, tab := range group.tabs {
			if tab != nil {
				tabs = append(tabs, tab)
			}
		}
	}

	return tabs
}

func (m *Model) getTabByIDGlobal(tabID TabID) *Tab {
	for wtID := range m.tabsByWorktree {
		if tab := m.getTabByID(wtID, tabID); tab != nil {
			return tab
		}
	}
	return nil
}

// MonitorSelectedIndex returns the clamped monitor selection.
func (m *Model) MonitorSelectedIndex(count int) int {
	return m.monitor.SelectedIndex(count)
}

// SetMonitorSelectedIndex updates the monitor selection.
func (m *Model) SetMonitorSelectedIndex(index, count int) {
	m.monitor.SetSelectedIndex(index, count)
}

// MoveMonitorSelection adjusts the monitor selection based on grid movement.
func (m *Model) MoveMonitorSelection(dx, dy, cols, rows, count int) {
	m.monitor.MoveSelection(dx, dy, cols, rows, count)
}

// ResetMonitorSelection clears monitor selection state.
func (m *Model) ResetMonitorSelection() {
	m.monitor.Reset()
}

func (m *Model) renderTerminalCanvas(term *vterm.VTerm, width, height int, showCursor bool) string {
	if term == nil || width <= 0 || height <= 0 {
		return ""
	}
	if m.terminalCanvas == nil || m.terminalCanvas.Width != width || m.terminalCanvas.Height != height {
		m.terminalCanvas = compositor.NewCanvas(width, height)
	}
	return compositor.RenderTerminalWithCanvas(
		m.terminalCanvas,
		term,
		width,
		height,
		showCursor,
		compositor.HexColor(string(common.ColorForeground)),
		compositor.HexColor(string(common.ColorBackground)),
	)
}

// CloseAllTabs is deprecated - tabs now persist per-worktree
// This is kept for compatibility but does nothing
func (m *Model) CloseAllTabs() {
	// No-op: tabs now persist per-worktree and are not closed when switching
}
