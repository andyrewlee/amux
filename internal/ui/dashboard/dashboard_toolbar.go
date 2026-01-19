package dashboard

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// renderToolbar renders the action buttons toolbar
func (m *Model) renderToolbar() string {
	m.toolbarHits = m.toolbarHits[:0]

	// Toolbar buttons use TabPlus style which has border (3 lines tall)
	buttonHeight := 3

	// First row: Help and Monitor
	var row1 []string
	x := 0

	// Help button
	helpBtn := m.styles.TabPlus.Render("Help")
	helpWidth := lipgloss.Width(helpBtn)
	m.toolbarHits = append(m.toolbarHits, toolbarButton{
		kind: toolbarHelp,
		region: common.HitRegion{
			X:      x,
			Y:      0,
			Width:  helpWidth,
			Height: buttonHeight,
		},
	})
	row1 = append(row1, helpBtn)
	x += helpWidth

	// Monitor button
	monitorBtn := m.styles.TabPlus.Render("Monitor")
	monitorWidth := lipgloss.Width(monitorBtn)
	m.toolbarHits = append(m.toolbarHits, toolbarButton{
		kind: toolbarMonitor,
		region: common.HitRegion{
			X:      x,
			Y:      0,
			Width:  monitorWidth,
			Height: buttonHeight,
		},
	})
	row1 = append(row1, monitorBtn)

	firstRow := lipgloss.JoinHorizontal(lipgloss.Bottom, row1...)

	// Second row: Delete/Remove button (only when cursor is on a worktree or project)
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		switch m.rows[m.cursor].Type {
		case RowWorktree:
			deleteBtn := m.styles.TabPlus.Render("Delete")
			deleteWidth := lipgloss.Width(deleteBtn)
			m.toolbarHits = append(m.toolbarHits, toolbarButton{
				kind: toolbarDelete,
				region: common.HitRegion{
					X:      0,
					Y:      buttonHeight, // Second row starts after first row
					Width:  deleteWidth,
					Height: buttonHeight,
				},
			})
			return firstRow + "\n" + deleteBtn
		case RowProject:
			removeBtn := m.styles.TabPlus.Render("Remove")
			removeWidth := lipgloss.Width(removeBtn)
			m.toolbarHits = append(m.toolbarHits, toolbarButton{
				kind: toolbarRemove,
				region: common.HitRegion{
					X:      0,
					Y:      buttonHeight,
					Width:  removeWidth,
					Height: buttonHeight,
				},
			})
			return firstRow + "\n" + removeBtn
		}
	}

	return firstRow
}

// toolbarHeight returns the current toolbar height based on whether delete is visible
func (m *Model) toolbarHeight() int {
	buttonHeight := 3
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		switch m.rows[m.cursor].Type {
		case RowWorktree, RowProject:
			return buttonHeight * 2 // Two rows
		}
	}
	return buttonHeight // One row
}

// handleToolbarClick checks if a click is on a toolbar button and returns the appropriate command
func (m *Model) handleToolbarClick(screenX, screenY int) tea.Cmd {
	// Convert screen coordinates to content coordinates
	borderTop := 1
	borderLeft := 1
	paddingLeft := 1

	contentX := screenX - borderLeft - paddingLeft
	contentY := screenY - borderTop

	toolbarHeight := m.toolbarHeight()

	// Check if click is within the toolbar area
	if contentY < m.toolbarY || contentY >= m.toolbarY+toolbarHeight {
		return nil
	}

	// Calculate Y relative to toolbar start
	localY := contentY - m.toolbarY

	// Check toolbar button hits
	for _, hit := range m.toolbarHits {
		if hit.region.Contains(contentX, localY) {
			switch hit.kind {
			case toolbarHelp:
				return func() tea.Msg { return messages.ToggleHelp{} }
			case toolbarMonitor:
				return func() tea.Msg { return messages.ToggleMonitor{} }
			case toolbarDelete:
				return m.handleDelete()
			case toolbarRemove:
				return m.handleDelete()
			}
		}
	}
	return nil
}
