package sidebar

import (
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

	wtID := m.worktreeID()
	tab := tabs[idx]

	// Close PTY and cleanup
	if tab.State != nil {
		m.stopPTYReader(tab.State)
		if tab.State.Terminal != nil {
			tab.State.Terminal.Close()
		}
	}

	// Remove tab from slice
	m.tabsByWorktree[wtID] = append(tabs[:idx], tabs[idx+1:]...)

	// Adjust active index
	activeIdx := m.getActiveTabIdx()
	newLen := len(m.tabsByWorktree[wtID])
	if newLen == 0 {
		m.activeTabByWorktree[wtID] = 0
	} else if activeIdx >= newLen {
		m.activeTabByWorktree[wtID] = newLen - 1
	} else if idx < activeIdx {
		m.activeTabByWorktree[wtID] = activeIdx - 1
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
		if inBounds {
			ts.Selection = SelectionState{
				Active: true,
				StartX: termX,
				StartY: termY,
				EndX:   termX,
				EndY:   termY,
			}
			if ts.VTerm != nil {
				ts.VTerm.SetSelection(termX, termY, termX, termY, true, false)
			}
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
				termX, termY, true, false,
			)
		}
	}
	ts.mu.Unlock()

	return m, nil
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
		if ts.VTerm != nil {
			text := ts.VTerm.GetSelectedText(
				ts.Selection.StartX, ts.Selection.StartY,
				ts.Selection.EndX, ts.Selection.EndY,
			)
			if text != "" {
				if err := common.CopyToClipboard(text); err != nil {
					logging.Error("Failed to copy sidebar selection: %v", err)
				} else {
					logging.Info("Copied %d chars from sidebar", len(text))
				}
			}
		}
		ts.Selection.Active = false
	}
	ts.mu.Unlock()

	return m, nil
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
	return
}
