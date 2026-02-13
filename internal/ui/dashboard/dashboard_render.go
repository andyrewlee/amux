package dashboard

import (
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/ui/common"
)

// applyDirtyForeground sets the dirty color (ColorSecondary) on a row style
// when the row is dirty but not active and not selected.
func applyDirtyForeground(style lipgloss.Style, dirty, active, selected bool) lipgloss.Style {
	if dirty && !active && !selected {
		return style.Foreground(common.ColorSecondary)
	}
	return style
}

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
			if m.activeRoot == "" {
				style = style.Foreground(common.ColorPrimary)
			}
		} else if m.activeRoot == "" {
			style = style.Bold(true).Foreground(common.ColorPrimary)
		}
		return style.Render("[amux]")

	case RowProject:
		prefix := " "
		status := ""
		statusText := ""
		dirty := false
		active := row.ActivityWorkspaceID != "" && m.activeWorkspaceIDs[row.ActivityWorkspaceID]
		main := row.MainWorkspace
		if main != nil {
			if m.deletingWorkspaces[main.Root] {
				frame := common.SpinnerFrame(m.spinnerFrame)
				statusText = m.styles.StatusPending.Render(frame + " deleting")
			} else if s, ok := m.statusCache[main.Root]; ok && !s.Clean {
				dirty = true
			}
		}
		if statusText != "" {
			status = " " + statusText
		}

		// Project headers are selectable to access main branch
		style := m.styles.ProjectHeader.MarginTop(0)
		if selected {
			style = style.
				Bold(true).
				Foreground(common.ColorForeground).
				Background(common.ColorSelection)
			if active {
				style = style.Foreground(common.ColorPrimary)
			}
		} else if active {
			style = m.styles.ActiveWorkspace.PaddingLeft(0)
		}
		style = applyDirtyForeground(style, dirty, active, selected)

		// Reserve space for delete icon to keep status aligned
		deleteSlot := "   "
		deleteSlotWidth := 3
		if selected {
			deleteSlot = " " + common.Icons.Close + " "
		}

		// Truncate project name to fit within pane (width - border - padding - status - deleteSlot)
		name := row.Project.Name
		maxNameWidth := m.width - 3 - lipgloss.Width(status) - deleteSlotWidth - lipgloss.Width(prefix) - 1
		if maxNameWidth > 0 && lipgloss.Width(name) > maxNameWidth {
			runes := []rune(name)
			for len(runes) > 0 && lipgloss.Width(string(runes)) > maxNameWidth-1 {
				runes = runes[:len(runes)-1]
			}
			name = string(runes) + "…"
		}

		// Track delete slot position for click detection
		if selected {
			m.deleteIconX = lipgloss.Width(style.Render(prefix + name))
		}

		return style.Render(prefix+name+deleteSlot) + status

	case RowWorkspace:
		unstyledPrefix := " "
		styledPrefix := " "
		name := row.Workspace.Name
		status := ""
		statusText := ""
		dirty := false
		working := false

		// Check deletion state first
		if m.deletingWorkspaces[row.Workspace.Root] {
			frame := common.SpinnerFrame(m.spinnerFrame)
			statusText = m.styles.StatusPending.Render(frame + " deleting")
		} else if _, ok := m.creatingWorkspaces[row.Workspace.Root]; ok {
			frame := common.SpinnerFrame(m.spinnerFrame)
			statusText = m.styles.StatusPending.Render(frame + " creating")
		} else if row.ActivityWorkspaceID != "" && m.activeWorkspaceIDs[row.ActivityWorkspaceID] {
			// Active agents - color change only, no spinner
			working = true
		} else if s, ok := m.statusCache[row.Workspace.Root]; ok && !s.Clean {
			dirty = true
		}
		if statusText != "" {
			status = " " + statusText
		}

		// Determine row style based on selection and active state
		style := m.styles.WorkspaceRow
		if selected {
			style = m.styles.SelectedRow
			if working {
				style = style.Foreground(common.ColorPrimary)
			}
		} else if working {
			style = m.styles.ActiveWorkspace
		}
		style = applyDirtyForeground(style, dirty, working, selected)
		// Reserve space for delete icon to keep status aligned
		deleteSlot := "   "
		deleteSlotWidth := 3
		if selected {
			deleteSlot = " " + common.Icons.Close + " "
		}

		// Truncate workspace name to fit within pane (width - border - padding - status - deleteSlot)
		prefixWidth := lipgloss.Width(unstyledPrefix) + lipgloss.Width(styledPrefix)
		maxNameWidth := m.width - 3 - lipgloss.Width(status) - deleteSlotWidth - prefixWidth - 1
		if maxNameWidth > 0 && lipgloss.Width(name) > maxNameWidth {
			runes := []rune(name)
			for len(runes) > 0 && lipgloss.Width(string(runes)) > maxNameWidth-1 {
				runes = runes[:len(runes)-1]
			}
			name = string(runes) + "…"
		}

		// Track delete slot position for click detection
		if selected {
			m.deleteIconX = lipgloss.Width(unstyledPrefix) + lipgloss.Width(style.Render(styledPrefix+name))
		}

		return unstyledPrefix + style.Render(styledPrefix+name+deleteSlot) + status

	case RowCreate:
		unstyledPrefix := " "
		styledPrefix := " "
		style := m.styles.CreateButton
		if selected {
			style = m.styles.SelectedRow
		}
		return unstyledPrefix + style.Render(styledPrefix+common.Icons.Add+" New ")

	case RowSpacer:
		return ""
	}

	return ""
}

func (m *Model) helpItem(key, desc string) string {
	return common.RenderHelpItem(m.styles, key, desc)
}

// helpLineCount returns the number of help lines that will be displayed.
// This encapsulates the showKeymapHints check to avoid bugs where callers
// forget to check it.
func (m *Model) helpLineCount() int {
	if !m.showKeymapHints {
		return 0
	}
	contentWidth := m.width - 3
	if contentWidth < 1 {
		contentWidth = 1
	}
	return len(m.helpLines(contentWidth))
}

func (m *Model) helpLines(contentWidth int) []string {
	items := []string{
		m.helpItem("k/↑", "up"),
		m.helpItem("j/↓", "down"),
		m.helpItem("enter", "open"),
	}
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		switch m.rows[m.cursor].Type {
		case RowWorkspace:
			items = append(items, m.helpItem("D", "delete"))
		case RowProject:
			items = append(items, m.helpItem("D", "remove"))
		}
	}
	items = append(items,
		m.helpItem("r", "rescan"),
		m.helpItem("g", "top"),
		m.helpItem("G", "bottom"),
	)
	focusKey := "C-Spc h/j/k"
	if m.canFocusRight {
		focusKey = "C-Spc h/j/k/l"
	}
	items = append(items, m.helpItem(focusKey, "focus (or ←↑↓→)"))
	items = append(items,
		m.helpItem("C-Spc ?", "help"),
		m.helpItem("C-Spc q", "quit"),
	)
	return common.WrapHelpItems(items, contentWidth)
}
