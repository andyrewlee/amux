package sidebar

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/perf"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/compositor"
	"github.com/andyrewlee/amux/internal/vterm"
)

// SelectionState tracks mouse selection state
type SelectionState struct {
	Active    bool
	StartX    int
	StartLine int // Absolute line number (0 = first scrollback line)
	EndX      int
	EndLine   int // Absolute line number
}

// TerminalTabID is a unique identifier for a terminal tab
type TerminalTabID string

// terminalTabIDCounter is used to generate unique tab IDs
var terminalTabIDCounter uint64

// generateTerminalTabID creates a new unique terminal tab ID
func generateTerminalTabID() TerminalTabID {
	id := atomic.AddUint64(&terminalTabIDCounter, 1)
	return TerminalTabID(fmt.Sprintf("term-tab-%d", id))
}

// TerminalTab represents a single terminal tab
type TerminalTab struct {
	ID    TerminalTabID
	Name  string // "Terminal 1", "Terminal 2", etc.
	State *TerminalState
}

// TerminalState holds the terminal state for a workspace
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

	// Snapshot cache for VTermLayer - avoid recreating snapshot when terminal unchanged
	cachedSnap       *compositor.VTermSnapshot
	cachedVersion    uint64
	cachedShowCursor bool

	readerActive      bool
	ptyMsgCh          chan tea.Msg
	readerCancel      chan struct{}
	ptyRestartBackoff time.Duration
	ptyHeartbeat      int64
	ptyRestartCount   int
	ptyRestartSince   time.Time
}

// terminalTabHitKind identifies the type of tab bar click target
type terminalTabHitKind int

const (
	terminalTabHitTab terminalTabHitKind = iota
	terminalTabHitClose
	terminalTabHitPlus
)

// terminalTabHit represents a clickable region in the tab bar
type terminalTabHit struct {
	kind   terminalTabHitKind
	index  int
	region common.HitRegion
}

// TerminalModel is the Bubbletea model for the sidebar terminal section
type TerminalModel struct {
	// State per workspace - multiple tabs per workspace
	tabsByWorkspace      map[string][]*TerminalTab
	activeTabByWorkspace map[string]int
	tabHits              []terminalTabHit // for mouse click handling
	pendingCreation      map[string]bool  // tracks workspaces with tab creation in progress

	// Current workspace
	workspace *data.Workspace

	// Layout
	width           int
	height          int
	focused         bool
	offsetX         int
	offsetY         int
	showKeymapHints bool

	// Styles
	styles common.Styles

	// PTY message sink
	msgSink func(tea.Msg)
}

// NewTerminalModel creates a new sidebar terminal model
func NewTerminalModel() *TerminalModel {
	return &TerminalModel{
		tabsByWorkspace:      make(map[string][]*TerminalTab),
		activeTabByWorkspace: make(map[string]int),
		pendingCreation:      make(map[string]bool),
		styles:               common.DefaultStyles(),
	}
}

// SetShowKeymapHints controls whether helper text is rendered.
func (m *TerminalModel) SetShowKeymapHints(show bool) {
	if m.showKeymapHints == show {
		return
	}
	m.showKeymapHints = show
	m.refreshTerminalSize()
}

// SetStyles updates the component's styles (for theme changes).
func (m *TerminalModel) SetStyles(styles common.Styles) {
	m.styles = styles
}

// SetMsgSink sets a callback for PTY messages.
func (m *TerminalModel) SetMsgSink(sink func(tea.Msg)) {
	m.msgSink = sink
}

