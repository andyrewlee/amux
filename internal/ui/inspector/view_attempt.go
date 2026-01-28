package inspector

import (
	"fmt"
	"strings"

	"github.com/andyrewlee/amux/internal/ui/common"
)

func (m *Model) renderAttemptView(width int) string {
	var b strings.Builder
	b.WriteString(m.styles.Subtitle.Render("Attempt Logs"))
	b.WriteString("\n")
	if len(m.Logs) == 0 {
		b.WriteString(m.styles.Muted.Render("Logs appear in the main pane while the agent runs."))
		b.WriteString("\n\n")
	} else {
		logs := m.Logs
		if len(logs) > 8 {
			logs = logs[len(logs)-8:]
		}
		for _, entry := range logs {
			b.WriteString(truncate(m.renderLogEntry(entry, width), width))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if m.NextActionSummary != "" {
		b.WriteString(m.styles.Subtitle.Render("Next Actions"))
		b.WriteString("\n")
		summary := m.NextActionSummary
		if m.NextActionStatus != "" {
			summary = fmt.Sprintf("%s (%s)", summary, m.NextActionStatus)
		}
		b.WriteString(truncate(summary, width))
		b.WriteString("\n")
		actions := []string{
			"[Diff]",
			"[Open Editor]",
		}
		if m.ScriptRunning {
			actions = append(actions, "[Stop Server]")
		} else {
			actions = append(actions, "[Start Server]")
		}
		if m.HasWorktree {
			actions = append(actions, "[Run Setup]")
		}
		if m.zone != nil {
			actions[0] = m.zone.Mark(gitActionZoneID("diff"), actions[0])
			actions[1] = m.zone.Mark(quickActionZoneID("open_editor"), actions[1])
			if m.ScriptRunning {
				actions[2] = m.zone.Mark(quickActionZoneID("stop_server"), actions[2])
			} else {
				actions[2] = m.zone.Mark(quickActionZoneID("start_server"), actions[2])
			}
			if m.HasWorktree {
				actions[len(actions)-1] = m.zone.Mark(quickActionZoneID("run_setup"), actions[len(actions)-1])
			}
		}
		b.WriteString(truncate(strings.Join(actions, " "), width))
		b.WriteString("\n\n")
	}
	if m.QueuedMessage != "" {
		b.WriteString(m.styles.Warning.Render("Queued message"))
		b.WriteString("\n")
		line := truncate(m.QueuedMessage, width)
		if m.zone != nil {
			line = m.zone.Mark(queueZoneID("cancel"), "[Cancel] ") + line
		} else {
			line = "[Cancel] " + line
		}
		b.WriteString(truncate(line, width))
		b.WriteString("\n\n")
	}
	if m.ReviewPreview != "" {
		b.WriteString(m.styles.Subtitle.Render("Review Comments"))
		b.WriteString("\n")
		lines := strings.Split(strings.TrimSpace(m.ReviewPreview), "\n")
		if len(lines) > 6 {
			lines = lines[:6]
		}
		for _, line := range lines {
			b.WriteString(truncate(line, width))
			b.WriteString("\n")
		}
		clearLabel := "[Clear]"
		if m.zone != nil {
			clearLabel = m.zone.Mark(reviewZoneID("clear"), clearLabel)
		}
		b.WriteString(truncate(clearLabel, width))
		b.WriteString("\n")
		b.WriteString("\n")
	}
	todos := extractTodos(m.Logs)
	if len(todos) > 0 {
		b.WriteString(m.styles.Subtitle.Render("TODOs"))
		b.WriteString("\n")
		for _, todo := range todos {
			b.WriteString(truncate("- "+todo, width))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(m.styles.Subtitle.Render("Follow-up"))
	b.WriteString("\n")
	profile := strings.TrimSpace(m.AgentProfile)
	if profile == "" {
		profile = "default"
	}
	controls := []string{
		fmt.Sprintf("[Variant: %s]", profile),
		"[Attach]",
		"[Insert PR comments]",
	}
	if m.zone != nil {
		controls[0] = m.zone.Mark(composerZoneID("variant"), controls[0])
		controls[1] = m.zone.Mark(composerZoneID("attach"), controls[1])
		controls[2] = m.zone.Mark(composerZoneID("insert_pr"), controls[2])
	}
	b.WriteString(truncate(strings.Join(controls, " "), width))
	b.WriteString("\n")
	b.WriteString(m.composer.View())
	return b.String()
}

func (m *Model) renderLogEntry(entry common.ActivityEntry, width int) string {
	icon := activityStatusIcon(entry.Status)
	prefix := common.ActivityPrefix(entry.Kind)
	summary := entry.Summary
	if summary == "" && len(entry.Details) > 0 {
		summary = strings.Join(entry.Details, " ")
	}
	line := fmt.Sprintf("%s %s %s", icon, prefix, summary)
	line = truncate(line, width)
	if entry.Kind == common.ActivityPlan || entry.Kind == common.ActivitySummary {
		return m.styles.Bold.Render(line)
	}
	return line
}

func (m *Model) helpLines(contentWidth int) []string {
	items := []string{
		common.RenderHelpItem(m.styles, "Enter", "run"),
		common.RenderHelpItem(m.styles, "c", "comment"),
		common.RenderHelpItem(m.styles, "t", "attempts"),
		common.RenderHelpItem(m.styles, "g", "diff"),
		common.RenderHelpItem(m.styles, "m", "move"),
		common.RenderHelpItem(m.styles, "p", "PR"),
		common.RenderHelpItem(m.styles, "b", "rebase"),
		common.RenderHelpItem(m.styles, "x", "resolve"),
	}
	if m.Mode == ModeAttempt {
		items = append(items,
			common.RenderHelpItem(m.styles, "v", "variant"),
			common.RenderHelpItem(m.styles, "a", "attach"),
			common.RenderHelpItem(m.styles, "i", "insert PR comments"),
		)
	}
	return common.WrapHelpItems(items, contentWidth)
}

func (m *Model) renderHeaderToggles() string {
	if m.Issue == nil {
		return ""
	}
	toggles := []struct {
		ID     string
		Label  string
		Active bool
	}{
		{ID: "preview", Label: "Preview", Active: m.auxMode == 1},
		{ID: "diff", Label: "Diff", Active: m.auxMode == 2},
	}
	parts := make([]string, 0, len(toggles))
	for _, toggle := range toggles {
		label := "[" + toggle.Label + "]"
		if toggle.Active {
			label = m.styles.Bold.Render(label)
		} else {
			label = m.styles.Muted.Render(label)
		}
		if m.zone != nil {
			label = m.zone.Mark(toggleZoneID(toggle.ID), label)
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, " ")
}

func (m *Model) renderGitActions(width int) string {
	if width <= 0 {
		return ""
	}
	if !m.HasWorktree {
		return ""
	}
	type action struct {
		ID     string
		Label  string
		Active bool
	}
	actions := []action{
		{ID: "diff", Label: "Diff"},
		{ID: "pr", Label: "PR"},
		{ID: "push", Label: "Push"},
		{ID: "merge", Label: "Merge"},
		{ID: "rebase", Label: "Rebase"},
		{ID: "base", Label: "Base"},
	}
	if m.Conflict {
		actions = append(actions, action{ID: "resolve", Label: "Resolve", Active: true})
	}
	parts := make([]string, 0, len(actions))
	for _, act := range actions {
		label := "[" + act.Label + "]"
		style := m.styles.Muted
		if act.Active {
			style = m.styles.Warning
		}
		disabled := m.AgentRunning && act.ID != "resolve"
		if m.Conflict && act.ID != "resolve" {
			disabled = true
		}
		if disabled {
			style = m.styles.Muted
		}
		if m.zone != nil {
			label = m.zone.Mark(gitActionZoneID(act.ID), label)
		}
		parts = append(parts, style.Render(label))
	}
	line := strings.Join(parts, " ")
	return truncate(line, width)
}

func (m *Model) renderGitBar(width int) string {
	if width <= 0 {
		return ""
	}
	if m.GitInfo.Branch == "" {
		return ""
	}
	parts := []string{
		fmt.Sprintf("%s %s %s", m.GitInfo.Branch, common.Icons.ArrowRight, m.GitInfo.Base),
	}
	if m.GitInfo.Summary != "" {
		parts = append(parts, m.GitInfo.Summary)
	}
	if m.GitInfo.Ahead > 0 || m.GitInfo.Behind > 0 {
		parts = append(parts, fmt.Sprintf("+%d -%d", m.GitInfo.Ahead, m.GitInfo.Behind))
	}
	if m.Conflict {
		parts = append(parts, "conflicts")
	}
	if m.GitInfo.RebaseInProgress {
		parts = append(parts, "rebase")
	}
	line := "Git: " + strings.Join(parts, " • ")
	return m.styles.Muted.Render(truncate(line, width))
}
