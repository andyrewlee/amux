package common

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/medusa/internal/config"
)

func (e *SandboxRulesEditor) View() string {
	if !e.visible {
		return ""
	}
	return e.dialogStyle().Render(strings.Join(e.renderLines(), "\n"))
}

func (e *SandboxRulesEditor) dialogStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorPrimary).
		Padding(1, 2).
		Width(e.dialogContentWidth())
}

func (e *SandboxRulesEditor) dialogContentWidth() int {
	if e.width > 0 {
		return min(80, max(60, e.width-20))
	}
	return 70
}

func (e *SandboxRulesEditor) renderLines() []string {
	var lines []string

	title := lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary)
	muted := lipgloss.NewStyle().Foreground(ColorMuted)

	lines = append(lines, title.Render("Sandbox Path Rules"))
	lines = append(lines, "")

	// Header
	contentWidth := e.dialogContentWidth() - 6
	lines = append(lines, muted.Render(fmt.Sprintf("  %-13s %-9s %s", "Action", "Type", "Path")))
	lines = append(lines, muted.Render("  "+strings.Repeat("─", min(contentWidth, 70))))

	// List with scrolling
	maxRows := e.maxVisibleRows()
	startIdx := e.scrollOffset
	endIdx := startIdx + maxRows
	if endIdx > len(e.rules) {
		endIdx = len(e.rules)
	}

	if len(e.rules) == 0 {
		lines = append(lines, muted.Render("  (no rules)"))
	} else {
		if startIdx > 0 {
			lines = append(lines, muted.Render("  ↑ more above"))
		}

		for i := startIdx; i < endIdx; i++ {
			rule := e.rules[i]
			prefix := "  "
			style := lipgloss.NewStyle().Foreground(ColorForeground)
			if rule.Locked {
				style = lipgloss.NewStyle().Foreground(ColorMuted)
			}
			if i == e.cursor {
				prefix = Icons.Cursor + " "
				if rule.Locked {
					style = style.Bold(true)
				} else {
					style = style.Foreground(ColorPrimary).Bold(true)
				}
			}

			actionStr := formatAction(rule.Action)
			pathTypeStr := string(rule.PathType)
			pathStr := rule.Path

			// Truncate path to fit
			maxPathLen := contentWidth - 25
			if maxPathLen < 10 {
				maxPathLen = 10
			}
			if len(pathStr) > maxPathLen {
				pathStr = pathStr[:maxPathLen-3] + "..."
			}

			line := fmt.Sprintf("%s%-13s %-9s %s", prefix, actionStr, pathTypeStr, pathStr)
			lines = append(lines, style.Render(line))
		}

		if endIdx < len(e.rules) {
			lines = append(lines, muted.Render("  ↓ more below"))
		}
	}

	// Always-included paths (not configurable)
	lines = append(lines, muted.Render("  "+strings.Repeat("─", min(contentWidth, 70))))
	lines = append(lines, muted.Render("  Always included:"))
	lines = append(lines, muted.Render(fmt.Sprintf("  %-13s %-9s %s", "allow-read", "*", "<all files (global)>")))
	lines = append(lines, muted.Render(fmt.Sprintf("  %-13s %-9s %s", "allow-write", "subpath", "<workspace root>")))
	lines = append(lines, muted.Render(fmt.Sprintf("  %-13s %-9s %s", "allow-write", "subpath", "<.git directories>")))
	lines = append(lines, muted.Render(fmt.Sprintf("  %-13s %-9s %s", "allow-write", "subpath", "<profile config, lock & shared>")))
	lines = append(lines, muted.Render(fmt.Sprintf("  %-13s %-9s %s", "allow-write", "regex", "^/dev/")))
	lines = append(lines, muted.Render(fmt.Sprintf("  %-13s %-9s %s", "allow-write", "subpath", "/private/tmp")))
	lines = append(lines, muted.Render(fmt.Sprintf("  %-13s %-9s %s", "allow-write", "subpath", "/private/var/folders")))
	lines = append(lines, "")

	// Input row (for adding or editing)
	if e.addingNew || e.editing {
		mode := "Add"
		if e.editing {
			mode = "Edit"
		}
		actionLabel := formatAction(sandboxActions[e.actionIdx])
		ptLabel := string(sandboxPathTypes[e.pathTypeIdx])
		lines = append(lines, fmt.Sprintf("%s: [%s] [%s]", mode, actionLabel, ptLabel))
		lines = append(lines, "Path: "+e.pathInput.View())
		lines = append(lines, muted.Render("Tab: cycle action  Shift-Tab: cycle type  Enter: confirm"))
		lines = append(lines, "")
	}

	// Controls
	lines = append(lines, muted.Render("[a] Add  [e] Edit  [d] Delete  [j/k] Navigate"))
	lines = append(lines, "")

	// Save / Cancel
	saveStyle := lipgloss.NewStyle().Foreground(ColorPrimary)
	lines = append(lines, saveStyle.Render("[S] Save")+"  "+muted.Render("[Esc] Cancel"))

	return lines
}

func formatAction(a config.SandboxAction) string {
	switch a {
	case config.SandboxAllowRead:
		return "allow-read"
	case config.SandboxAllowWrite:
		return "allow-write"
	case config.SandboxDenyRead:
		return "deny-read"
	default:
		return string(a)
	}
}
