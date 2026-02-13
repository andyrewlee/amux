package center

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// New creates a new center pane model.
func New(cfg *config.Config) *Model {
	return &Model{
		tabsByWorkspace:      make(map[string][]*Tab),
		activeTabByWorkspace: make(map[string]int),
		config:               cfg,
		agentManager:         appPty.NewAgentManager(cfg),
		styles:               common.DefaultStyles(),
		tabEvents:            make(chan tabEvent, 4096),
	}
}

// Init initializes the center pane.
func (m *Model) Init() tea.Cmd {
	return nil
}

// Focus sets the focus state.
func (m *Model) Focus() {
	m.focused = true
}

// Blur removes focus.
func (m *Model) Blur() {
	m.focused = false
}

// Focused returns whether the center pane is focused.
func (m *Model) Focused() bool {
	return m.focused
}

// SetWorkspace sets the active workspace.
func (m *Model) SetWorkspace(ws *data.Workspace) {
	m.workspace = ws
	if ws == nil {
		return
	}
	m.markTabFocused(m.workspaceID(), m.getActiveTabIdx())
}

// HasTabs returns whether there are any tabs for the current workspace.
func (m *Model) HasTabs() bool {
	return len(m.getTabs()) > 0
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
	// Propagate to all viewers in tabs
	for _, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			if tab != nil {
				if tab.DiffViewer != nil {
					tab.DiffViewer.SetStyles(styles)
				}
			}
		}
	}
}

// SetMsgSink sets a callback for PTY messages.
func (m *Model) SetMsgSink(sink func(tea.Msg)) {
	m.msgSink = sink
}

// SetSize sets the center pane size.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height

	// Use centralized metrics for terminal sizing
	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height

	// CommitViewer uses the same dimensions
	viewerWidth := termWidth
	viewerHeight := termHeight

	// Update all terminals across all workspaces
	for _, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			tab.mu.Lock()
			if tab.Terminal != nil {
				if tab.Terminal.Width != termWidth || tab.Terminal.Height != termHeight {
					tab.Terminal.Resize(termWidth, termHeight)
				}
			}
			if tab.DiffViewer != nil {
				tab.DiffViewer.SetSize(viewerWidth, viewerHeight)
			}
			tab.mu.Unlock()
			m.resizePTY(tab, termHeight, termWidth)
		}
	}
}

// SetOffset sets the X offset of the pane from screen left (for mouse coordinate conversion).
func (m *Model) SetOffset(x int) {
	m.offsetX = x
}

// Close cleans up all resources.
func (m *Model) Close() {
	for _, tabs := range m.tabsByWorkspace {
		for _, tab := range tabs {
			tab.markClosing()
			m.stopPTYReader(tab)
			tab.mu.Lock()
			if tab.ptyTraceFile != nil {
				_ = tab.ptyTraceFile.Close()
				tab.ptyTraceFile = nil
				tab.ptyTraceClosed = true
			}
			tab.pendingOutput = nil
			tab.DiffViewer = nil
			tab.Terminal = nil
			tab.cachedSnap = nil
			tab.Workspace = nil
			tab.Running = false
			tab.mu.Unlock()
			tab.markClosed()
		}
	}
	if m.agentManager != nil {
		m.agentManager.CloseAll()
	}
}

// TickSpinner advances the spinner animation frame.
func (m *Model) TickSpinner() {
	m.spinnerFrame++
}

// screenToTerminal converts screen coordinates to terminal coordinates
// Returns the terminal X, Y and whether the coordinates are within the terminal content area.
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
	return termX, termY, inBounds
}
