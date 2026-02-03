package common

import (
	"strings"

	"charm.land/lipgloss/v2"
)

func (e *PermissionsEditor) View() string {
	if !e.visible {
		return ""
	}
	return e.dialogStyle().Render(strings.Join(e.renderLines(), "\n"))
}

func (e *PermissionsEditor) dialogStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 2).
		Width(e.dialogContentWidth())
}

func (e *PermissionsEditor) dialogContentWidth() int {
	if e.width > 0 {
		return min(80, max(60, e.width-20))
	}
	return 70
}

func (e *PermissionsEditor) renderLines() []string {
	var lines []string

	title := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	muted := lipgloss.NewStyle().Foreground(ColorMuted)
	activeHeader := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	inactiveHeader := lipgloss.NewStyle().Bold(true).Foreground(ColorMuted)

	lines = append(lines, title.Render("Global Allow/Deny List"))
	lines = append(lines, "")

	// Calculate column width for side-by-side display
	contentWidth := e.dialogContentWidth() - 6 // subtract padding/borders
	colWidth := contentWidth / 2
	if colWidth < 20 {
		colWidth = 20
	}

	// Build Allow and Deny columns side by side
	allowHeader := inactiveHeader
	denyHeader := inactiveHeader
	if e.activeList == 0 {
		allowHeader = activeHeader
	} else {
		denyHeader = activeHeader
	}

	// Header row
	allowTitle := allowHeader.Render(padRight("Allow", colWidth))
	denyTitle := denyHeader.Render("Deny")
	lines = append(lines, allowTitle+denyTitle)

	// Separator row
	allowSep := allowHeader.Render(padRight(strings.Repeat("─", 5), colWidth))
	denySep := denyHeader.Render(strings.Repeat("─", 4))
	lines = append(lines, allowSep+denySep)

	// Build list items
	maxLen := max(len(e.allowList), len(e.denyList))
	if maxLen == 0 {
		maxLen = 1 // At least one row for "(empty)"
	}

	for i := 0; i < maxLen; i++ {
		var allowCell, denyCell string

		// Allow column
		if i < len(e.allowList) {
			prefix := "  "
			style := lipgloss.NewStyle().Foreground(ColorForeground)
			if e.activeList == 0 && i == e.cursor {
				prefix = Icons.Cursor + " "
				style = style.Foreground(ColorSuccess).Bold(true)
			}
			text := truncate(e.allowList[i], colWidth-3)
			allowCell = padRight(prefix+style.Render(text), colWidth)
		} else if i == 0 && len(e.allowList) == 0 {
			allowCell = padRight(muted.Render("  (empty)"), colWidth)
		} else {
			allowCell = padRight("", colWidth)
		}

		// Deny column
		if i < len(e.denyList) {
			prefix := "  "
			style := lipgloss.NewStyle().Foreground(ColorForeground)
			if e.activeList == 1 && i == e.cursor {
				prefix = Icons.Cursor + " "
				style = style.Foreground(ColorError).Bold(true)
			}
			text := truncate(e.denyList[i], colWidth-3)
			denyCell = prefix + style.Render(text)
		} else if i == 0 && len(e.denyList) == 0 {
			denyCell = muted.Render("  (empty)")
		} else {
			denyCell = ""
		}

		lines = append(lines, allowCell+denyCell)
	}
	lines = append(lines, "")

	// Input row (for adding or editing)
	if e.addingNew {
		listName := "Allow"
		if e.activeList == 1 {
			listName = "Deny"
		}
		lines = append(lines, "Add to "+listName+": "+e.input.View())
		lines = append(lines, "")
	} else if e.editing {
		lines = append(lines, "Edit: "+e.input.View())
		lines = append(lines, "")
	}

	// Help text explaining permission syntax
	lines = append(lines, muted.Render("Permission syntax:"))
	lines = append(lines, muted.Render("  Bash                - All bash commands"))
	lines = append(lines, muted.Render("  Bash(npm:*)         - Commands starting with 'npm'"))
	lines = append(lines, muted.Render("  Bash(git:*)         - Commands starting with 'git'"))
	lines = append(lines, muted.Render("  Read(~/.ssh/**)     - Read files in ~/.ssh recursively"))
	lines = append(lines, muted.Render("  Edit(/src/**)       - Edit files in project src/ dir"))
	lines = append(lines, "")

	// Controls
	lines = append(lines, muted.Render("[a] Add  [e] Edit  [d] Delete  [m] Move  [h/l] Switch"))
	lines = append(lines, "")

	// Save / Cancel
	saveStyle := lipgloss.NewStyle().Foreground(ColorPrimary)
	lines = append(lines, saveStyle.Render("[S] Save")+"  "+muted.Render("[Esc] Cancel"))

	return lines
}

// padRight pads a string to width with spaces on the right
func padRight(s string, width int) string {
	// Account for ANSI escape codes by using visible length
	visLen := lipgloss.Width(s)
	if visLen >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visLen)
}

// truncate shortens a string to maxLen, adding "..." if truncated
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