// AddTerminalForHarness creates a terminal state without a PTY for benchmarks/tests.
func (m *TerminalModel) AddTerminalForHarness(ws *data.Workspace) {
	if ws == nil {
		return
	}
	m.workspace = ws
	wsID := string(ws.ID())
	if len(m.tabsByWorkspace[wsID]) > 0 {
		return
	}
	termWidth, termHeight := m.TerminalSize()
	vt := vterm.New(termWidth, termHeight)
	tab := &TerminalTab{
		ID:   generateTerminalTabID(),
		Name: "Terminal 1",
		State: &TerminalState{
			VTerm:      vt,
			Running:    true,
			lastWidth:  termWidth,
			lastHeight: termHeight,
		},
	}
	m.tabsByWorkspace[wsID] = []*TerminalTab{tab}
	m.activeTabByWorkspace[wsID] = 0
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

// workspaceID returns the ID of the current workspace
func (m *TerminalModel) workspaceID() string {
	if m.workspace == nil {
		return ""
	}
	return string(m.workspace.ID())
}

// getTabs returns the tabs for the current workspace
func (m *TerminalModel) getTabs() []*TerminalTab {
	return m.tabsByWorkspace[m.workspaceID()]
}

// getActiveTabIdx returns the active tab index for the current workspace
func (m *TerminalModel) getActiveTabIdx() int {
	return m.activeTabByWorkspace[m.workspaceID()]
}

// setActiveTabIdx sets the active tab index for the current workspace
func (m *TerminalModel) setActiveTabIdx(idx int) {
	m.activeTabByWorkspace[m.workspaceID()] = idx
}

// getActiveTab returns the active tab for the current workspace
func (m *TerminalModel) getActiveTab() *TerminalTab {
	tabs := m.getTabs()
	idx := m.getActiveTabIdx()
	if idx >= 0 && idx < len(tabs) {
		return tabs[idx]
	}
	return nil
}

// getTerminal returns the terminal state for the current workspace's active tab
func (m *TerminalModel) getTerminal() *TerminalState {
	tab := m.getActiveTab()
	if tab != nil {
		return tab.State
	}
	return nil
}

// getTabByID returns the tab with the given ID, or nil if not found
func (m *TerminalModel) getTabByID(wsID string, tabID TerminalTabID) *TerminalTab {
	for _, tab := range m.tabsByWorkspace[wsID] {
		if tab.ID == tabID {
			return tab
		}
	}
	return nil
}

// nextTerminalName returns the next available terminal name
func nextTerminalName(tabs []*TerminalTab) string {
	maxNum := 0
	for _, tab := range tabs {
		var num int
		if _, err := fmt.Sscanf(tab.Name, "Terminal %d", &num); err == nil {
			if num > maxNum {
				maxNum = num
			}
		}
	}
	return fmt.Sprintf("Terminal %d", maxNum+1)
}

// NextTab switches to the next terminal tab (circular)
func (m *TerminalModel) NextTab() {
	tabs := m.getTabs()
	if len(tabs) <= 1 {
		return
	}
	idx := m.getActiveTabIdx()
	idx = (idx + 1) % len(tabs)
	m.setActiveTabIdx(idx)
	m.refreshTerminalSize()
}

// PrevTab switches to the previous terminal tab (circular)
func (m *TerminalModel) PrevTab() {
	tabs := m.getTabs()
	if len(tabs) <= 1 {
		return
	}
	idx := m.getActiveTabIdx()
	idx = (idx - 1 + len(tabs)) % len(tabs)
	m.setActiveTabIdx(idx)
	m.refreshTerminalSize()
}

// SelectTab selects a tab by index
func (m *TerminalModel) SelectTab(idx int) {
	tabs := m.getTabs()
	if idx >= 0 && idx < len(tabs) {
		m.setActiveTabIdx(idx)
		m.refreshTerminalSize()
	}
}

// HasMultipleTabs returns true if there are multiple tabs for the current workspace
func (m *TerminalModel) HasMultipleTabs() bool {
	return len(m.getTabs()) > 1
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
		if err := ts.Terminal.SendString(bracketedText); err != nil {
			logging.Warn("Sidebar paste failed: %v", err)
			ts.mu.Lock()
			ts.Running = false
			ts.mu.Unlock()
		}
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

		// Check if this is Cmd+C (copy command)
		k := msg.Key()
		isCopyKey := k.Mod.Contains(tea.ModSuper) && k.Code == 'c'

		// Handle explicit Cmd+C to copy current selection
		if isCopyKey {
			ts.mu.Lock()
			if ts.VTerm != nil && ts.VTerm.HasSelection() {
				text := ts.VTerm.GetSelectedText(
					ts.VTerm.SelStartX(), ts.VTerm.SelStartLine(),
					ts.VTerm.SelEndX(), ts.VTerm.SelEndLine(),
				)
				if text != "" {
					if err := common.CopyToClipboard(text); err != nil {
						logging.Error("Failed to copy to clipboard: %v", err)
					} else {
						logging.Info("Cmd+C copied %d chars from sidebar", len(text))
					}
				}
			}
			ts.mu.Unlock()
			return m, nil // Don't forward to terminal, don't clear selection
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
			if err := ts.Terminal.SendString(string(input)); err != nil {
				logging.Warn("Sidebar input failed: %v", err)
				ts.mu.Lock()
				ts.Running = false
				ts.mu.Unlock()
			}
		}

	case messages.SidebarPTYOutput:
		wsID := msg.WorkspaceID
		tabID := TerminalTabID(msg.TabID)
		tab := m.getTabByID(wsID, tabID)
		if tab != nil && tab.State != nil {
			ts := tab.State
			ts.pendingOutput = append(ts.pendingOutput, msg.Data...)
			if len(ts.pendingOutput) > ptyMaxBufferedBytes {
				overflow := len(ts.pendingOutput) - ptyMaxBufferedBytes
				perf.Count("sidebar_pty_drop_bytes", int64(overflow))
				ts.pendingOutput = append([]byte(nil), ts.pendingOutput[overflow:]...)
			}
			ts.lastOutputAt = time.Now()
			if !ts.flushScheduled {
				ts.flushScheduled = true
				ts.flushPendingSince = ts.lastOutputAt
				quiet, _ := m.flushTiming()
				cmds = append(cmds, common.SafeTick(quiet, func(t time.Time) tea.Msg {
					return messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: msg.TabID}
				}))
			}
		}

	case messages.SidebarPTYFlush:
		wsID := msg.WorkspaceID
		tabID := TerminalTabID(msg.TabID)
		tab := m.getTabByID(wsID, tabID)
		if tab != nil && tab.State != nil {
			ts := tab.State
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
				cmds = append(cmds, common.SafeTick(delay, func(t time.Time) tea.Msg {
					return messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: msg.TabID}
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
					cmds = append(cmds, common.SafeTick(time.Millisecond, func(t time.Time) tea.Msg {
						return messages.SidebarPTYFlush{WorkspaceID: wsID, TabID: msg.TabID}
					}))
				}
			}
		}

	case messages.SidebarPTYStopped:
		wsID := msg.WorkspaceID
		tabID := TerminalTabID(msg.TabID)
		tab := m.getTabByID(wsID, tabID)
		if tab != nil && tab.State != nil {
			ts := tab.State
			termAlive := ts.Terminal != nil && !ts.Terminal.IsClosed()
			m.stopPTYReader(ts)
			if termAlive {
				shouldRestart := true
				var backoff time.Duration
				ts.mu.Lock()
				if ts.ptyRestartSince.IsZero() || time.Since(ts.ptyRestartSince) > ptyRestartWindow {
					ts.ptyRestartSince = time.Now()
					ts.ptyRestartCount = 0
				}
				ts.ptyRestartCount++
				if ts.ptyRestartCount > ptyRestartMax {
					shouldRestart = false
					ts.Running = false
					ts.ptyRestartBackoff = 0
				} else {
					backoff = ts.ptyRestartBackoff
					if backoff <= 0 {
						backoff = 200 * time.Millisecond
					} else {
						backoff *= 2
						if backoff > 5*time.Second {
							backoff = 5 * time.Second
						}
					}
					ts.ptyRestartBackoff = backoff
				}
				ts.mu.Unlock()
				if shouldRestart {
					restartTab := msg.TabID
					restartWt := msg.WorkspaceID
					cmds = append(cmds, common.SafeTick(backoff, func(time.Time) tea.Msg {
						return messages.SidebarPTYRestart{WorkspaceID: restartWt, TabID: restartTab}
					}))
					logging.Warn("Sidebar PTY stopped for workspace %s tab %s; restarting in %s: %v", wsID, tabID, backoff, msg.Err)
				} else {
					logging.Warn("Sidebar PTY stopped for workspace %s tab %s; restart limit reached: %v", wsID, tabID, msg.Err)
				}
			} else {
				ts.Running = false
				ts.mu.Lock()
				ts.ptyRestartBackoff = 0
				ts.ptyRestartCount = 0
				ts.ptyRestartSince = time.Time{}
				ts.mu.Unlock()
				logging.Info("Sidebar PTY stopped for workspace %s tab %s: %v", wsID, tabID, msg.Err)
			}
		}

	case messages.SidebarPTYRestart:
		tab := m.getTabByID(msg.WorkspaceID, TerminalTabID(msg.TabID))
		if tab == nil || tab.State == nil {
			break
		}
		ts := tab.State
		if ts.Terminal == nil || ts.Terminal.IsClosed() {
			ts.mu.Lock()
			ts.ptyRestartBackoff = 0
			ts.mu.Unlock()
			break
		}
		if cmd := m.startPTYReader(msg.WorkspaceID, tab.ID); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case SidebarTerminalCreated:
		cmd := m.HandleTerminalCreated(msg.WorkspaceID, msg.TabID, msg.Terminal)
		cmds = append(cmds, cmd)

	case SidebarTerminalCreateFailed:
		// Clear pending flag so user can retry
		delete(m.pendingCreation, msg.WorkspaceID)
		logging.Error("Failed to create sidebar terminal: %v", msg.Err)
		// Surface error to user via app-level error handling
		cmds = append(cmds, func() tea.Msg {
			return messages.Error{Err: msg.Err, Context: "creating sidebar terminal"}
		})

	case messages.WorkspaceDeleted:
		if msg.Workspace != nil {
			wsID := string(msg.Workspace.ID())
			tabs := m.tabsByWorkspace[wsID]
			for _, tab := range tabs {
				if tab.State != nil {
					m.stopPTYReader(tab.State)
					tab.State.mu.Lock()
					if tab.State.Terminal != nil {
						tab.State.Terminal.Close()
					}
					tab.State.Running = false
					tab.State.ptyRestartBackoff = 0
					tab.State.mu.Unlock()
				}
			}
			delete(m.tabsByWorkspace, wsID)
			delete(m.activeTabByWorkspace, wsID)
			delete(m.pendingCreation, wsID)
		}
	}

	return m, common.SafeBatch(cmds...)
}

