package center

import (
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/vt"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// Tab represents a single tab in the center pane
type Tab struct {
	Name      string
	Assistant string
	Worktree  *data.Worktree
	Agent     *appPty.Agent
	Terminal  *vt.Emulator // Virtual terminal emulator
	mu        sync.Mutex   // Protects Terminal
	Running   bool         // Whether the agent is actively running
}

// Model is the Bubbletea model for the center pane
type Model struct {
	// State
	worktree     *data.Worktree
	tabs         []*Tab
	activeTab    int
	focused      bool
	agentManager *appPty.AgentManager

	// Layout
	width  int
	height int

	// Config
	config *config.Config
	styles common.Styles
}

// New creates a new center pane model
func New(cfg *config.Config) *Model {
	return &Model{
		tabs:         []*Tab{},
		config:       cfg,
		agentManager: appPty.NewAgentManager(cfg),
		styles:       common.DefaultStyles(),
	}
}

// Init initializes the center pane
func (m *Model) Init() tea.Cmd {
	return nil
}

// PTYOutput is a message containing PTY output data
type PTYOutput struct {
	TabIndex int
	Data     []byte
}

// PTYTick triggers a PTY read
type PTYTick struct {
	TabIndex int
}

// Update handles messages
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		logging.Debug("Center received key: %s, focused=%v, hasTabs=%v, numTabs=%d",
			msg.String(), m.focused, m.hasActiveAgent(), len(m.tabs))

		if !m.focused {
			logging.Debug("Center not focused, ignoring key")
			return m, nil
		}

		// When we have an active agent, forward almost ALL keys to the terminal
		// Only intercept essential control keys
		if m.hasActiveAgent() {
			tab := m.tabs[m.activeTab]
			logging.Debug("Has active agent, Agent=%v, Terminal=%v", tab.Agent != nil, tab.Agent != nil && tab.Agent.Terminal != nil)
			if tab.Agent != nil && tab.Agent.Terminal != nil {
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

				default:
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
		if msg.TabIndex >= 0 && msg.TabIndex < len(m.tabs) {
			tab := m.tabs[msg.TabIndex]
			tab.mu.Lock()
			if tab.Terminal != nil {
				tab.Terminal.Write(msg.Data)
			}
			tab.mu.Unlock()
		}
		// Continue reading
		if msg.TabIndex < len(m.tabs) {
			cmds = append(cmds, m.readPTY(msg.TabIndex))
		}

	case PTYTick:
		if msg.TabIndex < len(m.tabs) {
			cmds = append(cmds, m.readPTY(msg.TabIndex))
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
	if len(m.tabs) == 0 {
		b.WriteString(m.renderEmpty())
	} else if m.activeTab < len(m.tabs) {
		tab := m.tabs[m.activeTab]
		tab.mu.Lock()
		if tab.Terminal != nil {
			b.WriteString(tab.Terminal.Render())
		}
		tab.mu.Unlock()
	}

	// Help bar with styled keys
	helpItems := []string{
		m.styles.HelpKey.Render("esc") + m.styles.HelpDesc.Render(":dashboard"),
		m.styles.HelpKey.Render("ctrl+w") + m.styles.HelpDesc.Render(":close"),
		m.styles.HelpKey.Render("ctrl+]") + m.styles.HelpDesc.Render(":next tab"),
		m.styles.HelpKey.Render("ctrl+c") + m.styles.HelpDesc.Render(":interrupt"),
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
	if len(m.tabs) == 0 {
		return m.styles.TabBar.Render(m.styles.Muted.Render("No agents"))
	}

	var tabs []string
	for i, tab := range m.tabs {
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
		default:
			agentStyle = m.styles.AgentTerm
		}

		// Build tab content
		content := indicator + name

		if i == m.activeTab {
			// Active tab gets full highlight
			tabs = append(tabs, m.styles.ActiveTab.Render(content))
		} else {
			// Inactive tab with agent color for indicator
			tabs = append(tabs, m.styles.Tab.Render(agentStyle.Render(indicator)+name))
		}
	}

	// Add the plus button for new tab
	plusBtn := m.styles.TabPlus.Render("[+]")

	return m.styles.TabBar.Render(strings.Join(tabs, " ") + " " + plusBtn)
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

		// Create virtual terminal emulator
		term := vt.NewEmulator(termWidth, termHeight)

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

		m.tabs = append(m.tabs, tab)
		m.activeTab = len(m.tabs) - 1
		logging.Info("Tab added, numTabs=%d, activeTab=%d", len(m.tabs), m.activeTab)

		return messages.TabCreated{Index: m.activeTab, Name: assistant}
	}
}

// readPTY reads from the PTY for a tab
func (m *Model) readPTY(tabIndex int) tea.Cmd {
	if tabIndex >= len(m.tabs) {
		return nil
	}

	tab := m.tabs[tabIndex]
	if tab.Agent == nil || tab.Agent.Terminal == nil {
		return nil
	}

	return func() tea.Msg {
		buf := make([]byte, 4096)
		n, err := tab.Agent.Terminal.Read(buf)
		if err != nil {
			// PTY closed, schedule retry after delay
			time.Sleep(100 * time.Millisecond)
			return PTYTick{TabIndex: tabIndex}
		}
		if n > 0 {
			return PTYOutput{TabIndex: tabIndex, Data: buf[:n]}
		}
		return PTYTick{TabIndex: tabIndex}
	}
}

// closeCurrentTab closes the current tab
func (m *Model) closeCurrentTab() tea.Cmd {
	if len(m.tabs) == 0 || m.activeTab >= len(m.tabs) {
		return nil
	}

	tab := m.tabs[m.activeTab]
	index := m.activeTab

	// Close agent
	if tab.Agent != nil {
		m.agentManager.CloseAgent(tab.Agent)
	}

	// Remove from tabs
	m.tabs = append(m.tabs[:index], m.tabs[index+1:]...)

	// Adjust active tab
	if m.activeTab >= len(m.tabs) && m.activeTab > 0 {
		m.activeTab--
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

	tab := m.tabs[m.activeTab]
	if tab.Agent != nil {
		m.agentManager.SendInterrupt(tab.Agent)
	}

	return nil
}

// hasActiveAgent returns whether there's an active agent
func (m *Model) hasActiveAgent() bool {
	return len(m.tabs) > 0 && m.activeTab < len(m.tabs)
}

// nextTab switches to the next tab
func (m *Model) nextTab() {
	if len(m.tabs) > 0 {
		m.activeTab = (m.activeTab + 1) % len(m.tabs)
	}
}

// prevTab switches to the previous tab
func (m *Model) prevTab() {
	if len(m.tabs) > 0 {
		m.activeTab--
		if m.activeTab < 0 {
			m.activeTab = len(m.tabs) - 1
		}
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

	// Update all terminals
	for _, tab := range m.tabs {
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

// HasTabs returns whether there are any tabs
func (m *Model) HasTabs() bool {
	return len(m.tabs) > 0
}

// StartPTYReaders starts reading from all PTYs
func (m *Model) StartPTYReaders() tea.Cmd {
	var cmds []tea.Cmd
	for i := range m.tabs {
		cmds = append(cmds, m.readPTY(i))
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

	tab := m.tabs[m.activeTab]
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
	var tabs []data.TabInfo
	for _, tab := range m.tabs {
		tabs = append(tabs, data.TabInfo{
			Assistant: tab.Assistant,
			Name:      tab.Name,
		})
	}
	return tabs, m.activeTab
}

// CloseAllTabs closes all tabs and cleans up agents
func (m *Model) CloseAllTabs() {
	for _, tab := range m.tabs {
		if tab.Agent != nil {
			m.agentManager.CloseAgent(tab.Agent)
		}
	}
	m.tabs = []*Tab{}
	m.activeTab = 0
}
