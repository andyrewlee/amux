package center

import (
	"fmt"
	"strings"
	"sync"
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
	"github.com/andyrewlee/amux/internal/vterm"
)

// formatScrollPos formats the scroll position for display
func formatScrollPos(offset, total int) string {
	if total == 0 {
		return "0/0"
	}
	return fmt.Sprintf("%d/%d lines up", offset, total)
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
	Name      string
	Assistant string
	Worktree  *data.Worktree
	Agent     *appPty.Agent
	Terminal  *vterm.VTerm // Virtual terminal emulator with scrollback
	mu        sync.Mutex   // Protects Terminal
	Running   bool         // Whether the agent is actively running
	// Buffer PTY output to avoid rendering partial screen updates.
	pendingOutput  []byte
	flushScheduled bool
	// Mouse selection state
	Selection SelectionState
}

// PendingGTimeout fires when 'g' prefix times out
type PendingGTimeout struct{}

// Model is the Bubbletea model for the center pane
type Model struct {
	// State
	worktree            *data.Worktree
	tabsByWorktree      map[string][]*Tab // tabs per worktree ID
	activeTabByWorktree map[string]int    // active tab index per worktree
	focused             bool
	agentManager        *appPty.AgentManager

	// Key sequence state for vim-style gt/gT
	pendingG     bool
	pendingGTime time.Time

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

// getActiveTabIdx returns the active tab index for the current worktree
func (m *Model) getActiveTabIdx() int {
	return m.activeTabByWorktree[m.worktreeID()]
}

// setActiveTabIdx sets the active tab index for the current worktree
func (m *Model) setActiveTabIdx(idx int) {
	m.activeTabByWorktree[m.worktreeID()] = idx
}

// addTab adds a tab to the current worktree
func (m *Model) addTab(tab *Tab) {
	wtID := m.worktreeID()
	m.tabsByWorktree[wtID] = append(m.tabsByWorktree[wtID], tab)
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
	TabIndex   int
	Data       []byte
}

// PTYTick triggers a PTY read
type PTYTick struct {
	WorktreeID string
	TabIndex   int
}

// PTYFlush applies buffered PTY output for a tab.
type PTYFlush struct {
	WorktreeID string
	TabIndex   int
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
				// Clamp to terminal bounds
				if termX < 0 {
					termX = 0
				}
				if termY < 0 {
					termY = 0
				}
				termWidth := m.width - 4
				termHeight := m.height - 6
				if termWidth < 10 {
					termWidth = 80
				}
				if termHeight < 5 {
					termHeight = 24
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
		return m, nil

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

		// When we have an active agent, forward almost ALL keys to the terminal
		// Only intercept essential control keys
		if m.hasActiveAgent() {
			tab := tabs[activeIdx]
			logging.Debug("Has active agent, Agent=%v, Terminal=%v", tab.Agent != nil, tab.Agent != nil && tab.Agent.Terminal != nil)
			if tab.Agent != nil && tab.Agent.Terminal != nil {
				// Handle pending 'g' key sequence for vim-style gt/gT
				if m.pendingG {
					m.pendingG = false
					if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
						switch msg.Runes[0] {
						case 't':
							m.nextTab()
							return m, nil
						case 'T':
							m.prevTab()
							return m, nil
						default:
							// Forward both 'g' and current key to terminal
							tab.Agent.Terminal.SendString("g")
							// Fall through to normal key handling
						}
					} else {
						// Non-rune key after 'g' - forward both
						tab.Agent.Terminal.SendString("g")
						// Fall through to normal key handling
					}
				}

				// Start 'g' sequence
				if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == 'g' {
					m.pendingG = true
					m.pendingGTime = time.Now()
					return m, tea.Tick(time.Second, func(t time.Time) tea.Msg {
						return PendingGTimeout{}
					})
				}

				// Only intercept these specific keys - everything else goes to terminal
				switch {
				case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+w"))):
					// Close tab
					return m, m.closeCurrentTab()

				case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+]"))):
					// Switch to next tab (escape hatch that won't conflict)
					m.nextTab()
					return m, nil

				case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+["))):
					// This is Escape - let it go to terminal
					tab.Agent.Terminal.SendString("\x1b")
					return m, nil

				case msg.Type == tea.KeyPgUp:
					// Scroll up in scrollback
					tab.mu.Lock()
					if tab.Terminal != nil {
						tab.Terminal.ScrollView(tab.Terminal.Height / 2)
					}
					tab.mu.Unlock()
					return m, nil

				case msg.Type == tea.KeyPgDown:
					// Scroll down in scrollback
					tab.mu.Lock()
					if tab.Terminal != nil {
						tab.Terminal.ScrollView(-tab.Terminal.Height / 2)
					}
					tab.mu.Unlock()
					return m, nil

				case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+u"))):
					// Scroll up half page (vim style)
					tab.mu.Lock()
					if tab.Terminal != nil {
						tab.Terminal.ScrollView(tab.Terminal.Height / 2)
					}
					tab.mu.Unlock()
					return m, nil

				case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+d"))):
					// Scroll down half page (vim style)
					tab.mu.Lock()
					if tab.Terminal != nil {
						tab.Terminal.ScrollView(-tab.Terminal.Height / 2)
					}
					tab.mu.Unlock()
					return m, nil

				case key.Matches(msg, key.NewBinding(key.WithKeys("home"))):
					// Scroll to top
					tab.mu.Lock()
					if tab.Terminal != nil {
						tab.Terminal.ScrollViewToTop()
					}
					tab.mu.Unlock()
					return m, nil

				case key.Matches(msg, key.NewBinding(key.WithKeys("end"))):
					// Scroll to bottom (live)
					tab.mu.Lock()
					if tab.Terminal != nil {
						tab.Terminal.ScrollViewToBottom()
					}
					tab.mu.Unlock()
					return m, nil

				default:
					// If scrolled, any typing goes back to live and sends key
					tab.mu.Lock()
					if tab.Terminal != nil && tab.Terminal.IsScrolled() {
						tab.Terminal.ScrollViewToBottom()
					}
					tab.mu.Unlock()

					// EVERYTHING else goes to terminal
					input := keyToBytes(msg)
					if len(input) > 0 {
						logging.Debug("Sending to terminal: %q (len=%d)", input, len(input))
						tab.Agent.Terminal.SendString(string(input))
					} else {
						logging.Debug("keyToBytes returned empty for: %s", msg.String())
					}
					return m, nil
				}
			}
		}

		// No active agent - handle tab management keys
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+w"))):
			return m, m.closeCurrentTab()
		}

	case messages.LaunchAgent:
		return m, m.createAgentTab(msg.Assistant, msg.Worktree)

	case PTYOutput:
		wtTabs := m.tabsByWorktree[msg.WorktreeID]
		if msg.TabIndex >= 0 && msg.TabIndex < len(wtTabs) {
			tab := wtTabs[msg.TabIndex]
			tab.pendingOutput = append(tab.pendingOutput, msg.Data...)
			if !tab.flushScheduled {
				tab.flushScheduled = true
				cmds = append(cmds, tea.Tick(16*time.Millisecond, func(t time.Time) tea.Msg {
					return PTYFlush{WorktreeID: msg.WorktreeID, TabIndex: msg.TabIndex}
				}))
			}
		}
		// Continue reading
		if msg.TabIndex < len(wtTabs) {
			cmds = append(cmds, m.readPTYForWorktree(msg.WorktreeID, msg.TabIndex))
		}

	case PTYFlush:
		wtTabs := m.tabsByWorktree[msg.WorktreeID]
		if msg.TabIndex >= 0 && msg.TabIndex < len(wtTabs) {
			tab := wtTabs[msg.TabIndex]
			tab.flushScheduled = false
			if len(tab.pendingOutput) > 0 {
				tab.mu.Lock()
				if tab.Terminal != nil {
					tab.Terminal.Write(tab.pendingOutput)
				}
				tab.mu.Unlock()
				tab.pendingOutput = tab.pendingOutput[:0]
			}
		}

	case PTYTick:
		wtTabs := m.tabsByWorktree[msg.WorktreeID]
		if msg.TabIndex < len(wtTabs) {
			cmds = append(cmds, m.readPTYForWorktree(msg.WorktreeID, msg.TabIndex))
		}

	case PendingGTimeout:
		// If still pending after timeout, forward 'g' to terminal
		if m.pendingG && time.Since(m.pendingGTime) >= time.Second {
			m.pendingG = false
			tabs := m.getTabs()
			activeIdx := m.getActiveTabIdx()
			if len(tabs) > 0 && activeIdx < len(tabs) {
				tab := tabs[activeIdx]
				if tab.Agent != nil && tab.Agent.Terminal != nil {
					tab.Agent.Terminal.SendString("g")
				}
			}
		}
	}

	return m, tea.Batch(cmds...)
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
		return []byte{0x1b, '[', 'Z'} // Back tab / reverse tab
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
	// Note: KeyCtrlI is same as Tab, KeyCtrlM is same as Enter - handled above
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
			b.WriteString(tab.Terminal.Render())

			// Show scroll indicator if scrolled
			if tab.Terminal.IsScrolled() {
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

	// Help bar with styled keys (vim-friendly)
	helpItems := []string{
		m.styles.HelpKey.Render("esc") + m.styles.HelpDesc.Render(":dashboard"),
		m.styles.HelpKey.Render("^w") + m.styles.HelpDesc.Render(":close"),
		m.styles.HelpKey.Render("gt/gT") + m.styles.HelpDesc.Render(":tabs"),
		m.styles.HelpKey.Render("^u/d") + m.styles.HelpDesc.Render(":scroll"),
		m.styles.HelpKey.Render("^c") + m.styles.HelpDesc.Render(":interrupt"),
	}
	help := strings.Join(helpItems, "  ")
	contentHeight := strings.Count(b.String(), "\n") + 2
	padding := m.height - contentHeight - 5
	if padding > 0 {
		b.WriteString(strings.Repeat("\n", padding))
	}
	b.WriteString("\n" + help)

	// Apply pane styling
	style := m.styles.Pane
	if m.focused {
		style = m.styles.FocusedPane
	}

	return style.Width(m.width - 2).Height(m.height - 2).Render(b.String())
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
		name := tab.Assistant
		if name == "" {
			name = tab.Name
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

	// Add the plus button styled to match the tab bar line
	plusBtn := lipgloss.NewStyle().
		Padding(0, 1).
		Foreground(common.ColorMuted).
		Border(lipgloss.Border{Bottom: "─"}, false, false, true, false).
		BorderForeground(common.ColorBorder).
		Render("[+]")
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
	b.WriteString(m.styles.HelpKey.Render("Ctrl+T"))
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
				agent.Terminal.SendString(string(data))
			})
		}

		// Create tab
		tab := &Tab{
			Name:      assistant,
			Assistant: assistant,
			Worktree:  wt,
			Agent:     agent,
			Terminal:  term,
			Running:   true, // Agent starts running
		}

		// Set PTY size to match
		if agent.Terminal != nil {
			agent.Terminal.SetSize(uint16(termHeight), uint16(termWidth))
			logging.Info("Terminal size set to %dx%d", termWidth, termHeight)
		}

		// Add tab to the worktree's tab list
		wtID := string(wt.ID())
		m.tabsByWorktree[wtID] = append(m.tabsByWorktree[wtID], tab)
		m.activeTabByWorktree[wtID] = len(m.tabsByWorktree[wtID]) - 1

		return messages.TabCreated{Index: m.activeTabByWorktree[wtID], Name: assistant}
	}
}

