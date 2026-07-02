package sidebar

import (
	"time"

	tea "charm.land/bubbletea/v2"

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
	sessionName := ""
	opts := m.tmuxOpts

	// Close PTY and cleanup
	if tab.State != nil {
		m.stopPTYReader(tab.State)
		tab.State.mu.Lock()
		sessionName = tab.State.SessionName
		if tab.State.Terminal != nil {
			tab.State.Terminal.Close()
		}
		tab.State.Running = false
		tab.State.RestartBackoff = 0
		tab.State.mu.Unlock()
	}

	// Remove tab from slice
	m.tabs.ByWorkspace[wtID] = append(tabs[:idx], tabs[idx+1:]...)

	// Adjust active index
	activeIdx := m.getActiveTabIdx()
	newLen := len(m.tabs.ByWorkspace[wtID])
	if newLen == 0 {
		m.tabs.ActiveByWorkspace[wtID] = 0
	} else if activeIdx >= newLen {
		m.tabs.ActiveByWorkspace[wtID] = newLen - 1
	} else if idx < activeIdx {
		m.tabs.ActiveByWorkspace[wtID] = activeIdx - 1
	}

	m.refreshTerminalSize()
	if sessionName == "" {
		return m, nil
	}
	return m, closeSessionIfUnattached(sessionName, opts)
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
			ts.Selection = common.SelectionState{
				Active:    true,
				StartX:    termX,
				StartLine: absLine,
				EndX:      termX,
				EndLine:   absLine,
			}
			ts.VTerm.SetSelection(termX, absLine, termX, absLine, true, false)
		} else {
			ts.Selection = common.SelectionState{}
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
		needTick, gen, seq := common.DragSelect(
			ts.VTerm,
			&ts.Selection,
			&ts.selectionScroll,
			termX, termY, ts.VTerm.Width, ts.VTerm.Height,
			&ts.selectionLastTermX,
			ts.VTerm.ScrollViewAndNote,
			ts.VTerm.ScreenYToAbsoluteLine,
		)

		// Start tick loop for continuous scrolling if needed
		if needTick {
			activeTab := m.getActiveTab()
			if activeTab != nil {
				wsID := m.workspaceID()
				tabID := activeTab.ID
				cmd = common.SafeTick(common.SelectionScrollTickInterval, func(time.Time) tea.Msg {
					return SidebarSelectionScrollTick{WorkspaceID: wsID, TabID: tabID, Gen: gen, Seq: seq}
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
	text := ""
	if ts.Selection.Active {
		// Only copy if selection spans more than a single point
		if ts.VTerm != nil &&
			(ts.Selection.StartX != ts.Selection.EndX ||
				ts.Selection.StartLine != ts.Selection.EndLine) {
			text = ts.VTerm.SelectedText()
			// Keep selection visible - don't clear it
		}
		ts.Selection.Active = false
		ts.selectionScroll.Reset()
	}
	ts.mu.Unlock()
	common.CopyToClipboardWithLog(text, "sidebar selection")

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
	if !ts.Selection.Active || ts.VTerm == nil || !ts.selectionScroll.HandleTick(msg.Gen, msg.Seq) {
		ts.mu.Unlock()
		return nil
	}
	common.SelectionScrollTickStep(
		ts.VTerm,
		&ts.Selection,
		&ts.selectionScroll,
		ts.VTerm.Height,
		ts.selectionLastTermX,
		ts.VTerm.ScrollViewAndNote,
		ts.VTerm.ScreenYToAbsoluteLine,
	)
	nextSeq := ts.selectionScroll.TickSeq

	ts.mu.Unlock()

	wsID := msg.WorkspaceID
	tabID := msg.TabID
	return common.SafeTick(common.SelectionScrollTickInterval, func(time.Time) tea.Msg {
		return SidebarSelectionScrollTick{WorkspaceID: wsID, TabID: tabID, Gen: msg.Gen, Seq: nextSeq}
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
