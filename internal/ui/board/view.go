package board

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/ui/common"
)

// View renders the board.
func (m *Model) View() string {
	contentWidth := m.width - 2
	if contentWidth < 1 {
		contentWidth = 1
	}
	contentHeight := m.height - 2
	if contentHeight < 1 {
		contentHeight = 1
	}

	toolbarLines := m.toolbarLines(contentWidth)
	if len(toolbarLines) > contentHeight-1 {
		toolbarLines = nil
	}
	if contentHeight-len(toolbarLines) < 1 {
		toolbarLines = nil
	}

	colWidth := clamp(contentWidth/4, 24, 36)
	if len(m.Columns) > 0 {
		colWidth = clamp(contentWidth/len(m.Columns), 24, 36)
	}
	if colWidth > contentWidth {
		colWidth = contentWidth
	}

	visibleCols := 1
	if colWidth > 0 {
		visibleCols = contentWidth / colWidth
		if visibleCols < 1 {
			visibleCols = 1
		}
	}
	if m.scrollX > len(m.Columns)-visibleCols {
		m.scrollX = max(0, len(m.Columns)-visibleCols)
	}
	if m.Selection.Column < m.scrollX {
		m.scrollX = m.Selection.Column
	}
	if m.Selection.Column >= m.scrollX+visibleCols {
		m.scrollX = m.Selection.Column - visibleCols + 1
	}

	helpLines := m.helpLines(contentWidth)
	if !m.showKeymapHints {
		helpLines = nil
	}
	if len(helpLines) > contentHeight-len(toolbarLines)-1 {
		helpLines = nil
	}

	columnHeight := contentHeight - len(toolbarLines) - len(helpLines)
	if columnHeight < 1 {
		columnHeight = 1
	}

	cols := []string{}
	for colIdx := m.scrollX; colIdx < len(m.Columns) && colIdx < m.scrollX+visibleCols; colIdx++ {
		col := m.Columns[colIdx]
		cols = append(cols, m.renderColumn(colIdx, col, colWidth, columnHeight))
	}
	board := lipgloss.JoinHorizontal(lipgloss.Top, cols...)

	if len(toolbarLines) > 0 {
		board = strings.Join(toolbarLines, "\n") + "\n" + board
	}
	if len(helpLines) > 0 {
		board = board + "\n" + strings.Join(helpLines, "\n")
	}

	style := m.styles.Pane
	if m.focused {
		style = m.styles.FocusedPane
	}
	return style.Width(m.width - 2).Render(board)
}

func (m *Model) renderColumn(colIdx int, col BoardColumn, width, height int) string {
	header := m.renderHeader(colIdx, col, width)

	cardAreaHeight := height - 1
	if cardAreaHeight < 1 {
		cardAreaHeight = 1
	}

	m.ensureScrollOffsets()
	offset := m.scrollOffsets[colIdx]
	if m.Selection.Column == colIdx {
		selectedRow := m.Selection.Row
		if selectedRow < offset {
			offset = selectedRow
		}
		if selectedRow >= offset+cardAreaHeight/2 {
			offset = selectedRow - cardAreaHeight/2
		}
		maxOffset := max(0, len(col.Cards)-cardAreaHeight/2)
		if offset > maxOffset {
			offset = maxOffset
		}
		m.scrollOffsets[colIdx] = offset
	}

	var rows []string
	for i := offset; i < len(col.Cards) && len(rows) < cardAreaHeight; i++ {
		selected := m.Selection.Column == colIdx && m.Selection.Row == i
		rows = append(rows, m.renderCard(colIdx, i, col.Cards[i], width, selected))
	}
	for len(rows) < cardAreaHeight {
		rows = append(rows, strings.Repeat(" ", width))
	}

	content := header + "\n" + strings.Join(rows, "\n")
	return lipgloss.NewStyle().Width(width).Render(content)
}

