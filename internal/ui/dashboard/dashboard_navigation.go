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

// selectedWorkspaceIDAt returns the workspace ID of the row at idx, or "" if that
// row is not a workspace row. Read before SetProjects mutates m.rows.
func (m *Model) selectedWorkspaceIDAt(idx int) string {
	if idx < 0 || idx >= len(m.rows) {
		return ""
	}
	row := m.rows[idx]
	if row.Type == RowWorkspace && row.Workspace != nil {
		return string(row.Workspace.ID())
	}
	return ""
}

// workspaceRowIndex returns the index of the workspace row with the given ID, or
// -1 if no such row exists in the current rows.
func (m *Model) workspaceRowIndex(wsID string) int {
	for i := range m.rows {
		row := m.rows[i]
		if row.Type == RowWorkspace && row.Workspace != nil && string(row.Workspace.ID()) == wsID {
			return i
		}
	}
	return -1
}

// resolveCursorAfterRebuild re-anchors the cursor to the workspace selected
// before the rebuild. If that workspace is gone (deleted), it falls back to the
// nearest selectable row at or ABOVE the previous index — the predecessor — so
// repeated deletes walk upward instead of chasing the successor. A no-op when no
// workspace was selected; rebuildRows' clamp then governs.
func (m *Model) resolveCursorAfterRebuild(prevCursor int, selectedID string) {
	if selectedID == "" {
		return
	}
	if idx := m.workspaceRowIndex(selectedID); idx != -1 {
		m.cursor = idx
		return
	}
	// The selected workspace is gone. Land on the nearest selectable row strictly
	// ABOVE its old slot (the predecessor) — not the successor that shifted up into
	// that slot — so repeated deletes walk upward instead of chewing downward.
	start := prevCursor - 1
	if start > len(m.rows)-1 {
		start = len(m.rows) - 1
	}
	if start < 0 {
		return
	}
	if prev := m.findSelectableRow(start, -1); prev != -1 {
		m.cursor = prev
	}
}

// moveCursor moves the cursor by delta, skipping non-selectable rows
func (m *Model) moveCursor(delta int) {
	if len(m.rows) == 0 {
		return
	}

	// Determine direction and number of steps
	steps := delta
	if steps < 0 {
		steps = -steps
	}
	direction := 1
	if delta < 0 {
		direction = -1
	}

	// Walk row-by-row, skipping non-selectable rows
	for step := 0; step < steps; step++ {
		next := m.findSelectableRow(m.cursor+direction, direction)
		if next == -1 {
			// No more selectable rows in this direction
			break
		}
		m.cursor = next
	}
}

func rowLineCount(row Row) int {
	return 1
}

func (m *Model) rowIndexAt(screenX, screenY int) (int, bool) {
	borderTop := 1
	borderLeft := 1
	borderRight := 1
	paddingLeft := 0
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

	headerHeight := 0
	helpHeight := m.helpLineCount()
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

// ackDone marks a workspace's "done" indicator as seen so it stops rendering.
func (m *Model) ackDone(wsID string) {
	if wsID == "" {
		return
	}
	if m.doneAcked == nil {
		m.doneAcked = make(map[string]bool)
	}
	m.doneAcked[wsID] = true
}

// activateCurrentRow returns a command to activate the currently selected row.
// This is called automatically on cursor movement for instant content switching.
// Returns nil for rows that shouldn't auto-activate (like RowCreate which opens a dialog).
func (m *Model) activateCurrentRow() tea.Cmd {
	if m.cursor >= len(m.rows) {
		return nil
	}

	row := m.rows[m.cursor]
	switch row.Type {
	case RowHome:
		return func() tea.Msg { return messages.ShowWelcome{} }
	case RowProject:
		// Find and activate the main/primary workspace for this project
		var mainWS *data.Workspace
		for i := range row.Project.Workspaces {
			ws := &row.Project.Workspaces[i]
			if ws.IsMainBranch() || ws.IsPrimaryCheckout() {
				mainWS = ws
				break
			}
		}
		if mainWS != nil {
			m.ackDone(string(mainWS.ID()))
			return func() tea.Msg {
				return messages.WorkspaceActivated{
					Project:   row.Project,
					Workspace: mainWS,
				}
			}
		}
		return nil
	case RowWorkspace:
		if row.Workspace != nil {
			m.ackDone(string(row.Workspace.ID()))
		}
		return func() tea.Msg {
			return messages.WorkspaceActivated{
				Project:   row.Project,
				Workspace: row.Workspace,
			}
		}
	}

	// RowCreate, RowAddProject, RowSpacer - no auto-preview
	return nil
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
		// Find and activate the main/primary workspace for this project
		var mainWS *data.Workspace
		for i := range row.Project.Workspaces {
			ws := &row.Project.Workspaces[i]
			if ws.IsMainBranch() || ws.IsPrimaryCheckout() {
				mainWS = ws
				break
			}
		}
		if mainWS != nil {
			return func() tea.Msg {
				return messages.WorkspaceActivated{
					Project:   row.Project,
					Workspace: mainWS,
				}
			}
		}
		return nil
	case RowWorkspace:
		return func() tea.Msg {
			return messages.WorkspaceActivated{
				Project:   row.Project,
				Workspace: row.Workspace,
			}
		}
	case RowCreate:
		return func() tea.Msg {
			return messages.ShowCreateWorkspaceDialog{Project: row.Project}
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
	if row.Type == RowWorkspace && row.Workspace != nil {
		return func() tea.Msg {
			return messages.ShowDeleteWorkspaceDialog{
				Project:   row.Project,
				Workspace: row.Workspace,
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

// handleRename handles the rename key. Only workspace rows can be renamed
// (Tier-1 label rename); projects have no rename action.
func (m *Model) handleRename() tea.Cmd {
	if m.cursor >= len(m.rows) {
		return nil
	}

	row := m.rows[m.cursor]
	if row.Type == RowWorkspace && row.Workspace != nil {
		return func() tea.Msg {
			return messages.ShowRenameWorkspaceDialog{
				Project:   row.Project,
				Workspace: row.Workspace,
			}
		}
	}

	return nil
}

// refresh requests a workspace rescan/import.
func (m *Model) refresh() tea.Cmd {
	return func() tea.Msg { return messages.RescanWorkspaces{} }
}
