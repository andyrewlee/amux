package drawer

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/ui/common"
)

// View renders the drawer.
func (m *Model) View() string {
	contentWidth := m.width - 2
	if contentWidth < 1 {
		contentWidth = 1
	}

	var b strings.Builder
	b.WriteString(m.renderTabs(contentWidth))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", contentWidth))
	b.WriteString("\n")

	switch m.pane {
	case PaneLogs:
		b.WriteString(m.renderLogs(contentWidth))
	case PaneApprovals:
		b.WriteString(m.renderApprovals(contentWidth))
	case PaneProcesses:
		b.WriteString(m.renderProcesses(contentWidth))
	}

	helpLines := m.helpLines(contentWidth)
	if !m.showKeymapHints {
		helpLines = nil
	}
	if len(helpLines) > 0 {
		b.WriteString("\n")
		b.WriteString(strings.Join(helpLines, "\n"))
	}

	style := m.styles.Pane
	if m.focused {
		style = m.styles.FocusedPane
	}
	return style.Width(m.width - 2).Render(b.String())
}

func (m *Model) renderTabs(width int) string {
	labels := []string{"Logs", "Approvals", "Processes"}
	var parts []string
	for i, label := range labels {
		style := m.styles.Muted
		if m.pane == Pane(i) {
			style = m.styles.Title
		}
		rendered := style.Render(label)
		if m.zone != nil {
			rendered = m.zone.Mark(tabZoneID(i), rendered)
		}
		parts = append(parts, rendered)
	}
	left := strings.Join(parts, "  ")

	stop := m.styles.Muted.Render("[Stop]")
	open := m.styles.Muted.Render("[Open]")
	copyLabel := m.styles.Muted.Render("[Copy]")
	if m.zone != nil {
		stop = m.zone.Mark(toolbarZoneID("stop"), stop)
		open = m.zone.Mark(toolbarZoneID("open"), open)
		copyLabel = m.zone.Mark(toolbarZoneID("copy"), copyLabel)
	}
	actions := []string{stop, open}
	if m.pane == PaneLogs {
		actions = append(actions, copyLabel)
	}
	right := strings.Join(actions, " ")

	if right == "" {
		return truncate(left, width)
	}
	if lipgloss.Width(left)+1+lipgloss.Width(right) > width {
		return truncate(left, width)
	}
	return padRight(left, width-lipgloss.Width(right)-1) + " " + right
}

func (m *Model) renderLogs(width int) string {
	header := ""
	if proc := m.SelectedProcess(); proc != nil {
		status := proc.Status
		if status == "" {
			status = "idle"
		}
		header = fmt.Sprintf("%s • %s", proc.Name, status)
	}

	logs := m.visibleLogs()
	if len(logs) == 0 {
		if header != "" {
			return m.styles.Muted.Render(truncate(header+"\nNo logs for selected process", width))
		}
		if m.devURL != "" {
			return truncate("URL: "+m.devURL, width)
		}
		return m.styles.Muted.Render("No activity yet")
	}

	var lines []string
	if header != "" {
		lines = append(lines, header, "")
	}
	if m.devURL != "" {
		lines = append(lines, "URL: "+m.devURL)
	}
	for i, item := range logs {
		selected := i == m.selectedLog
		lines = append(lines, m.renderLogLine(item.Entry, width, selected))
		if m.expandedLogs[item.Entry.ID] {
			for _, detail := range item.Entry.Details {
				lines = append(lines, m.renderDetailLine(detail, width, selected))
			}
		}
		if item.Entry.Kind == common.ActivityApproval && item.Entry.Status == common.StatusPending {
			lines = append(lines, m.renderApprovalActions(item.Entry, width))
		}
	}
	if len(lines) > m.height-6 {
		lines = lines[len(lines)-(m.height-6):]
	}
	return truncate(strings.Join(lines, "\n"), width)
}

