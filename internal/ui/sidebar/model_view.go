package sidebar

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// View renders the sidebar
func (m *Model) View() string {
	var b strings.Builder

	// Render changes directly
	b.WriteString(m.renderChanges())

	// Help bar
	contentWidth := m.width
	if contentWidth < 1 {
		contentWidth = 1
	}
	helpLines := m.helpLines(contentWidth)
	if !m.showKeymapHints {
		helpLines = nil
	}

	// Padding
	contentHeight := strings.Count(b.String(), "\n") + 1
	targetHeight := m.height - len(helpLines)
	if targetHeight < 0 {
		targetHeight = 0
	}
	if targetHeight > contentHeight {
		b.WriteString(strings.Repeat("\n", targetHeight-contentHeight))
	}
	b.WriteString(strings.Join(helpLines, "\n"))

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

// renderChanges renders the git changes with grouped display
func (m *Model) renderChanges() string {
	if m.gitStatus == nil {
		return m.styles.Muted.Render("No status loaded")
	}

	var b strings.Builder

	// Show branch info
	if m.workspace != nil && m.workspace.Branch != "" {
		b.WriteString(m.styles.Muted.Render("branch: "))
		b.WriteString(m.styles.BranchName.Render(m.workspace.Branch))
		b.WriteString("\n")
	}

	// Filter input when in filter mode
	if m.filterMode {
		b.WriteString(m.styles.Muted.Render("/"))
		b.WriteString(m.filterInput.View())
		b.WriteString("\n")
	} else if m.filterQuery != "" {
		// Show active filter
		b.WriteString(m.styles.Muted.Render("filter: "))
		b.WriteString(m.styles.BranchName.Render(m.filterQuery))
		b.WriteString("\n")
	}

	if m.gitStatus.Clean {
		b.WriteString("\n")
		b.WriteString(m.styles.StatusClean.Render(common.Icons.Clean + " Working tree clean"))
		return b.String()
	}

	// Show file count
	total := m.gitStatus.GetDirtyCount()
	b.WriteString(m.styles.Muted.Render(strconv.Itoa(total) + " changed files"))
	b.WriteString("\n")

	visibleHeight := m.visibleHeight()

	// Adjust scroll
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	}
	if m.cursor >= m.scrollOffset+visibleHeight {
		m.scrollOffset = m.cursor - visibleHeight + 1
	}

	for i, item := range m.displayItems {
		if i < m.scrollOffset {
			continue
		}
		if i >= m.scrollOffset+visibleHeight {
			break
		}

		if item.isHeader {
			// Section header
			headerStyle := m.styles.SidebarHeader
			b.WriteString(headerStyle.Render(item.header))
			b.WriteString("\n")
		} else {
			// File entry
			cursor := common.Icons.CursorEmpty + " "
			if i == m.cursor {
				cursor = common.Icons.Cursor + " "
			}

			// Status indicator with color
			var statusStyle lipgloss.Style
			switch item.change.Kind {
			case git.ChangeModified:
				statusStyle = m.styles.StatusModified
			case git.ChangeAdded:
				statusStyle = m.styles.StatusAdded
			case git.ChangeDeleted:
				statusStyle = m.styles.StatusDeleted
			case git.ChangeRenamed:
				statusStyle = m.styles.StatusRenamed
			case git.ChangeUntracked:
				statusStyle = m.styles.StatusUntracked
			default:
				statusStyle = m.styles.Muted
			}

			// Use single-char status code for consistent alignment
			statusCode := item.change.KindString()

			// Build the prefix (cursor + status code)
			prefix := cursor + statusStyle.Render(statusCode) + " "
			prefixWidth := lipgloss.Width(prefix)

			// Calculate max path width, leaving room for prefix
			maxPathWidth := m.width - prefixWidth
			if maxPathWidth < 5 {
				maxPathWidth = 5
			}

			// Truncate path from left to fit, showing end of path (most relevant part)
			displayPath := item.change.Path
			pathWidth := lipgloss.Width(displayPath)
			if pathWidth > maxPathWidth {
				// Remove characters from start until it fits
				runes := []rune(displayPath)
				for len(runes) > 4 && lipgloss.Width(string(runes)) > maxPathWidth-3 {
					runes = runes[1:]
				}
				displayPath = "..." + string(runes)
			}

			line := prefix + m.styles.FilePath.Render(displayPath)
			b.WriteString(line + "\n")
		}
	}

	return b.String()
}

func (m *Model) helpItem(key, desc string) string {
	return common.RenderHelpItem(m.styles, key, desc)
}

func (m *Model) helpLines(contentWidth int) []string {
	items := []string{
		m.helpItem("k/↑", "up"),
		m.helpItem("j/↓", "down"),
		m.helpItem("/", "filter"),
	}
	return common.WrapHelpItems(items, contentWidth)
}

func (m *Model) helpLineCount() int {
	if !m.showKeymapHints {
		return 0
	}
	contentWidth := m.width
	if contentWidth < 1 {
		contentWidth = 1
	}
	return len(m.helpLines(contentWidth))
}