func (m *Model) renderHeader(colIdx int, col BoardColumn, width int) string {
	count := len(col.Cards)
	limit := m.wipLimitFor(col.Name)
	countLabel := itoa(count)
	if limit > 0 {
		countLabel = fmt.Sprintf("%d/%d", count, limit)
	}
	headerText := col.Name + " (" + countLabel + ")"
	dot := m.columnDot(col.Name)
	avail := max(1, width-2-lipgloss.Width(dot)-1)
	headerText = truncate(headerText, avail)
	textStyle := m.styles.Title
	if limit > 0 && count > limit {
		textStyle = m.styles.Warning
	}
	text := textStyle.Render(headerText)
	add := common.Icons.Add
	if m.zone != nil {
		text = m.zone.Mark(headerZoneID(colIdx), text)
		add = m.zone.Mark(headerAddZoneID(colIdx), add)
	}
	line := dot + " " + text + " " + add
	return padRight(line, width)
}

func (m *Model) renderCard(colIdx, rowIdx int, card IssueCard, width int, selected bool) string {
	line1 := card.Identifier
	if line1 != "" {
		line1 += " "
	}
	line1 += card.Title
	menu := ""
	if selected && width >= 4 {
		menu = " ..."
	}
	lineWidth := width
	if menu != "" {
		lineWidth = max(1, width-len(menu))
	}
	line1 = truncate(line1, lineWidth)
	if menu != "" {
		menuText := menu
		if m.zone != nil {
			menuText = m.zone.Mark(cardMenuZoneID(colIdx, rowIdx), menuText)
		}
		line1 = padRight(line1, lineWidth) + menuText
	}

	meta := []string{}
	if len(card.Labels) > 0 {
		labels := []string{}
		for _, label := range card.Labels {
			if label == "" {
				continue
			}
			r := []rune(label)
			labels = append(labels, string(r[0]))
		}
		if len(labels) > 0 {
			meta = append(meta, strings.Join(labels, ""))
		}
	}
	if card.Assignee != "" {
		meta = append(meta, initials(card.Assignee))
	}
	if len(card.Badges) > 0 {
		badges := make([]string, 0, len(card.Badges))
		for i, badge := range card.Badges {
			label := badge
			if m.zone != nil {
				label = m.zone.Mark(cardBadgeZoneID(colIdx, rowIdx, i), label)
			}
			badges = append(badges, label)
		}
		meta = append(meta, strings.Join(badges, " "))
	}
	if !card.UpdatedAt.IsZero() {
		meta = append(meta, relativeTimeShort(card.UpdatedAt))
	}
	line2 := truncate(strings.Join(meta, " • "), width)

	style := m.styles.WorkspaceRow
	if selected {
		style = m.styles.SelectedRow
	}
	content := style.Render(line1)
	if line2 != "" {
		content = content + "\n" + m.styles.Muted.Render(line2)
	}

	if m.zone != nil {
		content = m.zone.Mark(cardZoneID(colIdx, rowIdx), content)
	}
	return content
}

func (m *Model) helpLines(contentWidth int) []string {
	items := []string{
		common.RenderHelpItem(m.styles, "j/k", "up/down"),
		common.RenderHelpItem(m.styles, "h/l", "column"),
		common.RenderHelpItem(m.styles, "Enter", "open"),
		common.RenderHelpItem(m.styles, "s", "start"),
		common.RenderHelpItem(m.styles, "m", "move"),
		common.RenderHelpItem(m.styles, "a", "agent"),
		common.RenderHelpItem(m.styles, "d", "diff"),
		common.RenderHelpItem(m.styles, "p", "PR"),
		common.RenderHelpItem(m.styles, ".", "menu"),
		common.RenderHelpItem(m.styles, "A", "account"),
		common.RenderHelpItem(m.styles, "P", "project"),
		common.RenderHelpItem(m.styles, "L", "label"),
		common.RenderHelpItem(m.styles, "R", "recent"),
		common.RenderHelpItem(m.styles, "o", "auth"),
		common.RenderHelpItem(m.styles, "/", "search"),
		common.RenderHelpItem(m.styles, "f", "filter"),
		common.RenderHelpItem(m.styles, "c", "canceled"),
		common.RenderHelpItem(m.styles, "r", "refresh"),
	}
	return common.WrapHelpItems(items, contentWidth)
}

