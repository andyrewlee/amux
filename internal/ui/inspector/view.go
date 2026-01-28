package inspector

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/ui/common"
)

// View renders the inspector.
func (m *Model) View() string {
	contentWidth := m.width - 2
	if contentWidth < 1 {
		contentWidth = 1
	}

	var b strings.Builder
	b.WriteString(m.renderHeader(contentWidth))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", contentWidth))
	b.WriteString("\n")

	if m.Mode == ModeTask {
		b.WriteString(m.renderTaskView(contentWidth))
	} else {
		b.WriteString(m.renderAttemptView(contentWidth))
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

func (m *Model) renderHeader(width int) string {
	if m.Issue == nil {
		return m.styles.Title.Render("No issue selected")
	}
	titleText := fmt.Sprintf("%s %s", m.Issue.Identifier, m.Issue.Title)
	if m.AttemptBranch != "" {
		titleText = fmt.Sprintf("%s %s %s", titleText, common.Icons.ArrowRight, m.AttemptBranch)
	}
	toggles := m.renderHeaderToggles()
	if toggles != "" && width > lipgloss.Width(toggles)+1 {
		avail := width - lipgloss.Width(toggles) - 1
		titleText = truncate(titleText, avail)
		title := padRight(m.styles.Title.Render(titleText), avail)
		if m.zone != nil {
			title = m.zone.Mark(breadcrumbZoneID(), title)
		}
		return title + " " + toggles
	}
	title := truncate(m.styles.Title.Render(titleText), width)
	if m.zone != nil {
		title = m.zone.Mark(breadcrumbZoneID(), title)
	}
	return title
}

func (m *Model) renderTaskView(width int) string {
	var b strings.Builder
	if m.Issue == nil {
		b.WriteString(m.styles.Muted.Render("Select an issue from the board"))
		return b.String()
	}

	stateLine := fmt.Sprintf("State: %s", m.Issue.State.Name)
	assignee := "Unassigned"
	if m.Issue.Assignee != nil {
		assignee = m.Issue.Assignee.Name
	}
	b.WriteString(m.styles.Muted.Render(stateLine))
	b.WriteString("\n")
	assigneeLine := assignee
	if m.zone != nil && assignee != "Unassigned" {
		assigneeLine = m.zone.Mark(assigneeZoneID(), assigneeLine)
	}
	b.WriteString(m.styles.Muted.Render("Assignee: " + assigneeLine))
	b.WriteString("\n")
	labelsLine := ""
	if len(m.Issue.Labels) > 0 {
		labels := make([]string, 0, len(m.Issue.Labels))
		for i, label := range m.Issue.Labels {
			name := strings.TrimSpace(label.Name)
			if name == "" {
				continue
			}
			if m.zone != nil {
				name = m.zone.Mark(labelZoneID(i), name)
			}
			labels = append(labels, name)
		}
		if len(labels) > 0 {
			labelsLine = "Labels: " + strings.Join(labels, ", ")
		}
	} else {
		labelsLine = "Labels: none"
	}
	if labelsLine != "" {
		b.WriteString(m.styles.Muted.Render(labelsLine))
		b.WriteString("\n")
	}
	if m.Issue.Project != nil {
		b.WriteString(m.styles.Muted.Render("Project: " + m.Issue.Project.Name))
		b.WriteString("\n")
	}
	if m.RepoName != "" {
		b.WriteString(m.styles.Muted.Render("Repo: " + m.RepoName))
		b.WriteString("\n")
	}
	if m.ParentAttempt != "" {
		b.WriteString(m.styles.Muted.Render("Parent attempt: " + m.ParentAttempt))
		b.WriteString("\n")
	}
	if m.GitInfo.Branch != "" {
		b.WriteString(m.renderGitBar(width))
		b.WriteString("\n")
		b.WriteString(m.renderGitActions(width))
		b.WriteString("\n")
	} else if m.GitLine != "" {
		b.WriteString(m.styles.Muted.Render(truncate(m.GitLine, width)))
		b.WriteString("\n")
		b.WriteString(m.renderGitActions(width))
		b.WriteString("\n")
	}
	if m.AuthRequired {
		b.WriteString("\n")
		b.WriteString(m.styles.Warning.Render("Auth required for this account"))
		b.WriteString("\n")
	}
	if m.PRURL != "" {
		b.WriteString("\n")
		label := "PR"
		if m.PRNumber > 0 {
			label = fmt.Sprintf("PR #%d", m.PRNumber)
		}
		state := strings.ToUpper(strings.TrimSpace(m.PRState))
		if state == "" {
			state = "UNKNOWN"
		}
		line := fmt.Sprintf("%s (%s)", label, state)
		if m.zone != nil {
			line = m.zone.Mark(prZoneID(), line)
		}
		b.WriteString(m.styles.Muted.Render(truncate(line, width)))
		b.WriteString("\n")
	}

	if m.Conflict {
		b.WriteString("\n")
		b.WriteString(m.styles.Warning.Render("Rebase conflicts detected"))
		b.WriteString("\n")
		conflictActions := []struct {
			ID    string
			Label string
		}{
			{ID: "resolve", Label: "Resolve"},
			{ID: "open", Label: "Open Editor"},
			{ID: "abort", Label: "Abort Rebase"},
		}
		var parts []string
		for _, action := range conflictActions {
			label := "[" + action.Label + "]"
			if m.zone != nil {
				label = m.zone.Mark(conflictZoneID(action.ID), label)
			}
			parts = append(parts, m.styles.Muted.Render(label))
		}
		b.WriteString(truncate(strings.Join(parts, " "), width))
		b.WriteString("\n")
	}

	if strings.EqualFold(m.Issue.State.Type, "review") {
		b.WriteString("\n")
		b.WriteString(m.styles.Bold.Render("Review ready"))
		b.WriteString("\n")
		reviewActions := []struct {
			ID    string
			Label string
		}{
			{ID: "diff", Label: "View Diff"},
			{ID: "feedback", Label: "Send Feedback"},
			{ID: "done", Label: "Move Done"},
		}
		var parts []string
		for _, action := range reviewActions {
			label := "[" + action.Label + "]"
			if m.zone != nil {
				label = m.zone.Mark(reviewZoneID(action.ID), label)
			}
			parts = append(parts, m.styles.Muted.Render(label))
		}
		line := strings.Join(parts, " ")
		b.WriteString(truncate(line, width))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	desc := strings.TrimSpace(m.Issue.Description)
	if desc == "" {
		desc = "No description"
	}
	b.WriteString(truncate(desc, width))
	b.WriteString("\n\n")

	b.WriteString(m.styles.Subtitle.Render("Attempts"))
	b.WriteString("\n")
	if len(m.Attempts) == 0 {
		b.WriteString(m.styles.Muted.Render("No attempts yet"))
		b.WriteString("\n")
	} else {
		for _, attempt := range m.Attempts {
			status := attempt.Status
			if status != "" {
				status = strings.ReplaceAll(status, "_", " ")
			}
			row := fmt.Sprintf("%s  %s  %s", attempt.Executor, attempt.Branch, attempt.Updated)
			if status != "" {
				row = fmt.Sprintf("%s  %s", row, status)
			}
			b.WriteString(truncate(row, width))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(m.styles.Subtitle.Render("Comments"))
	b.WriteString("\n")
	if len(m.Comments) == 0 {
		b.WriteString(m.styles.Muted.Render("No comments yet"))
		b.WriteString("\n")
	} else {
		for _, comment := range m.Comments {
			meta := commentMeta(comment)
			b.WriteString(m.styles.Muted.Render(truncate(meta, width)))
			b.WriteString("\n")
			body := strings.TrimSpace(comment.Body)
			if body == "" {
				body = "(no content)"
			}
			b.WriteString(truncate(body, width))
			b.WriteString("\n\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(m.styles.Subtitle.Render("Actions"))
	b.WriteString("\n")
	for i, action := range m.Actions {
		prefix := "  "
		style := m.styles.Body
		if i == m.Cursor {
			prefix = common.Icons.Cursor + " "
			style = m.styles.SelectedRow
		}
		label := action.Label
		line := prefix + label
		if m.zone != nil {
			line = m.zone.Mark(actionZoneID(i), line)
		}
		line = truncate(line, width)
		enabled := action.Enabled
		if action.ID == "send_message" && m.Mode != ModeAttempt {
			enabled = false
		}
		if action.ID == "mark_done" && (m.Issue == nil || !strings.EqualFold(m.Issue.State.Type, "review")) {
			enabled = false
		}
		if action.ID == "insert_pr_comments" && (m.Mode != ModeAttempt || m.PRURL == "") {
			enabled = false
		}
		if action.ID == "start_server" && (!m.HasWorktree || m.ScriptRunning) {
			enabled = false
		}
		if action.ID == "stop_server" && (!m.HasWorktree || !m.ScriptRunning) {
			enabled = false
		}
		if m.AgentRunning && (action.ID == "pr" || action.ID == "rebase") {
			enabled = false
		}
		if m.Conflict && (action.ID == "pr" || action.ID == "rebase") {
			enabled = false
		}
		if action.ID == "rehydrate" && m.HasWorktree {
			enabled = false
		}
		if action.ID == "cancel_queue" && m.QueuedMessage == "" {
			enabled = false
		}
		if action.ID == "open_editor" && !m.HasWorktree {
			enabled = false
		}
		if action.ID == "create_subtask" && (!m.HasWorktree || m.AuthRequired) {
			enabled = false
		}
		if m.AuthRequired && actionRequiresAuth(action.ID) {
			enabled = false
		}
		lineStyle := style
		if !enabled {
			lineStyle = m.styles.Muted
		}
		b.WriteString(lineStyle.Render(line))
		b.WriteString("\n")
	}

	return b.String()
}
