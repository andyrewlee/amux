package dashboard

import (
	"strconv"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/ui/common"
)

// renderRow renders a single dashboard row
func (m *Model) renderRow(row Row, selected bool) string {
	switch row.Type {
	case RowHome:
		style := m.styles.HomeRow
		if selected {
			style = m.styles.HomeRow.
				Bold(true).
				Foreground(common.ColorForeground).
				Background(common.ColorSelection)
		} else if m.activeRoot == "" {
			style = style.Bold(true).Foreground(common.ColorPrimary)
		}
		return style.Render("[amux]")

	case RowProject:
		status := ""
		main := m.getMainWorktree(row.Project)
		if main != nil {
			if m.deletingWorktrees[main.Root] {
				frame := common.SpinnerFrame(m.spinnerFrame)
				status = " " + m.styles.StatusPending.Render(frame+" deleting")
			} else if m.loadingStatus[main.Root] {
				frame := common.SpinnerFrame(m.spinnerFrame)
				status = " " + m.styles.StatusPending.Render(frame)
			} else if s, ok := m.statusCache[main.Root]; ok {
				if s.Clean {
					status = " " + m.styles.StatusClean.Render(common.Icons.Clean)
				} else {
					count := s.GetDirtyCount()
					status = " " + m.styles.StatusDirty.Render(common.Icons.Dirty+strconv.Itoa(count))
				}
			}
		}

		// Project headers are selectable to access main branch
		style := m.styles.ProjectHeader.MarginTop(0)
		if selected {
			style = style.
				Bold(true).
				Foreground(common.ColorForeground).
				Background(common.ColorSelection)
		} else if m.isProjectActive(row.Project) {
			style = m.styles.ActiveWorktree.PaddingLeft(0)
		}

		// Truncate project name to fit within pane (width - border - padding - status)
		name := row.Project.Name
		maxNameWidth := m.width - 4 - lipgloss.Width(status) - 1
		if maxNameWidth > 0 && lipgloss.Width(name) > maxNameWidth {
			runes := []rune(name)
			for len(runes) > 0 && lipgloss.Width(string(runes)) > maxNameWidth-1 {
				runes = runes[:len(runes)-1]
			}
			name = string(runes) + "…"
		}
		return style.Render(name) + status

	case RowWorktree:
		name := row.Worktree.Name
		status := ""

		// Check deletion state first
		if m.deletingWorktrees[row.Worktree.Root] {
			frame := common.SpinnerFrame(m.spinnerFrame)
			status = " " + m.styles.StatusPending.Render(frame+" deleting")
		} else if _, ok := m.creatingWorktrees[row.Worktree.Root]; ok {
			frame := common.SpinnerFrame(m.spinnerFrame)
			status = " " + m.styles.StatusPending.Render(frame+" creating")
		} else if m.loadingStatus[row.Worktree.Root] {
			// Show spinner while loading
			frame := common.SpinnerFrame(m.spinnerFrame)
			status = " " + m.styles.StatusPending.Render(frame)
		} else if s, ok := m.statusCache[row.Worktree.Root]; ok {
			if s.Clean {
				status = " " + m.styles.StatusClean.Render(common.Icons.Clean)
			} else {
				count := s.GetDirtyCount()
				status = " " + m.styles.StatusDirty.Render(common.Icons.Dirty+strconv.Itoa(count))
			}
		}

		// Determine row style based on selection and active state
		style := m.styles.WorktreeRow
		if selected {
			style = m.styles.SelectedRow
		} else if row.Worktree.Root == m.activeRoot {
			style = m.styles.ActiveWorktree
		}

		// Truncate worktree name to fit within pane (width - border - padding - status)
		maxNameWidth := m.width - 4 - lipgloss.Width(status) - 1
		if maxNameWidth > 0 && lipgloss.Width(name) > maxNameWidth {
			runes := []rune(name)
			for len(runes) > 0 && lipgloss.Width(string(runes)) > maxNameWidth-1 {
				runes = runes[:len(runes)-1]
			}
			name = string(runes) + "…"
		}
		return style.Render(name) + status

	case RowCreate:
		style := m.styles.CreateButton
		if selected {
			style = m.styles.SelectedRow
		}
		return style.Render(common.Icons.Add + " New")

	case RowSpacer:
		return ""
	}

	return ""
}

func (m *Model) helpItem(key, desc string) string {
	return common.RenderHelpItem(m.styles, key, desc)
}

func (m *Model) helpLines(contentWidth int) []string {
	items := []string{
		m.helpItem("k/↑", "up"),
		m.helpItem("j/↓", "down"),
		m.helpItem("enter", "open"),
	}
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		switch m.rows[m.cursor].Type {
		case RowWorktree:
			items = append(items, m.helpItem("D", "delete"))
		case RowProject:
			items = append(items, m.helpItem("D", "remove"))
		}
	}
	items = append(items,
		m.helpItem("r", "refresh"),
		m.helpItem("g", "top"),
		m.helpItem("G", "bottom"),
	)
	focusKey := "C-Spc h/j/k"
	if m.canFocusRight {
		focusKey = "C-Spc h/j/k/l"
	}
	items = append(items, m.helpItem(focusKey, "focus (or ←↑↓→)"))
	items = append(items,
		m.helpItem("C-Spc m", "monitor"),
		m.helpItem("C-Spc ?", "help"),
		m.helpItem("C-Spc q", "quit"),
	)
	return common.WrapHelpItems(items, contentWidth)
}
