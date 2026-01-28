package diffview

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/diff"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// View renders diff view.
func (m *Model) View() string {
	contentWidth := m.width - 2
	if contentWidth < 1 {
		contentWidth = 1
	}

	headerLines, helpLines, visibleHeight := m.layout(contentWidth)

	lines := m.flatten()
	if m.selected < m.scroll {
		m.scroll = m.selected
	}
	if m.selected >= m.scroll+visibleHeight {
		m.scroll = m.selected - visibleHeight + 1
	}

	start := m.scroll
	end := min(len(lines), start+visibleHeight)

	var b strings.Builder
	for i, line := range headerLines {
		b.WriteString(line)
		if i < len(headerLines)-1 {
			b.WriteString("\n")
		}
	}
	if len(headerLines) > 0 {
		b.WriteString("\n")
	}
	for i := start; i < end; i++ {
		line := lines[i]
		text := m.renderLine(line, contentWidth)
		if i == m.selected {
			text = m.styles.SelectedRow.Render(text)
		}
		if m.zone != nil {
			text = m.zone.Mark(lineZoneID(i), text)
		}
		b.WriteString(text)
		b.WriteString("\n")
	}

	if len(helpLines) > 0 {
		b.WriteString(strings.Join(helpLines, "\n"))
	}

	style := m.styles.Pane
	if m.focused {
		style = m.styles.FocusedPane
	}
	return style.Width(m.width - 2).Render(b.String())
}

func (m *Model) layout(contentWidth int) ([]string, []string, int) {
	headerLines := m.headerLines(contentWidth)
	helpLines := m.helpLines(contentWidth)
	if !m.showKeymapHints {
		helpLines = nil
	}

	contentHeight := m.height - 2
	if contentHeight < 1 {
		contentHeight = 1
	}
	visibleHeight := contentHeight - len(headerLines) - len(helpLines)
	if visibleHeight < 1 {
		visibleHeight = 1
	}
	return headerLines, helpLines, visibleHeight
}

func (m *Model) headerLines(contentWidth int) []string {
	added, deleted := 0, 0
	for _, file := range m.Files {
		added += file.Added
		deleted += file.Deleted
	}
	summary := truncate(fmt.Sprintf("Diff (%d files, +%d -%d)", len(m.Files), added, deleted), contentWidth)
	toolbar := m.toolbarLine(contentWidth)
	if toolbar == "" {
		return []string{summary}
	}
	return []string{summary, toolbar}
}

func (m *Model) toolbarLine(contentWidth int) string {
	type toolbarButton struct {
		ID     string
		Label  string
		Active bool
		Toggle bool
	}

	label := "Unified"
	if !m.unified {
		label = "Split"
	}
	collapseAllLabel := "Collapse All"
	if m.allCollapsed() {
		collapseAllLabel = "Expand All"
	}
	buttons := []toolbarButton{
		{ID: "close", Label: "Close"},
		{ID: "unified", Label: label, Active: m.unified, Toggle: true},
		{ID: "ignore", Label: "Ignore WS", Active: m.ignoreWhitespace, Toggle: true},
		{ID: "wrap", Label: "Wrap", Active: m.wrap, Toggle: true},
		{ID: "collapse", Label: "Collapse"},
		{ID: "collapse-all", Label: collapseAllLabel},
		{ID: "comment", Label: "Comment"},
		{ID: "open", Label: "Open"},
	}

	var included []toolbarButton
	lineWidth := 0
	for _, btn := range buttons {
		text := "[" + btn.Label + "]"
		if lineWidth > 0 {
			lineWidth += 1
		}
		lineWidth += lipgloss.Width(text)
		if lineWidth > contentWidth {
			break
		}
		included = append(included, btn)
	}
	if len(included) == 0 {
		return ""
	}

	parts := make([]string, 0, len(included))
	for _, btn := range included {
		text := "[" + btn.Label + "]"
		style := m.styles.Body
		if btn.Toggle {
			if btn.Active {
				style = m.styles.Bold
			} else {
				style = m.styles.Muted
			}
		}
		text = style.Render(text)
		if m.zone != nil {
			text = m.zone.Mark(toolbarZoneID(btn.ID), text)
		}
		parts = append(parts, text)
	}

	line := strings.Join(parts, " ")
	return padRight(line, contentWidth)
}

func (m *Model) helpLines(contentWidth int) []string {
	items := []string{
		common.RenderHelpItem(m.styles, "u", "Unified/Split"),
		common.RenderHelpItem(m.styles, "i", "Ignore whitespace"),
		common.RenderHelpItem(m.styles, "w", "Wrap"),
		common.RenderHelpItem(m.styles, "x", "Collapse"),
		common.RenderHelpItem(m.styles, "c", "Comment"),
		common.RenderHelpItem(m.styles, "o", "Open"),
	}
	return common.WrapHelpItems(items, contentWidth)
}

func lineZoneID(idx int) string {
	return fmt.Sprintf("diff-line-%d", idx)
}

func toolbarZoneID(id string) string {
	return "diff-toolbar-" + id
}

func (m *Model) renderLine(line diff.Line, width int) string {
	if width <= 0 {
		return ""
	}
	if line.Type == diff.LineComment {
		text := "  " + line.Text
		if !m.wrap {
			text = truncate(text, width)
		}
		return m.styles.Muted.Render(text)
	}
	if m.unified || line.Type == diff.LineHeader || line.Type == diff.LineHunk {
		text := line.Text
		if !m.wrap {
			text = truncate(text, width)
		}
		return text
	}
	sep := " │ "
	colWidth := (width - len(sep)) / 2
	if colWidth < 4 {
		text := line.Text
		if !m.wrap {
			text = truncate(text, width)
		}
		return text
	}
	left := ""
	right := ""
	content := lineContent(line)
	switch line.Type {
	case diff.LineAdd:
		right = fmt.Sprintf("%4d %s", line.NewLine, content)
	case diff.LineDel:
		left = fmt.Sprintf("%4d %s", line.OldLine, content)
	case diff.LineContext:
		left = fmt.Sprintf("%4d %s", line.OldLine, content)
		right = fmt.Sprintf("%4d %s", line.NewLine, content)
	default:
		text := line.Text
		if !m.wrap {
			text = truncate(text, width)
		}
		return text
	}
	if !m.wrap {
		left = truncate(left, colWidth)
		right = truncate(right, colWidth)
	}
	left = padRight(left, colWidth)
	right = padRight(right, colWidth)
	return left + sep + right
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
