package dashboard

import (
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// tickSpinner returns a command that ticks the spinner
func (m *Model) tickSpinner() tea.Cmd {
	return common.SafeTick(spinnerInterval, func(t time.Time) tea.Msg {
		return SpinnerTickMsg{}
	})
}

// startSpinnerIfNeeded starts spinner ticks if we have pending activity.
func (m *Model) startSpinnerIfNeeded() tea.Cmd {
	if m.spinnerActive {
		return nil
	}
	if len(m.creatingWorkspaces) == 0 && len(m.deletingWorkspaces) == 0 {
		return nil
	}
	m.spinnerActive = true
	return m.tickSpinner()
}

// StartSpinnerIfNeeded is the public version for external callers.
func (m *Model) StartSpinnerIfNeeded() tea.Cmd {
	return m.startSpinnerIfNeeded()
}

// SetWorkspaceCreating marks a workspace as creating (or clears it).
func (m *Model) SetWorkspaceCreating(ws *data.Workspace, creating bool) tea.Cmd {
	if ws == nil {
		return nil
	}
	if creating {
		m.creatingWorkspaces[ws.Root] = ws
		m.rebuildRows()
		return m.startSpinnerIfNeeded()
	}
	delete(m.creatingWorkspaces, ws.Root)
	m.rebuildRows()
	return nil
}

// SetWorkspaceDeleting marks a workspace as deleting (or clears it).
func (m *Model) SetWorkspaceDeleting(root string, deleting bool) tea.Cmd {
	if deleting {
		m.deletingWorkspaces[root] = true
		return m.startSpinnerIfNeeded()
	}
	delete(m.deletingWorkspaces, root)
	return nil
}

// rebuildRows rebuilds the row list from projects
func (m *Model) rebuildRows() {
	m.rows = []Row{
		{Type: RowHome},
		{Type: RowSpacer},
	}

	for i := range m.projects {
		project := &m.projects[i]

		m.rows = append(m.rows, Row{
			Type:    RowProject,
			Project: project,
		})

		for _, ws := range m.sortedWorkspaces(project) {

			// Hide main branch - users access via project row
			if ws.IsMainBranch() || ws.IsPrimaryCheckout() {
				continue
			}

			m.rows = append(m.rows, Row{
				Type:      RowWorkspace,
				Project:   project,
				Workspace: ws,
			})
		}

		m.rows = append(m.rows, Row{
			Type:    RowCreate,
			Project: project,
		})

		m.rows = append(m.rows, Row{Type: RowSpacer})
	}

	// Clamp cursor
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	// Ensure cursor lands on a selectable row (skip spacers).
	if len(m.rows) > 0 && !isSelectable(m.rows[m.cursor].Type) {
		if next := m.findSelectableRow(m.cursor, 1); next != -1 {
			m.cursor = next
		} else if prev := m.findSelectableRow(m.cursor, -1); prev != -1 {
			m.cursor = prev
		}
	}

	m.clampScrollOffset()
}

// clampScrollOffset ensures scrollOffset stays within valid bounds.
func (m *Model) clampScrollOffset() {
	maxOffset := len(m.rows) - m.visibleHeight()
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.scrollOffset > maxOffset {
		m.scrollOffset = maxOffset
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

func (m *Model) sortedWorkspaces(project *data.Project) []*data.Workspace {
	existingRoots := make(map[string]bool, len(project.Workspaces))
	workspaces := make([]*data.Workspace, 0, len(project.Workspaces)+len(m.creatingWorkspaces))

	for i := range project.Workspaces {
		ws := &project.Workspaces[i]
		existingRoots[ws.Root] = true
		workspaces = append(workspaces, ws)
	}

	for _, ws := range m.creatingWorkspaces {
		if ws == nil || ws.Repo != project.Path {
			continue
		}
		if existingRoots[ws.Root] {
			continue
		}
		workspaces = append(workspaces, ws)
	}

	sort.SliceStable(workspaces, func(i, j int) bool {
		if workspaces[i].Created.Equal(workspaces[j].Created) {
			if workspaces[i].Name == workspaces[j].Name {
				return workspaces[i].Root < workspaces[j].Root
			}
			return workspaces[i].Name < workspaces[j].Name
		}
		return workspaces[i].Created.After(workspaces[j].Created)
	})

	return workspaces
}

// isProjectActive returns true if the project's main workspace is active
func (m *Model) isProjectActive(p *data.Project) bool {
	if p == nil {
		return false
	}
	main := m.getMainWorkspace(p)
	if main == nil {
		return false
	}
	return main.Root == m.activeRoot
}

// isProjectWorking returns true if any workspace in the project has active output.
func (m *Model) isProjectWorking(p *data.Project) bool {
	if p == nil {
		return false
	}
	for i := range p.Workspaces {
		ws := &p.Workspaces[i]
		if ws == nil {
			continue
		}
		if m.activeWorkspaceIDs[string(ws.ID())] {
			return true
		}
	}
	return false
}

// getMainWorkspace returns the primary or main branch workspace for a project
func (m *Model) getMainWorkspace(p *data.Project) *data.Workspace {
	if p == nil {
		return nil
	}
	for i := range p.Workspaces {
		ws := &p.Workspaces[i]
		if ws.IsMainBranch() || ws.IsPrimaryCheckout() {
			return ws
		}
	}
	return nil
}

// SelectedRow returns the currently selected row
func (m *Model) SelectedRow() *Row {
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		return &m.rows[m.cursor]
	}
	return nil
}

// Projects returns the current projects
func (m *Model) Projects() []data.Project {
	return m.projects
}

// ClearActiveRoot resets the active workspace selection to "Home".
func (m *Model) ClearActiveRoot() {
	m.activeRoot = ""
}
