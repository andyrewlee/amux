package sidebar

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/ui/common"
)

// View renders the project tree
func (m *ProjectTree) View() string {
	if m.workspace == nil {
		return m.renderWithHelp(m.styles.Muted.Render("No workspace selected"))
	}

	if len(m.flatNodes) == 0 {
		return m.renderWithHelp(m.styles.Muted.Render("Empty directory"))
	}

	var b strings.Builder
	visibleHeight := m.visibleHeight()

	// Adjust scroll
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	}
	if m.cursor >= m.scrollOffset+visibleHeight {
		m.scrollOffset = m.cursor - visibleHeight + 1
	}

	for i, node := range m.flatNodes {
		if i < m.scrollOffset {
			continue
		}
		if i >= m.scrollOffset+visibleHeight {
			break
		}

		// Cursor indicator
		cursor := common.Icons.CursorEmpty + " "
		if i == m.cursor {
			cursor = common.Icons.Cursor + " "
		}

		// Indentation
		indent := strings.Repeat("  ", node.Depth)

		// Icon
		var icon string
		if node.IsDir {
			if node.Expanded {
				icon = common.Icons.DirOpen + " "
			} else {
				icon = common.Icons.DirClosed + " "
			}
		} else {
			icon = common.Icons.File + " "
		}

		// Name with styling
		name := node.Name
		maxNameWidth := m.width - lipgloss.Width(cursor+indent+icon) - 1
		if maxNameWidth < 5 {
			maxNameWidth = 5
		}
		if lipgloss.Width(name) > maxNameWidth {
			runes := []rune(name)
			for len(runes) > 4 && lipgloss.Width(string(runes)) > maxNameWidth-3 {
				runes = runes[:len(runes)-1]
			}
			name = string(runes) + "..."
		}

		var nameStyled string
		if node.IsDir {
			nameStyled = m.styles.DirName.Render(name)
		} else {
			nameStyled = m.styles.FilePath.Render(name)
		}

		line := cursor + indent + icon + nameStyled
		b.WriteString(line + "\n")
	}

	content := b.String()
	if len(content) > 0 && content[len(content)-1] == '\n' {
		content = content[:len(content)-1]
	}

	return m.renderWithHelp(content)
}

func (m *ProjectTree) renderWithHelp(content string) string {
	// Help bar
	contentWidth := m.width
	if contentWidth < 1 {
		contentWidth = 1
	}
	helpLines := m.helpLines(contentWidth)
	if !m.showKeymapHints {
		helpLines = nil
	}

	contentHeight := 0
	if content != "" {
		contentHeight = strings.Count(content, "\n") + 1
	}

	targetHeight := m.height - len(helpLines)
	if targetHeight < 0 {
		targetHeight = 0
	}

	var b strings.Builder
	b.WriteString(content)
	if targetHeight > contentHeight {
		b.WriteString(strings.Repeat("\n", targetHeight-contentHeight))
	}
	if len(helpLines) > 0 {
		if content != "" && targetHeight == contentHeight {
			b.WriteString("\n")
		}
		b.WriteString(strings.Join(helpLines, "\n"))
	}

	// Ensure output doesn't exceed m.height lines
	result := b.String()
	if m.height > 0 {
		lines := strings.Split(result, "\n")
		if len(lines) > m.height {
			lines = lines[:m.height]
			result = strings.Join(lines, "\n")
		}
	}
	return result
}

func (m *ProjectTree) helpItem(key, desc string) string {
	return common.RenderHelpItem(m.styles, key, desc)
}

func (m *ProjectTree) helpLines(contentWidth int) []string {
	items := []string{
		m.helpItem("k/↑", "up"),
		m.helpItem("j/↓", "down"),
		m.helpItem("h/←", "collapse"),
		m.helpItem("l/→", "expand"),
		m.helpItem(".", "hidden"),
		m.helpItem("r", "refresh"),
	}
	return common.WrapHelpItems(items, contentWidth)
}

func (m *ProjectTree) helpLineCount() int {
	if !m.showKeymapHints {
		return 0
	}
	contentWidth := m.width
	if contentWidth < 1 {
		contentWidth = 1
	}
	return len(m.helpLines(contentWidth))
}
