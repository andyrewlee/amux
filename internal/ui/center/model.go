package center

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/ui/commits"
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

const ptyTraceLimit = 256 * 1024

func ptyTraceAllowed(assistant string) bool {
	value := strings.TrimSpace(os.Getenv("AMUX_PTY_TRACE"))
	if value == "" {
		return false
	}

	switch strings.ToLower(value) {
	case "0", "false", "no":
		return false
	case "1", "true", "yes", "all", "*":
		return true
	}

	target := strings.ToLower(strings.TrimSpace(assistant))
	if target == "" {
		return false
	}

	for _, part := range strings.Split(value, ",") {
		if strings.ToLower(strings.TrimSpace(part)) == target {
			return true
		}
	}

	return false
}

func ptyTraceDir() string {
	logPath := logging.GetLogPath()
	if logPath != "" {
		return filepath.Dir(logPath)
	}
	return os.TempDir()
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
	Terminal     *vterm.VTerm   // Virtual terminal emulator with scrollback
	CommitViewer *commits.Model // Commit viewer component (if this is a commit viewer tab)
	mu           sync.Mutex     // Protects Terminal
	Running      bool           // Whether the agent is actively running
	readerActive bool           // Guard to ensure only one PTY read loop per tab
	CopyMode     bool           // Whether the tab is in copy/scroll mode (keys not sent to PTY)
	// Buffer PTY output to avoid rendering partial screen updates.

	pendingOutput     []byte
	flushScheduled    bool
	lastOutputAt      time.Time
	flushPendingSince time.Time
	// Mouse selection state
	Selection SelectionState

	ptyTraceFile   *os.File
	ptyTraceBytes  int
	ptyTraceClosed bool

	// Snapshot cache for VTermLayer - avoid recreating snapshot when terminal unchanged
	cachedSnap       *compositor.VTermSnapshot
	cachedVersion    uint64
	cachedShowCursor bool
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
	SelActive  bool
	SelStartX  int
	SelStartY  int
	SelEndX    int
	SelEndY    int
}

