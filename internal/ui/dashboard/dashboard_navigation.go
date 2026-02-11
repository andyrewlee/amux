package dashboard

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/messages"
)

// isSelectable returns whether a row type can be selected
func isSelectable(rt RowType) bool {
	switch rt {
	case RowSpacer:
		return false
	default:
		return true
	}
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

// previewCurrentRow returns a command to preview the currently selected row.
// This is called automatically on cursor movement for instant content switching.
func (m *Model) previewCurrentRow() tea.Cmd {
	// If toolbar is focused, show welcome screen
	if m.toolbarFocused {
		return func() tea.Msg { return messages.ShowWelcome{} }
	}

	if m.cursor >= len(m.rows) {
		return nil
	}

	row := m.rows[m.cursor]
	switch row.Type {
	case RowHome:
		return func() tea.Msg { return messages.ShowWelcome{} }
	case RowProject, RowCreate:
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
				return messages.WorkspacePreviewed{
					Project:   row.Project,
					Workspace: mainWS,
				}
			}
		}
		return nil
	case RowWorkspace:
		return func() tea.Msg {
			return messages.WorkspacePreviewed{
				Project:   row.Project,
				Workspace: row.Workspace,
			}
		}
	case RowGroupWorkspace:
		return func() tea.Msg {
			return messages.GroupWorkspacePreviewed{
				Group:     row.Group,
				Workspace: row.GroupWorkspace,
			}
		}
	case RowGroupHeader:
		return func() tea.Msg {
			return messages.GroupPreviewed{Group: row.Group}
		}
	case RowGroupCreate:
		// Preview the group header when on the "New" button
		return func() tea.Msg {
			return messages.GroupPreviewed{Group: row.Group}
		}
	}

	// RowAddProject, RowAddGroup, RowSpacer - no auto-preview
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
	case RowGroupHeader:
		// Activate first workspace if one exists
		if len(row.Group.Workspaces) > 0 {
			gw := &row.Group.Workspaces[0]
			return func() tea.Msg {
				return messages.GroupWorkspaceActivated{
					Group:     row.Group,
					Workspace: gw,
				}
			}
		}
		return nil
	case RowGroupWorkspace:
		return func() tea.Msg {
			return messages.GroupWorkspaceActivated{
				Group:     row.Group,
				Workspace: row.GroupWorkspace,
			}
		}
	case RowGroupCreate:
		return func() tea.Msg {
			return messages.ShowCreateGroupWorkspaceDialog{Group: row.Group}
		}
	case RowAddGroup:
		return func() tea.Msg {
			return messages.ShowAddProjectDialog{}
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
	if row.Type == RowGroupHeader && row.Group != nil {
		return func() tea.Msg {
			return messages.ShowDeleteGroupDialog{GroupName: row.Group.Name}
		}
	}
	if row.Type == RowGroupWorkspace && row.GroupWorkspace != nil {
		return func() tea.Msg {
			return messages.ShowDeleteGroupWorkspaceDialog{
				Group:     row.Group,
				Workspace: row.GroupWorkspace,
			}
		}
	}

	return nil
}

// handleSetProfile opens the profile dialog for the current project
func (m *Model) handleSetProfile() tea.Cmd {
	if m.cursor >= len(m.rows) {
		return nil
	}

	row := m.rows[m.cursor]
	switch row.Type {
	case RowProject, RowWorkspace:
		project := row.Project
		if project == nil {
			return nil
		}
		// Check if any workspace for this project has an active session
		for i := range project.Workspaces {
			ws := &project.Workspaces[i]
			if m.activeWorkspaceIDs[string(ws.ID())] {
				return func() tea.Msg {
					return messages.Toast{
						Message: "Cannot change profile while workspaces have active sessions",
						Level:   messages.ToastError,
					}
				}
			}
		}
		return func() tea.Msg {
			return messages.ShowSetProfileDialog{Project: project}
		}
	case RowGroupHeader, RowGroupWorkspace:
		group := row.Group
		if group == nil {
			return nil
		}
		// Check if any workspace in the group has an active session
		for i := range group.Workspaces {
			gw := &group.Workspaces[i]
			if m.activeWorkspaceIDs[string(gw.ID())] {
				return func() tea.Msg {
					return messages.Toast{
						Message: "Cannot change profile while workspaces have active sessions",
						Level:   messages.ToastError,
					}
				}
			}
		}
		return func() tea.Msg {
			return messages.ShowSetGroupProfileDialog{Group: group}
		}
	default:
		return nil
	}
}

// handleRename requests renaming the currently selected workspace.
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
	if row.Type == RowGroupWorkspace && row.GroupWorkspace != nil && row.Group != nil {
		return func() tea.Msg {
			return messages.ShowRenameGroupWorkspaceDialog{
				Group:     row.Group,
				Workspace: row.GroupWorkspace,
			}
		}
	}
	if row.Type == RowGroupHeader && row.Group != nil {
		return func() tea.Msg {
			return messages.ShowRenameGroupDialog{
				Group: row.Group,
			}
		}
	}
	return nil
}

// handleEditGroupRepos opens the edit repos dialog for the current group.
func (m *Model) handleEditGroupRepos() tea.Cmd {
	if m.cursor >= len(m.rows) {
		return nil
	}
	row := m.rows[m.cursor]
	if (row.Type == RowGroupHeader || row.Type == RowGroupWorkspace) && row.Group != nil {
		group := row.Group
		// Block editing if any workspace in the group has an active session
		for i := range group.Workspaces {
			gw := &group.Workspaces[i]
			if m.activeWorkspaceIDs[string(gw.ID())] {
				return func() tea.Msg {
					return messages.Toast{
						Message: "Cannot edit repos while workspaces have active sessions",
						Level:   messages.ToastError,
					}
				}
			}
		}
		return func() tea.Msg {
			return messages.ShowEditGroupReposDialog{Group: group}
		}
	}
	return nil
}

// refresh requests a workspace rescan/import.
func (m *Model) refresh() tea.Cmd {
	return func() tea.Msg { return messages.RescanWorkspaces{} }
}
