package dashboard

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

// isSelectable returns whether a row type can be selected
func isSelectable(rt RowType) bool {
	return rt != RowSpacer
}

// findSelectableRow finds a selectable row starting from 'from' in direction 'dir'.
// Returns -1 if none found.
func (m *Model) findSelectableRow(from, dir int) int {
	if dir == 0 {
		dir = 1 // Default to forward
	}
	for i := from; i >= 0 && i < len(m.rows); i += dir {
		if isSelectable(m.rows[i].Type) {
			return i
		}
	}
	return -1
}

// moveCursor moves the cursor by delta, skipping non-selectable rows
func (m *Model) moveCursor(delta int) {
	if len(m.rows) == 0 {
		return
	}

	target := m.cursor + delta

	// Determine search direction
	dir := delta
	if dir == 0 {
		dir = 1 // For delta==0, search forward first
	}

	newCursor := m.findSelectableRow(target, dir)

	// If not found and delta was 0, try the other direction
	if newCursor == -1 && delta == 0 {
		newCursor = m.findSelectableRow(target, -1)
	}

	if newCursor != -1 {
		m.cursor = newCursor
	}
}

func rowLineCount(row Row) int {
	switch row.Type {
	case RowProject:
		return 2
	default:
		return 1
	}
}

func (m *Model) rowIndexAt(screenX, screenY int) (int, bool) {
	borderTop := 1
	borderLeft := 1
	borderRight := 1
	paddingLeft := 1
	paddingRight := 1

	contentX := screenX - borderLeft - paddingLeft
	contentY := screenY - borderTop

	contentWidth := m.width - (borderLeft + borderRight + paddingLeft + paddingRight)
	innerHeight := m.height - 2
	if contentWidth <= 0 || innerHeight <= 0 {
		return -1, false
	}
	if contentX < 0 || contentX >= contentWidth {
		return -1, false
	}
	if contentY < 0 || contentY >= innerHeight {
		return -1, false
	}

	headerHeight := 1 // trailing blank line
	helpLines := m.helpLines(contentWidth)
	helpHeight := len(helpLines)
	toolbarHeight := m.toolbarHeight()
	rowAreaHeight := innerHeight - headerHeight - toolbarHeight - helpHeight
	if rowAreaHeight < 1 {
		rowAreaHeight = 1
	}

	if contentY < headerHeight || contentY >= headerHeight+rowAreaHeight {
		return -1, false
	}

	rowY := contentY - headerHeight
	line := 0
	for i := m.scrollOffset; i < len(m.rows); i++ {
		if line >= rowAreaHeight {
			break
		}
		rowLines := rowLineCount(m.rows[i])
		if rowY < line+rowLines {
			return i, true
		}
		line += rowLines
	}

	return -1, false
}

// handleEnter handles the enter key
func (m *Model) handleEnter() tea.Cmd {
	if m.cursor >= len(m.rows) {
		return nil
	}

	row := m.rows[m.cursor]
	switch row.Type {
	case RowHome:
		return func() tea.Msg { return messages.ShowWelcome{} }
	case RowProject:
		// Find and activate the main/primary worktree for this project
		var mainWT *data.Worktree
		for i := range row.Project.Worktrees {
			wt := &row.Project.Worktrees[i]
			if wt.IsMainBranch() || wt.IsPrimaryCheckout() {
				mainWT = wt
				break
			}
		}
		if mainWT != nil {
			return func() tea.Msg {
				return messages.WorktreeActivated{
					Project:  row.Project,
					Worktree: mainWT,
				}
			}
		}
		return nil
	case RowWorktree:
		return func() tea.Msg {
			return messages.WorktreeActivated{
				Project:  row.Project,
				Worktree: row.Worktree,
			}
		}
	case RowCreate:
		return func() tea.Msg {
			return messages.ShowCreateWorktreeDialog{Project: row.Project}
		}
	}

	return nil
}

// handleDelete handles the delete key
func (m *Model) handleDelete() tea.Cmd {
	if m.cursor >= len(m.rows) {
		return nil
	}

	row := m.rows[m.cursor]
	if row.Type == RowWorktree && row.Worktree != nil {
		return func() tea.Msg {
			return messages.ShowDeleteWorktreeDialog{
				Project:  row.Project,
				Worktree: row.Worktree,
			}
		}
	}
	if row.Type == RowProject && row.Project != nil {
		return func() tea.Msg {
			return messages.ShowRemoveProjectDialog{
				Project: row.Project,
			}
		}
	}

	return nil
}

// refresh requests a dashboard refresh
func (m *Model) refresh() tea.Cmd {
	return func() tea.Msg { return messages.RefreshDashboard{} }
}