// HandleMonitorInput forwards input to a specific tab while in monitor view.
func (m *Model) HandleMonitorInput(tabID TabID, msg tea.Msg) tea.Cmd {
	tab := m.getTabByIDGlobal(tabID)
	if tab == nil || tab.Agent == nil || tab.Agent.Terminal == nil {
		return nil
	}

	switch msg := msg.(type) {
	case tea.PasteMsg:
		// Handle bracketed paste - send entire content at once with escape sequences.
		bracketedText := "\x1b[200~" + msg.Content + "\x1b[201~"
		_ = tab.Agent.Terminal.SendString(bracketedText)
		return nil

	case tea.KeyPressMsg:
		switch {
		case msg.Key().Code == tea.KeyPgUp:
			tab.mu.Lock()
			if tab.Terminal != nil {
				tab.Terminal.ScrollView(tab.Terminal.Height / 4)
			}
			tab.mu.Unlock()
			return nil

		case msg.Key().Code == tea.KeyPgDown:
			tab.mu.Lock()
			if tab.Terminal != nil {
				tab.Terminal.ScrollView(-tab.Terminal.Height / 4)
			}
			tab.mu.Unlock()
			return nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+u"))):
			tab.mu.Lock()
			if tab.Terminal != nil {
				tab.Terminal.ScrollView(tab.Terminal.Height / 4)
			}
			tab.mu.Unlock()
			return nil

		case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+d"))):
			tab.mu.Lock()
			if tab.Terminal != nil {
				tab.Terminal.ScrollView(-tab.Terminal.Height / 4)
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

	// Backpressure thresholds (inspired by tmux's TTY_BLOCK_START/STOP)
	// When pending output exceeds this, we throttle rendering frequency
	ptyBackpressureMultiplier = 8 // threshold = multiplier * width * height
	ptyBackpressureFlushMin   = 32 * time.Millisecond
)

// Model is the Bubbletea model for the center pane
type Model struct {
	// State
	worktree            *data.Worktree
	tabsByWorktree      map[string][]*Tab // tabs per worktree ID
	activeTabByWorktree map[string]int    // active tab index per worktree
	focused             bool
	canFocusRight       bool
	agentManager        *appPty.AgentManager
	monitor             MonitorModel

	// Dialog state
	saveDialog      *common.Dialog
	savedThreadPath string
	dialogOpenTime  time.Time

	// Layout
	width           int
	height          int
	offsetX         int // X offset from screen left (dashboard width)
	showKeymapHints bool

	// Config
	config  *config.Config
	styles  common.Styles
	tabHits []tabHit
}

type tabHitKind int

const (
	tabHitTab tabHitKind = iota
	tabHitClose
	tabHitPlus
	tabHitPrev
	tabHitNext
	tabHitSave
)

type tabHit struct {
	kind   tabHitKind
	index  int
	region common.HitRegion
}

func (m *Model) paneWidth() int {
	if m.width < 1 {
		return 1
	}
	return m.width
}

func (m *Model) contentWidth() int {
	frameX, _ := m.styles.Pane.GetFrameSize()
	width := m.paneWidth() - frameX
	if width < 1 {
		return 1
	}
	return width
}

// TerminalMetrics holds the computed geometry for the terminal content area.
// This is the single source of truth for terminal positioning and sizing.
type TerminalMetrics struct {
	// For mouse hit-testing (screen coordinates to terminal coordinates)
	ContentStartX int // X offset from pane left edge (border + padding)
	ContentStartY int // Y offset from pane top edge (border + tab bar)

	// Terminal dimensions
	Width  int // Terminal width in columns
	Height int // Terminal height in rows
}

// terminalMetrics computes the terminal content area geometry.
// It preserves the original layout constants while accounting for dynamic help lines.
func (m *Model) terminalMetrics() TerminalMetrics {
	// These values match the original working implementation
	const (
		borderLeft   = 1
		paddingLeft  = 1
		borderTop    = 1
		tabBarHeight = 3 // tab border + content (tabs render as 3 lines)
		baseOverhead = 6 // borders + tab bar + status line reserve
	)

	width := m.contentWidth()
	if width < 1 {
		width = 1
	}
	if width < 10 {
		width = 80
	}
	helpLineCount := 0
	if m.showKeymapHints {
		helpLineCount = len(m.helpLines(width))
	}
	height := m.height - baseOverhead - helpLineCount
	if height < 5 {
		height = 24
	}

	return TerminalMetrics{
		ContentStartX: borderLeft + paddingLeft,
		ContentStartY: borderTop + tabBarHeight,
		Width:         width,
		Height:        height,
	}
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

// SetCanFocusRight controls whether focus-right hints should be shown.
func (m *Model) SetCanFocusRight(can bool) {
	m.canFocusRight = can
}

// SetShowKeymapHints controls whether helper text is rendered.
func (m *Model) SetShowKeymapHints(show bool) {
	m.showKeymapHints = show
}

// SetStyles updates the component's styles (for theme changes).
func (m *Model) SetStyles(styles common.Styles) {
	m.styles = styles
	// Propagate to all commit viewers in tabs
	for _, tabs := range m.tabsByWorktree {
		for _, tab := range tabs {
			if tab != nil && tab.CommitViewer != nil {
				tab.CommitViewer.SetStyles(styles)
			}
		}
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
	// Only use slower Alt timing for true AltScreen mode (full-screen TUIs).
	// SyncActive (DEC 2026) already handles partial updates via screen snapshots,
	// so we don't need slower flush timing - it just makes streaming text feel laggy.
	if tab.Terminal != nil && tab.Terminal.AltScreen {
		quiet = ptyFlushQuietAlt
		maxInterval = ptyFlushMaxAlt
	}

	// Apply backpressure when pending output exceeds threshold
	// This prevents renderer thrashing during heavy output (like builds)
	if tab.Terminal != nil && len(tab.pendingOutput) > 0 {
		threshold := ptyBackpressureMultiplier * tab.Terminal.Width * tab.Terminal.Height
		if len(tab.pendingOutput) > threshold {
			// Under backpressure: use minimum flush interval
			if quiet < ptyBackpressureFlushMin {
				quiet = ptyBackpressureFlushMin
			}
			if maxInterval < ptyBackpressureFlushMin {
				maxInterval = ptyBackpressureFlushMin
			}
		}
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
	defer perf.Time("center_update")()
	var cmds []tea.Cmd

	// Handle dialog update if visible, but only for interactive messages.
	// PTY messages must still be processed to keep the terminal running.
	if m.saveDialog != nil && m.saveDialog.Visible() {
		switch typedMsg := msg.(type) {
		case tea.KeyPressMsg:
			// Debounce input to prevent accidental double-confirms (e.g. holding Enter)
			if time.Since(m.dialogOpenTime) < 500*time.Millisecond {
				return m, nil
			}
			var cmd tea.Cmd
			m.saveDialog, cmd = m.saveDialog.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		case tea.MouseClickMsg:
			// Translate screen coordinates to center pane local coordinates for dialog
			localMsg := tea.MouseClickMsg{
				X:      typedMsg.X - m.offsetX,
				Y:      typedMsg.Y,
				Button: typedMsg.Button,
			}
			var cmd tea.Cmd
			m.saveDialog, cmd = m.saveDialog.Update(localMsg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		case tea.MouseWheelMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg, tea.PasteMsg:
			var cmd tea.Cmd
			m.saveDialog, cmd = m.saveDialog.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}
		// Fall through for PTY messages, window size, etc.
	}

	switch msg := msg.(type) {
	case common.DialogResult:
		if !msg.Confirmed {
			m.saveDialog = nil
			return m, nil
		}

		switch msg.ID {
		case "save-thread":
			// Index 0 is "Save & Copy Path"
			if msg.Index == 0 {
				path, err := m.exportActiveThread()
				if err != nil {
					m.saveDialog = nil
					return m, func() tea.Msg { return messages.ThreadExportFailed{Err: err} }
				}
				if err := copyToClipboard(path); err != nil {
					logging.Error("Failed to copy path: %v", err)
				}
				m.savedThreadPath = path
				m.saveDialog = nil
				return m, func() tea.Msg { return messages.ThreadExported{Path: path, Copied: true} }
			}
			// Cancel
			m.saveDialog = nil
		}
		return m, nil
	case tea.MouseClickMsg:
		// Handle tab bar clicks (e.g., the plus button) even without an active agent.
		if msg.Button == tea.MouseLeft {
			if cmd := m.handleTabBarClick(msg); cmd != nil {
				return m, cmd
			}
		}

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

		// CommitViewer tabs: forward mouse events to commit viewer
		tab.mu.Lock()
		cv := tab.CommitViewer
		tab.mu.Unlock()
		if cv != nil {
			newCV, cmd := cv.Update(msg)
			tab.mu.Lock()
			tab.CommitViewer = newCV
			tab.mu.Unlock()
			return m, cmd
		}

		if msg.Button != tea.MouseLeft {
			return m, nil
		}

		// Convert screen coordinates to terminal coordinates
		termX, termY, inBounds := m.screenToTerminal(msg.X, msg.Y)

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
		return m, tea.Batch(cmds...)

	case tea.MouseMotionMsg:
		// Handle mouse drag events for text selection
		if !m.focused || !m.hasActiveAgent() {
			return m, nil
		}

		if msg.Button != tea.MouseLeft {
			return m, nil
		}

		tabs := m.getTabs()
		activeIdx := m.getActiveTabIdx()
		if activeIdx >= len(tabs) {
			return m, nil
		}
		tab := tabs[activeIdx]

		// CommitViewer tabs: forward mouse events to commit viewer
		tab.mu.Lock()
		cv := tab.CommitViewer
		tab.mu.Unlock()
		if cv != nil {
			newCV, cmd := cv.Update(msg)
			tab.mu.Lock()
			tab.CommitViewer = newCV
			tab.mu.Unlock()
			return m, cmd
		}

		termX, termY, _ := m.screenToTerminal(msg.X, msg.Y)

		// Update selection while dragging
		tab.mu.Lock()
		if tab.Selection.Active {
			termWidth := m.contentWidth()
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
		return m, tea.Batch(cmds...)

	case tea.MouseReleaseMsg:
		// Handle mouse release events for text selection
		if !m.focused || !m.hasActiveAgent() {
			return m, nil
		}

		if msg.Button != tea.MouseLeft {
			return m, nil
		}

		tabs := m.getTabs()
		activeIdx := m.getActiveTabIdx()
		if activeIdx >= len(tabs) {
			return m, nil
		}
		tab := tabs[activeIdx]

		// CommitViewer tabs: forward mouse events to commit viewer
		tab.mu.Lock()
		cv := tab.CommitViewer
		tab.mu.Unlock()
		if cv != nil {
			newCV, cmd := cv.Update(msg)
			tab.mu.Lock()
			tab.CommitViewer = newCV
			tab.mu.Unlock()
			return m, cmd
		}

		tab.mu.Lock()
		if tab.Selection.Active {
			// Extract selected text and copy to clipboard
			if tab.Terminal != nil {
				text := tab.Terminal.GetSelectedText(
					tab.Selection.StartX, tab.Selection.StartY,
					tab.Selection.EndX, tab.Selection.EndY,
				)
				if text != "" {
					if err := copyToClipboard(text); err != nil {
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
		return m, tea.Batch(cmds...)

	case tea.MouseWheelMsg:
		if !m.focused || !m.hasActiveAgent() {
			return m, nil
		}

		tabs := m.getTabs()
		activeIdx := m.getActiveTabIdx()
		if activeIdx >= len(tabs) {
			return m, nil
		}
		tab := tabs[activeIdx]

		// CommitViewer tabs: forward mouse events to commit viewer
		tab.mu.Lock()
		cv := tab.CommitViewer
		tab.mu.Unlock()
		if cv != nil {
			newCV, cmd := cv.Update(msg)
			tab.mu.Lock()
			tab.CommitViewer = newCV
			tab.mu.Unlock()
			return m, cmd
		}

		return m, nil

	case tea.PasteMsg:
		tabs := m.getTabs()
		activeIdx := m.getActiveTabIdx()
		if len(tabs) > 0 && activeIdx < len(tabs) {
			tab := tabs[activeIdx]
			if !m.focused {
				return m, nil
			}
			if tab.Agent != nil && tab.Agent.Terminal != nil {
				bracketedText := "\x1b[200~" + msg.Content + "\x1b[201~"
				_ = tab.Agent.Terminal.SendString(bracketedText)
				logging.Debug("Pasted %d bytes via bracketed paste", len(msg.Content))
				return m, nil
			}
		}
		return m, nil

	case tea.KeyPressMsg:
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

			// CommitViewer tabs: forward keys to commit viewer
			tab.mu.Lock()
			cv := tab.CommitViewer
			tab.mu.Unlock()
			if cv != nil {
				// Handle ctrl+w for closing tab
				if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+w"))) {
					return m, m.closeCurrentTab()
				}
				// Handle ctrl+n/p for tab switching
				if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+n"))) {
					m.nextTab()
					return m, nil
				}
				if key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+p"))) {
					m.prevTab()
					return m, nil
				}
				// Forward all other keys to commit viewer
				newCV, cmd := cv.Update(msg)
				tab.mu.Lock()
				tab.CommitViewer = newCV
				tab.mu.Unlock()
				return m, cmd
			}

			// Copy mode: handle scroll navigation without sending to PTY
			if tab.CopyMode {
				return m, m.handleCopyModeKey(tab, msg)
			}

			if tab.Agent != nil && tab.Agent.Terminal != nil {
				// Only intercept these specific keys - everything else goes to terminal
				switch {
				case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+n"))):
					m.nextTab()
					return m, nil

				case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+p"))):
					m.prevTab()
					return m, nil

				case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+w"))):
					// Close tab
					return m, m.closeCurrentTab()

				case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+]"))):
					// Switch to next tab (escape hatch that won't conflict)
					m.nextTab()
					return m, nil

				case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+["))):
					// This is Escape - let it go to terminal
					_ = tab.Agent.Terminal.SendString("\x1b")
					return m, nil
				}

				// PgUp/PgDown for scrollback (these don't conflict with embedded TUIs)
				switch msg.Key().Code {
				case tea.KeyPgUp:
					tab.mu.Lock()
					if tab.Terminal != nil {
						tab.Terminal.ScrollView(tab.Terminal.Height / 4)
					}
					tab.mu.Unlock()
					return m, nil

				case tea.KeyPgDown:
					tab.mu.Lock()
					if tab.Terminal != nil {
						tab.Terminal.ScrollView(-tab.Terminal.Height / 4)
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

	case messages.SaveThreadRequest:
		m.saveDialog = common.NewSelectDialog(
			"save-thread",
			"Save Thread",
			"Save current thread to file?",
			[]string{"Save & Copy Path", "Cancel"},
		)
		m.saveDialog.Show()
		m.saveDialog.SetSize(m.width, m.height)
		m.dialogOpenTime = time.Now()
		return m, nil

	case messages.OpenDiff:
		return m, m.createViewerTab(msg.File, msg.StatusCode, msg.Worktree)

	case messages.OpenCommitViewer:
		return m, m.createCommitViewerTab(msg.Worktree)

	case messages.ViewCommitDiff:
		return m, m.createCommitDiffTab(msg.Hash, msg.Worktree)

	case PTYOutput:
		tab := m.getTabByID(msg.WorktreeID, msg.TabID)
		if tab != nil {
			tab.pendingOutput = append(tab.pendingOutput, msg.Data...)
			perf.Count("pty_output_bytes", int64(len(msg.Data)))
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
					flushDone := perf.Time("pty_flush")
					tab.Terminal.Write(tab.pendingOutput[:chunkSize])
					flushDone()
					perf.Count("pty_flush_bytes", int64(chunkSize))
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

	default:
		// Forward unknown messages to active commit viewer if one exists
		tabs := m.getTabs()
		activeIdx := m.getActiveTabIdx()
		if len(tabs) > 0 && activeIdx < len(tabs) {
			tab := tabs[activeIdx]
			tab.mu.Lock()
			cv := tab.CommitViewer
			tab.mu.Unlock()
			if cv != nil {
				newCV, cmd := cv.Update(msg)
				tab.mu.Lock()
				tab.CommitViewer = newCV
				tab.mu.Unlock()
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
	}

	return m, tea.Batch(cmds...)
}

// View renders the center pane
func (m *Model) View() string {
	defer perf.Time("center_view")()
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
		if tab.CommitViewer != nil {
			// Sync focus state with center pane focus
			tab.CommitViewer.SetFocused(m.focused)
			// Render commit viewer
			b.WriteString(tab.CommitViewer.View())
		} else if tab.Terminal != nil {
			tab.Terminal.ShowCursor = m.focused
			// Use VTerm.Render() directly - it uses dirty line caching and delta styles
			b.WriteString(tab.Terminal.Render())

			if status := m.terminalStatusLineLocked(tab); status != "" {
				b.WriteString("\n" + status)
			}
		}
		tab.mu.Unlock()
	}

	// Help bar with styled keys (prefix mode)
	contentWidth := m.contentWidth()
	if contentWidth < 1 {
		contentWidth = 1
	}
	helpLines := m.helpLines(contentWidth)
	if !m.showKeymapHints {
		helpLines = nil
	}
	// Pad to the inner pane height (border excluded), reserving the help lines.
	// buildBorderedPane will use contentHeight = height - 2, so we target that.
	innerHeight := m.height - 2
	if innerHeight < 0 {
		innerHeight = 0
	}

	// Build content with help at bottom
	content := b.String()
	helpContent := strings.Join(helpLines, "\n")

	// Count current lines
	contentLines := strings.Split(content, "\n")
	helpLineCount := len(helpLines)

	// Calculate padding needed
	targetContentLines := innerHeight - helpLineCount
	if targetContentLines < 0 {
		targetContentLines = 0
	}

	// Pad or truncate content to targetContentLines
	if len(contentLines) < targetContentLines {
		// Pad with empty lines
		for len(contentLines) < targetContentLines {
			contentLines = append(contentLines, "")
		}
	} else if len(contentLines) > targetContentLines {
		// Truncate
		contentLines = contentLines[:targetContentLines]
	}

	// Combine content and help
	result := strings.Join(contentLines, "\n")
	if helpContent != "" {
		result += "\n" + helpContent
	}

	return result
}

// HasSaveDialog returns true if a save dialog is visible
func (m *Model) HasSaveDialog() bool {
	return m.saveDialog != nil && m.saveDialog.Visible()
}

// OverlayDialog overlays the save dialog on top of bordered content
func (m *Model) OverlayDialog(borderedContent string) string {
	if m.saveDialog == nil || !m.saveDialog.Visible() {
		return borderedContent
	}
	return m.overlayCenter(borderedContent, m.saveDialog.View())
}

// overlayCenter renders the dialog as a true modal overlay on top of content
func (m *Model) overlayCenter(content string, dialogView string) string {
	dialogLines := strings.Split(dialogView, "\n")

	// Calculate dialog dimensions
	dialogHeight := len(dialogLines)
	dialogWidth := 0
	for _, line := range dialogLines {
		if w := lipgloss.Width(line); w > dialogWidth {
			dialogWidth = w
		}
	}

	// Center the dialog (true center)
	x := (m.width - dialogWidth) / 2
	y := (m.height - dialogHeight) / 2

	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	// Split content into lines - preserve exact line count
	contentLines := strings.Split(content, "\n")
	originalLineCount := len(contentLines)
	neededHeight := y + dialogHeight

	// Pad content lines if needed so the dialog can render within bounds.
	if originalLineCount < neededHeight {
		padWidth := 0
		if originalLineCount > 0 {
			padWidth = lipgloss.Width(contentLines[0])
		} else if m.width > 0 {
			padWidth = m.width - 2
		}
		padding := ""
		if padWidth > 0 {
			padding = strings.Repeat(" ", padWidth)
		}
		for len(contentLines) < neededHeight {
			contentLines = append(contentLines, padding)
		}
	}

	// Overlay dialog lines onto content using ANSI-aware functions
	for i, dialogLine := range dialogLines {
		contentY := y + i
		if contentY >= 0 && contentY < len(contentLines) {
			bgLine := contentLines[contentY]

			// Get left portion of background (before dialog)
			left := ansi.Truncate(bgLine, x, "")
			// Pad left if needed
			leftWidth := lipgloss.Width(left)
			if leftWidth < x {
				left += strings.Repeat(" ", x-leftWidth)
			}

			// Get right portion of background (after dialog)
			rightStart := x + dialogWidth
			bgWidth := lipgloss.Width(bgLine)
			var right string
			if rightStart < bgWidth {
				right = ansi.TruncateLeft(bgLine, rightStart, "")
			}

			// Compose: left + dialog + right
			contentLines[contentY] = left + dialogLine + right
		}
	}

	// Preserve original line count exactly
	maxLines := originalLineCount
	if neededHeight > maxLines {
		maxLines = neededHeight
	}
	if len(contentLines) > maxLines {
		contentLines = contentLines[:maxLines]
	}

	return strings.Join(contentLines, "\n")
}

func (m *Model) helpItem(key, desc string) string {
	return common.RenderHelpItem(m.styles, key, desc)
}

func (m *Model) helpLines(contentWidth int) []string {
	items := []string{}

	hasTabs := len(m.getTabs()) > 0
	if m.worktree != nil {
		items = append(items,
			m.helpItem("C-Spc a", "new tab"),
			m.helpItem("C-Spc d", "commits"),
		)
	}
	if hasTabs {
		items = append(items,
			m.helpItem("C-Spc s", "save"),
			m.helpItem("C-Spc x", "close"),
			m.helpItem("C-Spc p", "prev"),
			m.helpItem("C-Spc n", "next"),
			m.helpItem("C-Spc 1-9", "jump tab"),
			m.helpItem("C-Spc [", "copy"),
			m.helpItem("PgUp", "scroll up"),
			m.helpItem("PgDn", "scroll down"),
		)
		if m.CopyModeActive() {
			items = append(items,
				m.helpItem("g", "top"),
				m.helpItem("G", "bottom"),
			)
		}
	}
	return common.WrapHelpItems(items, contentWidth)
}

// renderTabBar renders the tab bar with activity indicators
func (m *Model) renderTabBar() string {
	m.tabHits = m.tabHits[:0]
	currentTabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()

	if len(currentTabs) == 0 {
		empty := m.styles.TabPlus.Render("New agent")
		emptyWidth := lipgloss.Width(empty)
		if emptyWidth > 0 {
			m.tabHits = append(m.tabHits, tabHit{
				kind:  tabHitPlus,
				index: -1,
				region: common.HitRegion{
					X:      0,
					Y:      0,
					Width:  emptyWidth,
					Height: 1,
				},
			})
		}
		return empty
	}

	var renderedTabs []string
	x := 0

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
		case "droid":
			agentStyle = m.styles.AgentDroid
		default:
			agentStyle = m.styles.AgentTerm
		}

		// Build tab content with agent-colored indicator and a close affordance
		closeLabel := m.styles.Muted.Render("x")
		content := agentStyle.Render(indicator) + name + " " + closeLabel

		style := m.styles.Tab
		var rendered string
		if i == activeIdx {
			// Active tab gets highlight border
			style = m.styles.ActiveTab
			rendered = style.Render(content)
		} else {
			// Inactive tab
			rendered = style.Render(content)
		}
		renderedWidth := lipgloss.Width(rendered)
		if renderedWidth > 0 {
			m.tabHits = append(m.tabHits, tabHit{
				kind:  tabHitTab,
				index: i,
				region: common.HitRegion{
					X:      x,
					Y:      0,
					Width:  renderedWidth,
					Height: 1,
				},
			})

			frameX, _ := style.GetFrameSize()
			leftFrame := frameX / 2
			prefixWidth := lipgloss.Width(agentStyle.Render(indicator) + name + " ")
			closeWidth := lipgloss.Width(closeLabel)
			closeX := x + leftFrame + prefixWidth
			if closeWidth > 0 {
				// Expand close button hit region for easier clicking
				// Include the space before "x" and extend to end of tab
				expandedCloseX := closeX - 1 // include the space before "x"
				expandedCloseWidth := renderedWidth - leftFrame - prefixWidth + 1
				m.tabHits = append(m.tabHits, tabHit{
					kind:  tabHitClose,
					index: i,
					region: common.HitRegion{
						X:      expandedCloseX,
						Y:      0,
						Width:  expandedCloseWidth,
						Height: 1,
					},
				})
			}
		}
		x += renderedWidth
		renderedTabs = append(renderedTabs, rendered)
	}

	// Add control buttons with matching border style
	btn := m.styles.TabPlus.Render("+")
	btnWidth := lipgloss.Width(btn)
	if btnWidth > 0 {
		m.tabHits = append(m.tabHits, tabHit{
			kind:  tabHitPlus,
			index: -1,
			region: common.HitRegion{
				X:      x,
				Y:      0,
				Width:  btnWidth,
				Height: 1,
			},
		})
	}
	renderedTabs = append(renderedTabs, btn)
	x += btnWidth

	// Add save button right-aligned when there are tabs
	if len(currentTabs) > 0 {
		saveBtn := m.styles.TabPlus.Render("Save")
		saveWidth := lipgloss.Width(saveBtn)

		// Calculate padding to right-align the Save button
		contentWidth := m.contentWidth()
		padding := contentWidth - x - saveWidth
		if padding > 0 {
			renderedTabs = append(renderedTabs, strings.Repeat(" ", padding))
			x += padding
		}

		if saveWidth > 0 {
			m.tabHits = append(m.tabHits, tabHit{
				kind:  tabHitSave,
				index: -1,
				region: common.HitRegion{
					X:      x,
					Y:      0,
					Width:  saveWidth,
					Height: 1,
				},
			})
		}
		renderedTabs = append(renderedTabs, saveBtn)
	}

	// Join tabs horizontally at the bottom so borders align
	return lipgloss.JoinHorizontal(lipgloss.Bottom, renderedTabs...)
}

func (m *Model) handleTabBarClick(msg tea.MouseClickMsg) tea.Cmd {
	// Tab bar is at screen Y=2: Y=0 is pane border, Y=1 is tab border, Y=2 is tab content
	// Account for border (1) and padding (1) on the left side when converting X coordinates
	const (
		borderTop   = 2
		borderLeft  = 1
		paddingLeft = 1
	)
	if msg.Y != borderTop {
		return nil
	}
	// Convert screen X to content X (subtract pane offset, border, and padding)
	localX := msg.X - m.offsetX - borderLeft - paddingLeft
	if localX < 0 {
		return nil
	}
	// Convert screen Y to local Y within tab bar content (all tab hits are at Y=0)
	localY := msg.Y - borderTop
	// Check close buttons first (they overlap with tab regions)
	for _, hit := range m.tabHits {
		if hit.kind == tabHitClose && hit.region.Contains(localX, localY) {
			return m.closeTabAt(hit.index)
		}
	}
	// Then check tabs and other buttons
	for _, hit := range m.tabHits {
		if hit.region.Contains(localX, localY) {
			switch hit.kind {
			case tabHitPlus:
				return func() tea.Msg { return messages.ShowSelectAssistantDialog{} }
			case tabHitTab:
				m.setActiveTabIdx(hit.index)
				return nil
			case tabHitSave:
				return func() tea.Msg { return messages.SaveThreadRequest{} }
			}
		}
	}
	return nil
}

// renderEmpty renders the empty state
func (m *Model) renderEmpty() string {
	var b strings.Builder
	b.WriteString("\n\n")
	b.WriteString(m.styles.Title.Render("No agents running"))
	b.WriteString("\n\n")

	// New agent button
	agentBtn := m.styles.TabPlus.Render("New agent")
	b.WriteString(agentBtn)
	b.WriteString("  ")

	// Commits button
	commitsBtn := m.styles.TabPlus.Render("Commits")
	b.WriteString(commitsBtn)

	// Help text
	b.WriteString("\n\n")
	helpStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
	b.WriteString(helpStyle.Render("C-Spc a:new agent  C-Spc d:commits"))

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
		termWidth := m.contentWidth()
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

// createViewerTab creates a new viewer tab for a file diff
func (m *Model) createViewerTab(file string, statusCode string, wt *data.Worktree) tea.Cmd {
	if wt == nil {
		return func() tea.Msg {
			return messages.Error{Err: fmt.Errorf("no worktree selected"), Context: "creating viewer"}
		}
	}
	return func() tea.Msg {
		logging.Info("Creating viewer tab: file=%s statusCode=%s worktree=%s", file, statusCode, wt.Name)

		// Escape filename for shell
		escapedFile := "'" + strings.ReplaceAll(file, "'", "'\\''") + "'"

		var cmd string
		if statusCode == "??" {
			// Untracked file: show full content with line numbers prefixed by + to indicate additions
			cmd = fmt.Sprintf("awk '{print \"\\033[32m+ \" $0 \"\\033[0m\"}' %s | less -R", escapedFile)
		} else {
			// Tracked file: use git diff with color
			cmd = fmt.Sprintf("git diff --color=always -- %s | less -R", escapedFile)
		}

		agent, err := m.agentManager.CreateViewer(wt, cmd)
		if err != nil {
			logging.Error("Failed to create viewer: %v", err)
			return messages.Error{Err: err, Context: "creating viewer"}
		}

		logging.Info("Viewer created, Terminal=%v", agent.Terminal != nil)

		// Calculate terminal dimensions
		termWidth := m.contentWidth()
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
		displayName := fmt.Sprintf("Diff: %s", file)
		if len(displayName) > 20 {
			displayName = "..." + displayName[len(displayName)-17:]
		}

		tab := &Tab{
			ID:        generateTabID(),
			Name:      displayName,
			Assistant: "viewer", // Use a generic type for styling
			Worktree:  wt,
			Agent:     agent,
			Terminal:  term,
			Running:   true,
		}

		// Set PTY size to match
		if agent.Terminal != nil {
			_ = agent.Terminal.SetSize(uint16(termHeight), uint16(termWidth))
		}

		// Add tab to the worktree's tab list
		m.tabsByWorktree[wtID] = append(m.tabsByWorktree[wtID], tab)
		m.activeTabByWorktree[wtID] = len(m.tabsByWorktree[wtID]) - 1

		return messages.TabCreated{Index: m.activeTabByWorktree[wtID], Name: displayName}
	}
}

// createCommitViewerTab creates a tab with the commit viewer component
func (m *Model) createCommitViewerTab(wt *data.Worktree) tea.Cmd {
	if wt == nil {
		return func() tea.Msg {
			return messages.Error{Err: fmt.Errorf("no worktree selected"), Context: "creating commit viewer"}
		}
	}

	logging.Info("Creating commit viewer tab: worktree=%s", wt.Name)

	// Calculate dimensions for the commit viewer
	viewerWidth := m.contentWidth()
	viewerHeight := m.height - 6
	if viewerWidth < 40 {
		viewerWidth = 80
	}
	if viewerHeight < 10 {
		viewerHeight = 24
	}

	// Create commit viewer model
	cv := commits.New(wt, viewerWidth, viewerHeight)
	cv.SetFocused(true)

	// Create tab with unique ID
	wtID := string(wt.ID())
	displayName := "Commits"

	tab := &Tab{
		ID:           generateTabID(),
		Name:         displayName,
		Assistant:    "commits",
		Worktree:     wt,
		CommitViewer: cv,
	}

	// Add tab to the worktree's tab list
	m.tabsByWorktree[wtID] = append(m.tabsByWorktree[wtID], tab)
	m.activeTabByWorktree[wtID] = len(m.tabsByWorktree[wtID]) - 1

	// Return the Init command to start loading commits
	return tea.Batch(
		cv.Init(),
		func() tea.Msg { return messages.TabCreated{Index: m.activeTabByWorktree[wtID], Name: displayName} },
	)
}

// createCommitDiffTab creates a viewer tab showing a specific commit's diff
func (m *Model) createCommitDiffTab(hash string, wt *data.Worktree) tea.Cmd {
	if wt == nil {
		return func() tea.Msg {
			return messages.Error{Err: fmt.Errorf("no worktree selected"), Context: "creating commit diff viewer"}
		}
	}
	return func() tea.Msg {
		logging.Info("Creating commit diff tab: hash=%s worktree=%s", hash, wt.Name)

		// Use git show with color, piped through less for interactive viewing
		cmd := fmt.Sprintf("git show --color=always %s | less -R", hash)

		agent, err := m.agentManager.CreateViewer(wt, cmd)
		if err != nil {
			logging.Error("Failed to create commit diff viewer: %v", err)
			return messages.Error{Err: err, Context: "creating commit diff viewer"}
		}

		// Calculate terminal dimensions
		termWidth := m.contentWidth()
		termHeight := m.height - 6
		if termWidth < 10 {
			termWidth = 80
		}
		if termHeight < 5 {
			termHeight = 24
		}

		// Create virtual terminal emulator with scrollback
		term := vterm.New(termWidth, termHeight)

		// Set up response writer for terminal queries
		if agent.Terminal != nil {
			term.SetResponseWriter(func(data []byte) {
				_ = agent.Terminal.SendString(string(data))
			})
		}

		// Create tab with unique ID
		wtID := string(wt.ID())
		displayName := fmt.Sprintf("Commit: %s", hash)

		tab := &Tab{
			ID:        generateTabID(),
			Name:      displayName,
			Assistant: "viewer",
			Worktree:  wt,
			Agent:     agent,
			Terminal:  term,
			Running:   true,
		}

		// Set PTY size to match
		if agent.Terminal != nil {
			_ = agent.Terminal.SetSize(uint16(termHeight), uint16(termWidth))
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
			m.tracePTYOutput(tab, buf[:n])
			return PTYOutput{WorktreeID: wtID, TabID: tabID, Data: buf[:n]}
		}
		// No data but no error - continue polling
		return PTYTick{WorktreeID: wtID, TabID: tabID}
	}
}

func (m *Model) tracePTYOutput(tab *Tab, data []byte) {
	if tab == nil || !ptyTraceAllowed(tab.Assistant) {
		return
	}

	tab.mu.Lock()
	defer tab.mu.Unlock()

	if tab.ptyTraceClosed {
		return
	}

	if tab.ptyTraceFile == nil {
		dir := ptyTraceDir()
		name := fmt.Sprintf("amux-pty-claude-%s-%s.log", tab.ID, time.Now().Format("20060102-150405"))
		path := filepath.Join(dir, name)
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			logging.Warn("PTY trace open failed: %v", err)
			tab.ptyTraceClosed = true
			return
		}
		tab.ptyTraceFile = file
		worktreeName := ""
		if tab.Worktree != nil {
			worktreeName = tab.Worktree.Name
		}
		_, _ = file.Write([]byte(fmt.Sprintf(
			"TRACE %s assistant=%s worktree=%s tab=%s\n",
			time.Now().Format(time.RFC3339Nano),
			tab.Assistant,
			worktreeName,
			tab.ID,
		)))
		logging.Info("PTY trace enabled: %s", path)
	}

	remaining := ptyTraceLimit - tab.ptyTraceBytes
	if remaining <= 0 {
		_ = tab.ptyTraceFile.Close()
		tab.ptyTraceClosed = true
		return
	}

	if len(data) > remaining {
		data = data[:remaining]
	}

	_, _ = tab.ptyTraceFile.Write([]byte(fmt.Sprintf("chunk offset=%d bytes=%d\n", tab.ptyTraceBytes, len(data))))
	_, _ = tab.ptyTraceFile.Write([]byte(hex.Dump(data)))
	tab.ptyTraceBytes += len(data)

	if tab.ptyTraceBytes >= ptyTraceLimit {
		_, _ = tab.ptyTraceFile.Write([]byte("TRACE TRUNCATED\n"))
		_ = tab.ptyTraceFile.Close()
		tab.ptyTraceClosed = true
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

	return m.closeTabAt(activeIdx)
}

func (m *Model) closeTabAt(index int) tea.Cmd {
	tabs := m.getTabs()
	if len(tabs) == 0 || index < 0 || index >= len(tabs) {
		return nil
	}

	tab := tabs[index]

	// Close agent
	if tab.Agent != nil {
		_ = m.agentManager.CloseAgent(tab.Agent)
	}

	tab.mu.Lock()
	if tab.ptyTraceFile != nil {
		_ = tab.ptyTraceFile.Close()
		tab.ptyTraceFile = nil
		tab.ptyTraceClosed = true
	}
	// Clean up CommitViewer
	tab.CommitViewer = nil
	tab.mu.Unlock()

	// Remove from tabs
	m.removeTab(index)

	// Adjust active tab
	tabs = m.getTabs() // Get updated tabs
	activeIdx := m.getActiveTabIdx()
	if index == activeIdx {
		if activeIdx >= len(tabs) && activeIdx > 0 {
			m.setActiveTabIdx(activeIdx - 1)
		}
	} else if index < activeIdx {
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
func (m *Model) handleCopyModeKey(tab *Tab, msg tea.KeyPressMsg) tea.Cmd {
	switch {
	// Exit copy mode
	case msg.Key().Code == tea.KeyEsc || msg.Key().Code == tea.KeyEscape:
		fallthrough
	case msg.String() == "q":
		tab.CopyMode = false
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.ScrollViewToBottom()
		}
		tab.mu.Unlock()
		return nil

	// Scroll up one line
	case msg.String() == "k":
		fallthrough
	case msg.Key().Code == tea.KeyUp:
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.ScrollView(1)
		}
		tab.mu.Unlock()
		return nil

	// Scroll down one line
	case msg.String() == "j":
		fallthrough
	case msg.Key().Code == tea.KeyDown:
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.ScrollView(-1)
		}
		tab.mu.Unlock()
		return nil

	// Scroll up quarter page
	case msg.Key().Code == tea.KeyPgUp:
		fallthrough
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+u"))):
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.ScrollView(tab.Terminal.Height / 4)
		}
		tab.mu.Unlock()
		return nil

	// Scroll down quarter page
	case msg.Key().Code == tea.KeyPgDown:
		fallthrough
	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+d"))):
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.ScrollView(-tab.Terminal.Height / 4)
		}
		tab.mu.Unlock()
		return nil

	// Scroll to top
	case msg.String() == "g":
		tab.mu.Lock()
		if tab.Terminal != nil {
			tab.Terminal.ScrollViewToTop()
		}
		tab.mu.Unlock()
		return nil

	// Scroll to bottom
	case msg.String() == "G":
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

	if m.saveDialog != nil {
		m.saveDialog.SetSize(width, height)
	}

	// Use centralized metrics for terminal sizing
	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height

	// CommitViewer uses the same dimensions
	viewerWidth := termWidth
	viewerHeight := termHeight

	// Update all terminals across all worktrees
	for _, tabs := range m.tabsByWorktree {
		for _, tab := range tabs {
			tab.mu.Lock()
			if tab.Terminal != nil {
				tab.Terminal.Resize(termWidth, termHeight)
			}
			if tab.CommitViewer != nil {
				tab.CommitViewer.SetSize(viewerWidth, viewerHeight)
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
	// Use centralized metrics for consistent geometry
	tm := m.terminalMetrics()

	// X offset includes pane position + border + padding
	contentStartX := m.offsetX + tm.ContentStartX
	// Y offset is just border + tab bar (pane Y starts at 0)
	contentStartY := tm.ContentStartY

	// Convert screen coordinates to terminal coordinates
	termX = screenX - contentStartX
	termY = screenY - contentStartY

	// Check bounds
	inBounds = termX >= 0 && termX < tm.Width && termY >= 0 && termY < tm.Height
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

// HasRunningAgents returns whether any tab has an active agent across worktrees.
func (m *Model) HasRunningAgents() bool {
	for _, tabs := range m.tabsByWorktree {
		for _, tab := range tabs {
			if tab.Running {
				return true
			}
		}
	}
	return false
}

// HasActiveAgents returns whether any tab has emitted output recently.
// This is used to drive UI activity indicators without relying on process liveness alone.
func (m *Model) HasActiveAgents() bool {
	now := time.Now()
	for _, tabs := range m.tabsByWorktree {
		for _, tab := range tabs {
			if !tab.Running {
				continue
			}
			if tab.flushScheduled || len(tab.pendingOutput) > 0 {
				return true
			}
			if !tab.lastOutputAt.IsZero() && now.Sub(tab.lastOutputAt) < 2*time.Second {
				return true
			}
		}
	}
	return false
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
			snap.Screen = tab.Terminal.VisibleScreen()
			snap.CursorX = tab.Terminal.CursorX
			snap.CursorY = tab.Terminal.CursorY
			snap.ViewOffset = tab.Terminal.ViewOffset
			snap.Width = tab.Terminal.Width
			snap.Height = tab.Terminal.Height
			snap.SelActive = tab.Terminal.SelActive()
			snap.SelStartX = tab.Terminal.SelStartX()
			snap.SelStartY = tab.Terminal.SelStartY()
			snap.SelEndX = tab.Terminal.SelEndX()
			snap.SelEndY = tab.Terminal.SelEndY()
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

// TerminalLayer returns a VTermLayer for the active terminal, if any.
// This creates a snapshot of the terminal state while holding the lock,
// so the returned layer can be safely used for rendering without locks.
// Uses snapshot caching to avoid recreating when terminal state unchanged.
func (m *Model) TerminalLayer() *compositor.VTermLayer {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return nil
	}
	tab := tabs[activeIdx]
	tab.mu.Lock()
	defer tab.mu.Unlock()
	if tab.Terminal == nil {
		return nil
	}

	// Check if we can reuse the cached snapshot
	version := tab.Terminal.Version()
	showCursor := m.focused
	if tab.cachedSnap != nil &&
		tab.cachedVersion == version &&
		tab.cachedShowCursor == showCursor {
		// Reuse cached snapshot
		return compositor.NewVTermLayer(tab.cachedSnap)
	}

	// Create new snapshot while holding the lock
	snap := compositor.NewVTermSnapshot(tab.Terminal, showCursor)
	if snap == nil {
		return nil
	}

	// Cache the snapshot
	tab.cachedSnap = snap
	tab.cachedVersion = version
	tab.cachedShowCursor = showCursor

	return compositor.NewVTermLayer(snap)
}

// HasCommitViewer returns true if the active tab has a commit viewer.
func (m *Model) HasCommitViewer() bool {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return false
	}
	tab := tabs[activeIdx]
	tab.mu.Lock()
	defer tab.mu.Unlock()
	return tab.CommitViewer != nil
}

// TerminalViewport returns the terminal content area coordinates relative to the pane.
// Returns (x, y, width, height) where the terminal content should be rendered.
// This is for layer-based rendering positioning within the bordered pane.
// Uses terminalMetrics() as the single source of truth for geometry.
func (m *Model) TerminalViewport() (x, y, width, height int) {
	tm := m.terminalMetrics()
	return tm.ContentStartX, tm.ContentStartY, tm.Width, tm.Height
}

// ViewChromeOnly renders only the pane chrome (border, tab bar, help lines) without
// the terminal content. This is used with VTermLayer for layer-based rendering.
// IMPORTANT: The output structure must match View() exactly so buildBorderedPane
// produces the same layout.
func (m *Model) ViewChromeOnly() string {
	defer perf.Time("center_view_chrome")()
	var b strings.Builder

	// Tab bar
	b.WriteString(m.renderTabBar())
	b.WriteString("\n")

	// Calculate content dimensions to match View() exactly
	contentWidth := m.contentWidth()
	if contentWidth < 1 {
		contentWidth = 1
	}

	helpLines := m.helpLines(contentWidth)
	if !m.showKeymapHints {
		helpLines = nil
	}
	statusLine := m.activeTerminalStatusLine()

	// Match View()'s padding logic exactly:
	// innerHeight = m.height - 2 (space inside buildBorderedPane)
	// targetContentLines = innerHeight - helpLineCount
	innerHeight := m.height - 2
	if innerHeight < 0 {
		innerHeight = 0
	}
	helpLineCount := len(helpLines)
	targetContentLines := innerHeight - helpLineCount
	if targetContentLines < 0 {
		targetContentLines = 0
	}

	// We already have 1 line (tab bar), so we need targetContentLines - 1 more lines
	emptyLinesNeeded := targetContentLines - 1
	statusLineVisible := statusLine != ""
	if statusLineVisible {
		if emptyLinesNeeded > 0 {
			emptyLinesNeeded--
		} else {
			statusLineVisible = false
		}
	}
	if emptyLinesNeeded < 0 {
		emptyLinesNeeded = 0
	}

	// Fill with empty lines (will be overwritten by VTermLayer)
	emptyLine := strings.Repeat(" ", contentWidth)
	for i := 0; i < emptyLinesNeeded; i++ {
		b.WriteString(emptyLine)
		b.WriteString("\n")
	}

	if statusLineVisible {
		b.WriteString(statusLine)
		if helpLineCount > 0 {
			b.WriteString("\n")
		}
	}

	// Add help lines at bottom (matching View()'s format)
	helpContent := strings.Join(helpLines, "\n")
	if helpContent != "" {
		b.WriteString(helpContent)
	}

	return b.String()
}

// terminalStatusLineLocked returns the status line for the active terminal.
// Caller must hold tab.mu.
func (m *Model) terminalStatusLineLocked(tab *Tab) string {
	if tab == nil || tab.Terminal == nil {
		return ""
	}
	if tab.CopyMode {
		modeStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(common.ColorBackground).
			Background(common.ColorWarning)
		return modeStyle.Render(" COPY MODE (q/Esc exit • j/k/↑/↓ line • PgUp/PgDn/Ctrl+u/d half • g/G top/bottom) ")
	}
	if tab.Terminal.IsScrolled() {
		offset, total := tab.Terminal.GetScrollInfo()
		scrollStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(common.ColorBackground).
			Background(common.ColorInfo)
		return scrollStyle.Render(" SCROLL: " + formatScrollPos(offset, total) + " ")
	}
	return ""
}

// activeTerminalStatusLine returns the status line for the active terminal.
func (m *Model) activeTerminalStatusLine() string {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return ""
	}
	tab := tabs[activeIdx]
	tab.mu.Lock()
	defer tab.mu.Unlock()
	return m.terminalStatusLineLocked(tab)
}

// CloseAllTabs is deprecated - tabs now persist per-worktree
// This is kept for compatibility but does nothing
func (m *Model) CloseAllTabs() {
	// No-op: tabs now persist per-worktree and are not closed when switching
}

func copyToClipboard(text string) error {
	// Prioritize pbcopy on macOS as it is more reliable in various environments.
	if runtime.GOOS == "darwin" {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	// Fallback to library for other OS or if pbcopy fails
	return clipboard.WriteAll(text)
}
