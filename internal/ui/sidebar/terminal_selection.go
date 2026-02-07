package sidebar

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// handleTabBarClick handles mouse click events on the tab bar
func (m *TerminalModel) handleTabBarClick(msg tea.MouseClickMsg) (*TerminalModel, tea.Cmd) {
	// Tab bar is a single line tall.
	// Convert screen coordinates to local coordinates
	localX := msg.X - m.offsetX
	localY := msg.Y - m.offsetY

	// Tab bar spans Y=0 to Y=tabBarHeight-1
	if localY < 0 || localY >= tabBarHeight {
		return m, nil
	}

	// Hit regions are calculated for Y=0, so we check against that line.
	hitY := 0

	// Check close buttons first (they overlap with tab regions)
	for _, hit := range m.tabHits {
		if hit.kind == terminalTabHitClose && hit.region.Contains(localX, hitY) {
			tabs := m.getTabs()
			if hit.index >= 0 && hit.index < len(tabs) {
				return m.closeTabAt(hit.index)
			}
			return m, nil
		}
	}

	// Then check tabs and plus button
	for _, hit := range m.tabHits {
		if hit.region.Contains(localX, hitY) {
			switch hit.kind {
			case terminalTabHitPlus:
				return m, m.CreateNewTab()
			case terminalTabHitTab:
				m.setActiveTabIdx(hit.index)
				m.refreshTerminalSize()
				return m, nil
			}
		}
	}
	return m, nil
}

// closeTabAt closes the tab at the given index
func (m *TerminalModel) closeTabAt(idx int) (*TerminalModel, tea.Cmd) {
	tabs := m.getTabs()
	if idx < 0 || idx >= len(tabs) {
		return m, nil
	}

	wtID := m.workspaceID()
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
	m.tabsByWorkspace[wtID] = append(tabs[:idx], tabs[idx+1:]...)

	// Adjust active index
	activeIdx := m.getActiveTabIdx()
	newLen := len(m.tabsByWorkspace[wtID])
	if newLen == 0 {
		m.activeTabByWorkspace[wtID] = 0
	} else if activeIdx >= newLen {
		m.activeTabByWorkspace[wtID] = newLen - 1
	} else if idx < activeIdx {
		m.activeTabByWorkspace[wtID] = activeIdx - 1
	}

	m.refreshTerminalSize()
	return m, nil
}

// handleMouseClick handles mouse click events for selection
func (m *TerminalModel) handleMouseClick(msg tea.MouseClickMsg) (*TerminalModel, tea.Cmd) {
	if !m.focused {
		return m, nil
	}

	// Check if clicking on tab bar (3 lines tall)
	// Tab bar is always visible (shows "New terminal" when no tabs exist)
	if msg.Button == tea.MouseLeft {
		localY := msg.Y - m.offsetY
		if localY >= 0 && localY < tabBarHeight {
			return m.handleTabBarClick(msg)
		}
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
		ts.selectionScroll.Reset()
		if inBounds && ts.VTerm != nil {
			// Convert screen Y to absolute line number
			absLine := ts.VTerm.ScreenYToAbsoluteLine(termY)
			ts.Selection = SelectionState{
				Active:    true,
				StartX:    termX,
				StartLine: absLine,
				EndX:      termX,
				EndLine:   absLine,
			}
			ts.VTerm.SetSelection(termX, absLine, termX, absLine, true, false)
		} else {
			ts.Selection = SelectionState{}
		}
		ts.mu.Unlock()
	}

	return m, nil
}

// handleMouseMotion handles mouse motion events for selection dragging
func (m *TerminalModel) handleMouseMotion(msg tea.MouseMotionMsg) (*TerminalModel, tea.Cmd) {
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

	var cmd tea.Cmd

	ts.mu.Lock()
	if ts.Selection.Active && ts.VTerm != nil {
		termWidth := ts.VTerm.Width
		termHeight := ts.VTerm.Height

		// Clamp X to terminal bounds
		if termX < 0 {
			termX = 0
		}
		if termX >= termWidth {
			termX = termWidth - 1
		}

		// Set scroll direction from unclamped Y before clamping
		ts.selectionScroll.SetDirection(termY, termHeight)

		// Auto-scroll when dragging at edges
		if termY < 0 {
			// Dragging above viewport - scroll up into history
			ts.VTerm.ScrollView(1)
			termY = 0
		} else if termY >= termHeight {
			// Dragging below viewport - scroll down toward live
			ts.VTerm.ScrollView(-1)
			termY = termHeight - 1
		}

		// Convert to absolute line and update selection
		absLine := ts.VTerm.ScreenYToAbsoluteLine(termY)
		startX := ts.VTerm.SelStartX()
		startLine := ts.VTerm.SelStartLine()
		if !ts.VTerm.HasSelection() {
			startX = ts.Selection.StartX
			startLine = ts.Selection.StartLine
		}
		ts.Selection.EndX = termX
		ts.Selection.EndLine = absLine
		ts.VTerm.SetSelection(
			startX, startLine,
			termX, absLine, true, false,
		)
		ts.Selection.StartX = startX
		ts.Selection.StartLine = startLine

		// Store last X for tick-based endpoint updates
		ts.selectionLastTermX = termX

		// Start tick loop for continuous scrolling if needed
		if needTick, gen := ts.selectionScroll.NeedsTick(); needTick {
			activeTab := m.getActiveTab()
			if activeTab != nil {
				wsID := m.workspaceID()
				tabID := activeTab.ID
				cmd = common.SafeTick(common.SelectionScrollTickInterval, func(time.Time) tea.Msg {
					return SidebarSelectionScrollTick{WorkspaceID: wsID, TabID: tabID, Gen: gen}
				})
			}
		}
	}
	ts.mu.Unlock()

	return m, cmd
}

