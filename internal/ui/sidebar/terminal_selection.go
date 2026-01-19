package sidebar

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// handleMouseClick handles mouse click events for selection
func (m *TerminalModel) handleMouseClick(msg tea.MouseClickMsg) (*TerminalModel, tea.Cmd) {
	if !m.focused {
		return m, nil
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
