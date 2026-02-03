package common

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/messages"
)

func (d *PermissionsDialog) View() string {
	if !d.visible {
		return ""
	}
	return d.dialogStyle().Render(strings.Join(d.renderLines(), "\n"))
}

func (d *PermissionsDialog) dialogStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 2).
		Width(d.dialogContentWidth())
}

func (d *PermissionsDialog) dialogContentWidth() int {
	if d.width > 0 {
		return min(60, max(40, d.width-20))
	}
	return 50
}

func (d *PermissionsDialog) renderLines() []string {
	var lines []string

	title := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	muted := lipgloss.NewStyle().Foreground(ColorMuted)

	lines = append(lines, title.Render(fmt.Sprintf("Pending Permissions (%d new)", len(d.permissions))))
	lines = append(lines, "")

	for i, p := range d.permissions {
		actionLabel := d.actionLabel(p.Action)
		prefix := "  "
		style := lipgloss.NewStyle().Foreground(ColorForeground)
		if i == d.cursor {
			prefix = Icons.Cursor + " "
			style = style.Bold(true)
		}

		source := ""
		if p.Source != "" {
			source = muted.Render(fmt.Sprintf("  (%s)", p.Source))
		}

		// Show edit input if editing this permission
		if d.editing && i == d.editIndex {
			lines = append(lines, prefix+style.Render(actionLabel)+" "+d.input.View())
		} else {
			lines = append(lines, prefix+style.Render(actionLabel+" "+p.Permission)+source)
		}
	}

	// Show edit input row if editing
	if d.editing {
		lines = append(lines, "")
		lines = append(lines, muted.Render("  Enter to save • Esc to cancel"))
	}

	lines = append(lines, "")

	// Apply button
	applyStyle := muted
	if d.cursor >= len(d.permissions) && !d.editing {
		applyStyle = lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	}
	lines = append(lines, applyStyle.Render("[Apply]")+"  "+muted.Render("[Esc] Cancel"))

	lines = append(lines, "")
	if d.editing {
		lines = append(lines, muted.Render("Edit the permission to make it more generic"))
	} else {
		lines = append(lines, muted.Render("a/d/s set action • e edit • h/l cycle • Enter apply"))
	}

	return lines
}

func (d *PermissionsDialog) actionLabel(action messages.PermissionActionType) string {
	switch action {
	case messages.PermissionAllow:
		return lipgloss.NewStyle().Foreground(ColorSuccess).Render("[Allow]")
	case messages.PermissionDeny:
		return lipgloss.NewStyle().Foreground(ColorError).Render("[Deny ]")
	case messages.PermissionSkip:
		return lipgloss.NewStyle().Foreground(ColorMuted).Render("[Skip ]")
	default:
		return "[???  ]"
	}
}