// View renders the terminal section
func (m *TerminalModel) View() string {
	var b strings.Builder

	// Always render tab bar (shows "New terminal" when no tabs exist)
	tabBar := m.renderTabBar()
	if tabBar != "" {
		b.WriteString(tabBar)
		b.WriteString("\n")
	}

	ts := m.getTerminal()
	if ts == nil || ts.VTerm == nil {
		// Show placeholder when no terminal
		if len(m.getTabs()) == 0 {
			// Empty state - tab bar already shows "New terminal" button
		} else {
			placeholder := m.styles.Muted.Render("No terminal")
			b.WriteString(placeholder)
		}
	} else {
		ts.mu.Lock()
		ts.VTerm.ShowCursor = m.focused
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

		if isScrolled {
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
	helpLines := m.helpLinesForLayout(contentWidth)

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

// SetWorkspace sets the active workspace and creates terminal tab if needed
func (m *TerminalModel) SetWorkspace(ws *data.Workspace) tea.Cmd {
	m.workspace = ws
	if ws == nil {
		m.refreshTerminalSize()
		return nil
	}

	wsID := string(ws.ID())
	if len(m.tabsByWorkspace[wsID]) > 0 {
		// Tabs already exist for this workspace
		m.refreshTerminalSize()
		return nil
	}
	if m.pendingCreation[wsID] {
		// Creation already in progress
		return nil
	}

	// Create first terminal tab
	m.pendingCreation[wsID] = true
	return m.createTerminalTab(ws)
}

// SetWorkspacePreview sets the active workspace without creating tabs.
func (m *TerminalModel) SetWorkspacePreview(ws *data.Workspace) {
	m.workspace = ws
}

// EnsureTerminalTab creates a terminal tab if none exists for the current workspace.
// Used for lazy initialization when the terminal pane is focused.
func (m *TerminalModel) EnsureTerminalTab() tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	if len(m.getTabs()) > 0 {
		return nil
	}
	wsID := m.workspaceID()
	if m.pendingCreation[wsID] {
		return nil
	}
	m.pendingCreation[wsID] = true
	return m.createTerminalTab(m.workspace)
}

// CreateNewTab creates a new terminal tab for the current workspace and returns a command
func (m *TerminalModel) CreateNewTab() tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	return m.createTerminalTab(m.workspace)
}

// CloseActiveTab closes the active terminal tab
func (m *TerminalModel) CloseActiveTab() tea.Cmd {
	tabs := m.getTabs()
	if len(tabs) == 0 {
		return nil
	}

	wsID := m.workspaceID()
	idx := m.getActiveTabIdx()
	if idx < 0 || idx >= len(tabs) {
		return nil
	}

	tab := tabs[idx]

	// Close PTY and cleanup
	if tab.State != nil {
		m.stopPTYReader(tab.State)
		tab.State.mu.Lock()
		if tab.State.Terminal != nil {
			tab.State.Terminal.Close()
		}
		tab.State.Running = false
		tab.State.ptyRestartBackoff = 0
		tab.State.mu.Unlock()
	}

	// Remove tab from slice
	m.tabsByWorkspace[wsID] = append(tabs[:idx], tabs[idx+1:]...)

	// Adjust active index
	newLen := len(m.tabsByWorkspace[wsID])
	if newLen == 0 {
		m.activeTabByWorkspace[wsID] = 0
	} else if idx >= newLen {
		m.activeTabByWorkspace[wsID] = newLen - 1
	}

	m.refreshTerminalSize()
	return nil
}
