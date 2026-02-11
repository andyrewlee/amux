package dashboard

import (
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/medusa/internal/data"
	"github.com/andyrewlee/medusa/internal/ui/common"
)

// tickSpinner returns a command that ticks the spinner
func (m *Model) tickSpinner() tea.Cmd {
	return common.SafeTick(spinnerInterval, func(t time.Time) tea.Msg {
		return SpinnerTickMsg{}
	})
}

// startSpinnerIfNeeded starts spinner ticks if we have pending activity or running agents.
func (m *Model) startSpinnerIfNeeded() tea.Cmd {
	if m.spinnerActive {
		return nil
	}
	if len(m.creatingWorkspaces) == 0 && len(m.deletingWorkspaces) == 0 && !m.hasActiveAgents() && !m.forceSpinner {
		return nil
	}
	m.spinnerActive = true
	return m.tickSpinner()
}

// SetForceSpinner forces the spinner to stay active regardless of other state.
func (m *Model) SetForceSpinner(force bool) tea.Cmd {
	m.forceSpinner = force
	if force {
		return m.startSpinnerIfNeeded()
	}
	return nil
}

// hasActiveAgents returns true if any workspace has an actively processing agent.
func (m *Model) hasActiveAgents() bool {
	for _, state := range m.workspaceAgentStates {
		if state >= 2 {
			return true
		}
	}
	return false
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
		// Move cursor to the newly created workspace row.
		for i, row := range m.rows {
			if row.Type == RowWorkspace && row.Workspace != nil && row.Workspace.Root == ws.Root {
				m.cursor = i
				break
			}
		}
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

// rebuildRows rebuilds the row list from projects and groups
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

	// Group rows
	for i := range m.groups {
		group := &m.groups[i]

		m.rows = append(m.rows, Row{
			Type:  RowGroupHeader,
			Group: group,
		})

		for j := range group.Workspaces {
			gw := &group.Workspaces[j]
			if gw.Archived {
				continue
			}
			m.rows = append(m.rows, Row{
				Type:           RowGroupWorkspace,
				Group:          group,
				GroupWorkspace: gw,
			})
		}

		m.rows = append(m.rows, Row{
			Type:  RowGroupCreate,
			Group: group,
		})

		m.rows = append(m.rows, Row{Type: RowSpacer})
	}

	// Unified "+ Add Project" button at the bottom
	m.rows = append(m.rows, Row{Type: RowAddGroup})

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
		return workspaces[i].Created.Before(workspaces[j].Created)
	})

	return workspaces
}

// isProjectActive returns true if the project's primary workspace is active.
func (m *Model) isProjectActive(p *data.Project) bool {
	if p == nil {
		return false
	}
	mainWS := m.getMainWorkspace(p)
	if mainWS == nil {
		return false
	}
	return m.activeWorkspaceIDs[string(mainWS.ID())]
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
