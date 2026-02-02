package dashboard

import (
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
		prefix := " "
		status := ""
		statusText := ""
		dirty := false
		main := m.getMainWorkspace(row.Project)
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
		} else if m.isProjectActive(row.Project) {
			style = m.styles.ActiveWorkspace.PaddingLeft(0)
		}
		// Dirty color takes priority for project headers.
		if dirty && !m.isProjectActive(row.Project) {
			style = style.Foreground(common.ColorSecondary)
		}

		// Reserve space for delete icon to keep status aligned
		deleteSlot := "   "
		deleteSlotWidth := 3
		if selected {
			deleteSlot = " " + common.Icons.Close + " "
		}

		// Append profile indicator if set
		name := row.Project.Name
		profileTag := ""
		if row.Project.Profile != "" {
			profileTag = " [" + row.Project.Profile + "]"
		}

		// Truncate project name to fit within pane (width - border - padding - status - deleteSlot)
		maxNameWidth := m.width - 3 - lipgloss.Width(status) - deleteSlotWidth - lipgloss.Width(prefix) - lipgloss.Width(profileTag) - 1
		if maxNameWidth > 0 && lipgloss.Width(name) > maxNameWidth {
			runes := []rune(name)
			for len(runes) > 0 && lipgloss.Width(string(runes)) > maxNameWidth-1 {
				runes = runes[:len(runes)-1]
			}
			name = string(runes) + "…"
		}

		// Track delete slot position for click detection
		if selected {
			m.deleteIconX = lipgloss.Width(style.Render(prefix + name + profileTag))
		}

		return style.Render(prefix+name+profileTag+deleteSlot) + status

	case RowWorkspace:
		unstyledPrefix := " "
		styledPrefix := " "
		name := row.Workspace.Name
		status := ""
		statusText := ""
		dirty := false

		// Agent state indicator (spinner=active, ●=running, ○=idle)
		indicatorWidth := 2 // icon + space
		agentState := 0
		indicator := common.Icons.Idle + " "
		if row.Workspace != nil {
			wsID := string(row.Workspace.ID())
			if state, hasAgents := m.workspaceAgentStates[wsID]; hasAgents {
				agentState = state
				switch {
				case state >= 2: // actively processing
					indicator = common.SpinnerFrame(m.spinnerFrame) + " "
				case state >= 1: // running but idle
					indicator = common.Icons.Running + " "
				}
			}
		}

		// Check deletion state first
		if m.deletingWorkspaces[row.Workspace.Root] {
			frame := common.SpinnerFrame(m.spinnerFrame)
			statusText = m.styles.StatusPending.Render(frame + " deleting")
		} else if _, ok := m.creatingWorkspaces[row.Workspace.Root]; ok {
			frame := common.SpinnerFrame(m.spinnerFrame)
			statusText = m.styles.StatusPending.Render(frame + " creating")
		} else if s, ok := m.statusCache[row.Workspace.Root]; ok && !s.Clean {
			dirty = true
		}
		if statusText != "" {
			status = " " + statusText
		}

		// Determine row style based on selection and dirty state
		style := m.styles.WorkspaceRow
		if selected {
			style = m.styles.SelectedRow
		} else if dirty {
			style = style.Foreground(common.ColorSecondary)
		}

		// Style indicator separately: primary for running/active, muted for idle
		iconFg := common.ColorMuted
		if agentState >= 1 {
			iconFg = common.ColorPrimary
		}
		iconStyle := lipgloss.NewStyle().Foreground(iconFg)
		if selected {
			iconStyle = iconStyle.Bold(true).Background(common.ColorSelection)
		}
		indicatorStyled := iconStyle.Render(indicator)

		// Reserve space for delete icon to keep status aligned
		deleteSlot := "   "
		deleteSlotWidth := 3
		if selected {
			deleteSlot = " " + common.Icons.Close + " "
		}

		// Truncate workspace name to fit within pane (width - border - padding - status - deleteSlot - indicator)
		prefixWidth := lipgloss.Width(unstyledPrefix) + lipgloss.Width(styledPrefix) + indicatorWidth
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
			m.deleteIconX = lipgloss.Width(unstyledPrefix+styledPrefix) + indicatorWidth + lipgloss.Width(style.Render(name))
		}

		return unstyledPrefix + style.Render(styledPrefix) + indicatorStyled + style.Render(name+deleteSlot) + status

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
			items = append(items, m.helpItem("P", "profile"))
		case RowProject:
			items = append(items, m.helpItem("D", "remove"))
			items = append(items, m.helpItem("P", "profile"))
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
		m.helpItem("C-Spc m", "monitor"),
		m.helpItem("C-Spc ?", "help"),
		m.helpItem("q", "quit"),
	)
	return common.WrapHelpItems(items, contentWidth)
}