// handleMouseRelease handles mouse release events for selection completion
func (m *TerminalModel) handleMouseRelease(msg tea.MouseReleaseMsg) (*TerminalModel, tea.Cmd) {
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
		// Only copy if selection spans more than a single point
		if ts.VTerm != nil &&
			(ts.Selection.StartX != ts.Selection.EndX ||
				ts.Selection.StartLine != ts.Selection.EndLine) {
			text := ts.VTerm.GetSelectedText(
				ts.VTerm.SelStartX(), ts.VTerm.SelStartLine(),
				ts.VTerm.SelEndX(), ts.VTerm.SelEndLine(),
			)
			if text != "" {
				if err := common.CopyToClipboard(text); err != nil {
					logging.Error("Failed to copy sidebar selection: %v", err)
				} else {
					logging.Info("Copied %d chars from sidebar", len(text))
				}
			}
			// Keep selection visible - don't clear it
		}
		ts.Selection.Active = false
		ts.selectionScroll.Reset()
	}
	ts.mu.Unlock()

	return m, nil
}

// SetOffset sets the absolute screen coordinates where the terminal starts
func (m *TerminalModel) SetOffset(x, y int) {
	m.offsetX = x
	m.offsetY = y
}

// handleSelectionScrollTick handles a SidebarSelectionScrollTick message,
// scrolling the viewport and extending the selection highlight.
func (m *TerminalModel) handleSelectionScrollTick(msg SidebarSelectionScrollTick) tea.Cmd {
	tab := m.getTabByID(msg.WorkspaceID, msg.TabID)
	if tab == nil || tab.State == nil {
		return nil
	}
	ts := tab.State
	ts.mu.Lock()
	if !ts.Selection.Active || ts.VTerm == nil || !ts.selectionScroll.HandleTick(msg.Gen) {
		ts.mu.Unlock()
		return nil
	}
	ts.VTerm.ScrollView(ts.selectionScroll.ScrollDir)

	// Update selection endpoint to viewport edge
	edgeY := 0
	if ts.selectionScroll.ScrollDir < 0 {
		edgeY = ts.VTerm.Height - 1
	}
	absLine := ts.VTerm.ScreenYToAbsoluteLine(edgeY)
	endX := ts.selectionLastTermX
	startX := ts.VTerm.SelStartX()
	startLine := ts.VTerm.SelStartLine()
	if !ts.VTerm.HasSelection() {
		startX = ts.Selection.StartX
		startLine = ts.Selection.StartLine
	}
	ts.Selection.EndX = endX
	ts.Selection.EndLine = absLine
	ts.VTerm.SetSelection(startX, startLine, endX, absLine, true, false)
	ts.Selection.StartX = startX
	ts.Selection.StartLine = startLine

	ts.mu.Unlock()

	wsID := msg.WorkspaceID
	tabID := msg.TabID
	return common.SafeTick(common.SelectionScrollTickInterval, func(time.Time) tea.Msg {
		return SidebarSelectionScrollTick{WorkspaceID: wsID, TabID: tabID, Gen: msg.Gen}
	})
}

// screenToTerminal converts screen coordinates to terminal coordinates
func (m *TerminalModel) screenToTerminal(screenX, screenY int) (termX, termY int, inBounds bool) {
	termX = screenX - m.offsetX
	termY = screenY - m.offsetY

	// Account for tab bar offset
	termY -= tabBarHeight

	// Check bounds
	ts := m.getTerminal()
	if ts != nil && ts.VTerm != nil {
		inBounds = termX >= 0 && termX < ts.VTerm.Width && termY >= 0 && termY < ts.VTerm.Height
	} else {
		// Fallback if no terminal
		width, height, _ := m.terminalViewportSize()
		inBounds = termX >= 0 && termX < width && termY >= 0 && termY < height
	}
	return termX, termY, inBounds
}