// readPTY reads from the PTY for a tab in the current worktree
func (m *Model) readPTY(tabIndex int) tea.Cmd {
	return m.readPTYForWorktree(m.worktreeID(), tabIndex)
}

// readPTYForWorktree reads from the PTY for a tab in a specific worktree
func (m *Model) readPTYForWorktree(wtID string, tabIndex int) tea.Cmd {
	wtTabs := m.tabsByWorktree[wtID]
	if tabIndex >= len(wtTabs) {
		return nil
	}

	tab := wtTabs[tabIndex]
	if tab.Agent == nil || tab.Agent.Terminal == nil {
		return nil
	}

	return func() tea.Msg {
		buf := make([]byte, 4096)
		n, err := tab.Agent.Terminal.Read(buf)
		if err != nil {
			// PTY closed, schedule retry after delay
			time.Sleep(100 * time.Millisecond)
			return PTYTick{WorktreeID: wtID, TabIndex: tabIndex}
		}
		if n > 0 {
			return PTYOutput{WorktreeID: wtID, TabIndex: tabIndex, Data: buf[:n]}
		}
		return PTYTick{WorktreeID: wtID, TabIndex: tabIndex}
	}
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
		m.agentManager.CloseAgent(tab.Agent)
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

// sendInterrupt sends an interrupt to the active agent
func (m *Model) sendInterrupt() tea.Cmd {
	if !m.hasActiveAgent() {
		return nil
	}

	tabs := m.getTabs()
	tab := tabs[m.getActiveTabIdx()]
	if tab.Agent != nil {
		m.agentManager.SendInterrupt(tab.Agent)
	}

	return nil
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
				tab.Agent.Terminal.SetSize(uint16(termHeight), uint16(termWidth))
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
	// - Tab bar: 1 line at top (after border)
	borderTop := 1
	borderLeft := 1
	paddingLeft := 1
	tabBarHeight := 1

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
		for i := range tabs {
			cmds = append(cmds, m.readPTYForWorktree(wtID, i))
		}
	}
	return tea.Batch(cmds...)
}

// Close cleans up all resources
func (m *Model) Close() {
	m.agentManager.CloseAll()
}

// SendText sends text to the active agent's terminal
func (m *Model) SendText(text string) {
	if !m.hasActiveAgent() {
		return
	}

	tabs := m.getTabs()
	tab := tabs[m.getActiveTabIdx()]
	if tab.Agent != nil && tab.Agent.Terminal != nil {
		// Use bracketed paste mode for multi-line text
		// This prevents newlines from being interpreted as command execution
		if strings.Contains(text, "\n") {
			// Start bracketed paste: \e[200~
			// End bracketed paste: \e[201~
			bracketedText := "\x1b[200~" + text + "\x1b[201~"
			tab.Agent.Terminal.SendString(bracketedText)
		} else {
			tab.Agent.Terminal.SendString(text)
		}
		// Send enter to submit
		tab.Agent.Terminal.SendString("\r")
	}
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

// CloseAllTabs is deprecated - tabs now persist per-worktree
// This is kept for compatibility but does nothing
func (m *Model) CloseAllTabs() {
	// No-op: tabs now persist per-worktree and are not closed when switching
}