func (m *Model) toolbarLines(contentWidth int) []string {
	actions := toolbarActions()
	if len(actions) == 0 {
		return nil
	}

	selected := m.SelectedCard()
	parts := make([]string, 0, len(actions))
	for _, action := range actions {
		if action.ID == "auth" && len(m.authMissing) == 0 {
			continue
		}
		label := "[" + action.Label + "]"
		if action.ID == "filter" && m.Filters.ActiveOnly {
			label = "[Filter*]"
		}
		if action.ID == "canceled" && m.Filters.ShowCanceled {
			label = "[Canceled*]"
		}
		if action.ID == "account" && m.Filters.Account != "" {
			label = "[Account*]"
		}
		if action.ID == "project" && m.Filters.Project != "" {
			label = "[Project*]"
		}
		if action.ID == "label" && m.Filters.Label != "" {
			label = "[Label*]"
		}
		if action.ID == "recent" && m.Filters.UpdatedWithinDays > 0 {
			label = "[Recent*]"
		}
		enabled := !action.RequiresSelection || selected != nil
		if action.ID == "refresh" && m.backoffActive() {
			enabled = false
		}
		if !enabled {
			label = m.styles.Muted.Render(label)
		}
		if m.zone != nil {
			label = m.zone.Mark(toolbarZoneID(action.ID), label)
		}
		parts = append(parts, label)
	}
	if len(m.authMissing) > 0 {
		label := "[Auth Required]"
		label = m.styles.Warning.Render(label)
		parts = append(parts, label)
	}
	if !m.backoffUntil.IsZero() && time.Now().Before(m.backoffUntil) {
		label := "[Backoff " + m.backoffUntil.Format("15:04") + "]"
		label = m.styles.Muted.Render(label)
		parts = append(parts, label)
	}
	lines := common.WrapHelpItems(parts, contentWidth)
	for i := range lines {
		lines[i] = padRight(lines[i], contentWidth)
	}
	return lines
}

func cardZoneID(colIdx, rowIdx int) string {
	return "board-card-" + itoa(colIdx) + "-" + itoa(rowIdx)
}

func cardBadgeZoneID(colIdx, rowIdx, badgeIdx int) string {
	return "board-card-badge-" + itoa(colIdx) + "-" + itoa(rowIdx) + "-" + itoa(badgeIdx)
}

func headerZoneID(colIdx int) string {
	return "board-header-" + itoa(colIdx)
}

func headerAddZoneID(colIdx int) string {
	return "board-header-add-" + itoa(colIdx)
}

func toolbarZoneID(id string) string {
	return "board-toolbar-" + id
}

func cardMenuZoneID(colIdx, rowIdx int) string {
	return "board-card-menu-" + itoa(colIdx) + "-" + itoa(rowIdx)
}

func clamp(val, minVal, maxVal int) int {
	if val < minVal {
		return minVal
	}
	if val > maxVal {
		return maxVal
	}
	return val
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

func initials(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return ""
	}
	var out strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		r := []rune(part)
		out.WriteRune(r[0])
	}
	return out.String()
}

func (m *Model) columnDot(name string) string {
	color := common.ColorMuted
	switch strings.ToLower(name) {
	case "todo", "backlog":
		color = common.ColorMuted
	case "in progress", "doing", "started":
		color = common.ColorPrimary
	case "in review", "review":
		color = common.ColorWarning
	case "done", "completed":
		color = common.ColorSuccess
	}
	return lipgloss.NewStyle().Foreground(color).Render(common.Icons.Running)
}

func (m *Model) wipLimitFor(name string) int {
	if len(m.wipLimits) == 0 {
		return 0
	}
	for key, limit := range m.wipLimits {
		if strings.EqualFold(key, name) {
			return limit
		}
	}
	return 0
}

func relativeTimeShort(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	if d < time.Minute {
		return "now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	if d < 7*24*time.Hour {
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
}