func (m *Model) renderApprovals(width int) string {
	if len(m.approvals) == 0 {
		return m.styles.Muted.Render("No approvals pending")
	}
	lines := []string{}
	for i, item := range m.approvals {
		line := item.Summary
		if item.ExpiresAt.After(time.Now()) {
			remaining := time.Until(item.ExpiresAt)
			line = fmt.Sprintf("%s (%s left)", line, formatShortDuration(remaining))
		}
		line = truncate(line, width)
		if i == m.selectedApproval {
			line = m.styles.SelectedRow.Render(line)
		}
		if m.zone != nil {
			line = m.zone.Mark(approvalZoneID(i), line)
		}
		lines = append(lines, line)
		if len(item.Details) > 0 && i == m.selectedApproval {
			for _, detail := range item.Details {
				lines = append(lines, truncate("  "+detail, width))
			}
		}
		lines = append(lines, m.renderApprovalButtons(item.ID, width))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderProcesses(width int) string {
	if len(m.processes) == 0 {
		return m.styles.Muted.Render("No processes running")
	}
	lines := make([]string, 0, len(m.processes))
	for i, proc := range m.processes {
		statusIcon := common.Icons.Idle
		if proc.Status == "running" {
			statusIcon = common.Icons.Running
		} else if proc.ExitCode != nil && *proc.ExitCode != 0 {
			statusIcon = common.Icons.Delete
		} else if proc.ExitCode != nil {
			statusIcon = common.Icons.Clean
		}
		timeAgo := ""
		if proc.Status == "running" && !proc.StartedAt.IsZero() {
			timeAgo = " • " + relativeTime(proc.StartedAt)
		} else if !proc.CompletedAt.IsZero() {
			timeAgo = " • done " + relativeTime(proc.CompletedAt)
		}
		exitLabel := ""
		if proc.ExitCode != nil {
			exitLabel = fmt.Sprintf(" (exit %d)", *proc.ExitCode)
		}
		line := fmt.Sprintf("%s %s%s%s", statusIcon, proc.Name, timeAgo, exitLabel)
		line = truncate(line, width)
		if i == m.selectedProc {
			line = m.styles.SelectedRow.Render(line)
		}
		if m.zone != nil {
			line = m.zone.Mark(processZoneID(i), line)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderLogLine(entry common.ActivityEntry, width int, selected bool) string {
	icon := activityStatusIcon(entry.Status)
	prefix := common.ActivityPrefix(entry.Kind)
	summary := entry.Summary
	if summary == "" && len(entry.Details) > 0 {
		summary = entry.Details[0]
	}
	expand := ""
	if len(entry.Details) > 0 {
		if m.expandedLogs[entry.ID] {
			expand = "▾ "
		} else {
			expand = "▸ "
		}
	}
	line := fmt.Sprintf("%s %s %s%s", icon, prefix, expand, summary)
	line = truncate(line, width)
	if selected {
		line = m.styles.SelectedRow.Render(line)
	} else if entry.Kind == common.ActivityPlan || entry.Kind == common.ActivitySummary {
		line = m.styles.Bold.Render(line)
	}
	if m.zone != nil {
		line = m.zone.Mark(logZoneID(entry.ID), line)
	}
	return line
}

func (m *Model) renderDetailLine(detail string, width int, selected bool) string {
	line := truncate("  "+detail, width)
	if selected {
		return m.styles.SelectedRow.Render(line)
	}
	return line
}

func (m *Model) renderApprovalActions(entry common.ActivityEntry, width int) string {
	approve := "[Approve]"
	deny := "[Deny]"
	if m.zone != nil {
		approve = m.zone.Mark(approvalActionZoneID(entry.ApprovalID, "approve"), approve)
		deny = m.zone.Mark(approvalActionZoneID(entry.ApprovalID, "deny"), deny)
	}
	line := approve + " " + deny
	return truncate(line, width)
}

func (m *Model) renderApprovalButtons(id string, width int) string {
	approve := "[Approve]"
	deny := "[Deny]"
	if m.zone != nil {
		approve = m.zone.Mark(approvalActionZoneID(id, "approve"), approve)
		deny = m.zone.Mark(approvalActionZoneID(id, "deny"), deny)
	}
	line := approve + " " + deny
	return truncate(line, width)
}

func (m *Model) helpLines(contentWidth int) []string {
	items := []string{
		common.RenderHelpItem(m.styles, "[", "prev"),
		common.RenderHelpItem(m.styles, "]", "next"),
		common.RenderHelpItem(m.styles, "j/k", "select"),
		common.RenderHelpItem(m.styles, "Enter", "expand"),
		common.RenderHelpItem(m.styles, "x", "stop"),
		common.RenderHelpItem(m.styles, "o", "open"),
		common.RenderHelpItem(m.styles, "c", "copy logs"),
	}
	return common.WrapHelpItems(items, contentWidth)
}

func truncate(text string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}

func padRight(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(text) >= width {
		return text
	}
	return text + strings.Repeat(" ", width-lipgloss.Width(text))
}

func relativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func activityStatusIcon(status common.ActivityStatus) string {
	switch status {
	case common.StatusRunning:
		return common.Icons.Running
	case common.StatusPending:
		return common.Icons.Pending
	case common.StatusError:
		return common.Icons.Delete
	default:
		return common.Icons.Clean
	}
}

func formatShortDuration(d time.Duration) string {
	if d < 0 {
		return "0s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}

func tabZoneID(idx int) string {
	return fmt.Sprintf("drawer-tab-%d", idx)
}

func toolbarZoneID(id string) string {
	return "drawer-toolbar-" + id
}

func processZoneID(idx int) string {
	return fmt.Sprintf("drawer-proc-%d", idx)
}

func logZoneID(id string) string {
	return "drawer-log-" + id
}

func approvalZoneID(idx int) string {
	return fmt.Sprintf("drawer-approval-%d", idx)
}

func approvalActionZoneID(id, action string) string {
	return fmt.Sprintf("drawer-approval-%s-%s", id, action)
}
