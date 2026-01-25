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
		return spinnerTickMsg{}
	})
}

// startSpinnerIfNeeded starts spinner ticks if we have pending activity.
func (m *Model) startSpinnerIfNeeded() tea.Cmd {
	if m.spinnerActive {
		return nil
	}
	if len(m.loadingStatus) == 0 && len(m.creatingWorktrees) == 0 && len(m.deletingWorktrees) == 0 {
		return nil
	}
	m.spinnerActive = true
	return m.tickSpinner()
}

// SetWorktreeCreating marks a worktree as creating (or clears it).
func (m *Model) SetWorktreeCreating(wt *data.Worktree, creating bool) tea.Cmd {
	if wt == nil {
		return nil
	}
	if creating {
		m.creatingWorktrees[wt.Root] = wt
		m.rebuildRows()
		return m.startSpinnerIfNeeded()
	}
	delete(m.creatingWorktrees, wt.Root)
	m.rebuildRows()
	return nil
}

// SetWorktreeDeleting marks a worktree as deleting (or clears it).
func (m *Model) SetWorktreeDeleting(root string, deleting bool) tea.Cmd {
	if deleting {
		m.deletingWorktrees[root] = true
		return m.startSpinnerIfNeeded()
	}
	delete(m.deletingWorktrees, root)
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

		for _, wt := range m.sortedWorktrees(project) {

			// Hide main branch - users access via project row
			if wt.IsMainBranch() || wt.IsPrimaryCheckout() {
				continue
			}

			m.rows = append(m.rows, Row{
				Type:     RowWorktree,
				Project:  project,
				Worktree: wt,
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
}

func (m *Model) sortedWorktrees(project *data.Project) []*data.Worktree {
	existingRoots := make(map[string]bool, len(project.Worktrees))
	worktrees := make([]*data.Worktree, 0, len(project.Worktrees)+len(m.creatingWorktrees))

	for i := range project.Worktrees {
		wt := &project.Worktrees[i]
		existingRoots[wt.Root] = true
		worktrees = append(worktrees, wt)
	}

	for _, wt := range m.creatingWorktrees {
		if wt == nil || wt.Repo != project.Path {
			continue
		}
		if existingRoots[wt.Root] {
			continue
		}
		worktrees = append(worktrees, wt)
	}

	sort.SliceStable(worktrees, func(i, j int) bool {
		return worktrees[i].Created.After(worktrees[j].Created)
	})

	return worktrees
}

// isProjectActive returns true if the project's main worktree is active
func (m *Model) isProjectActive(p *data.Project) bool {
	if p == nil {
		return false
	}
	main := m.getMainWorktree(p)
	if main == nil {
		return false
	}
	return main.Root == m.activeRoot
}

// getMainWorktree returns the primary or main branch worktree for a project
func (m *Model) getMainWorktree(p *data.Project) *data.Worktree {
	if p == nil {
		return nil
	}
	for i := range p.Worktrees {
		wt := &p.Worktrees[i]
		if wt.IsMainBranch() || wt.IsPrimaryCheckout() {
			return wt
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

// ClearActiveRoot resets the active worktree selection to "Home".
func (m *Model) ClearActiveRoot() {
	m.activeRoot = ""
}
