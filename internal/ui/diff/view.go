package diff

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// View renders the diff viewer
func (m *Model) View() string {
	if m.loading {
		return m.renderLoading()
	}

	if m.err != nil {
		return m.renderError()
	}

	if m.diff == nil {
		return m.renderEmpty()
	}

	if m.diff.Binary {
		return m.renderBinary()
	}

	if m.diff.Large {
		return m.renderLarge()
	}

	if m.diff.Empty || len(m.diff.Lines) == 0 {
		return m.renderNoChanges()
	}

	return m.renderDiff()
}

// renderLoading shows loading spinner
func (m *Model) renderLoading() string {
	var b strings.Builder

	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")

	loadingStyle := lipgloss.NewStyle().
		Foreground(common.ColorMuted()).
		Italic(true)
	b.WriteString(loadingStyle.Render("  Loading diff..."))

	return b.String()
}

// renderError shows error message
func (m *Model) renderError() string {
	var b strings.Builder

	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")

	errorStyle := lipgloss.NewStyle().
		Foreground(common.ColorError())
	b.WriteString(errorStyle.Render("  Error: " + m.err.Error()))

	return b.String()
}

// renderEmpty shows empty state
func (m *Model) renderEmpty() string {
	var b strings.Builder

	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")

	emptyStyle := lipgloss.NewStyle().
		Foreground(common.ColorMuted())
	b.WriteString(emptyStyle.Render("  No file selected"))

	return b.String()
}

// renderBinary shows binary file warning
func (m *Model) renderBinary() string {
	var b strings.Builder

	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")

	warningStyle := lipgloss.NewStyle().
		Foreground(common.ColorWarning()).
		Bold(true)
	b.WriteString(warningStyle.Render("  ⚠ Binary file - cannot display diff"))

	return b.String()
}

// renderLarge shows large file warning
func (m *Model) renderLarge() string {
	var b strings.Builder

	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")

	warningStyle := lipgloss.NewStyle().
		Foreground(common.ColorWarning()).
		Bold(true)
	b.WriteString(warningStyle.Render("  ⚠ File too large to display (> 2MB)"))

	return b.String()
}

// renderNoChanges shows no changes message
func (m *Model) renderNoChanges() string {
	var b strings.Builder

	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")

	emptyStyle := lipgloss.NewStyle().
		Foreground(common.ColorMuted())
	b.WriteString(emptyStyle.Render("  No changes to display"))

	return b.String()
}

// renderHeader renders the file header
func (m *Model) renderHeader() string {
	path := ""
	if m.change != nil {
		path = m.change.Path
	}

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(common.ColorPrimary())

	modeStr := ""
	switch m.mode {
	case git.DiffModeStaged:
		modeStr = " (staged)"
	case git.DiffModeUnstaged:
		modeStr = " (unstaged)"
	case git.DiffModeBranch:
		modeStr = " (branch)"
	}

	return headerStyle.Render(path + modeStr)
}

// renderDiff renders the actual diff content
func (m *Model) renderDiff() string {
	var b strings.Builder

	// Header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Stats line
	if m.diff != nil {
		added := m.diff.AddedLines()
		deleted := m.diff.DeletedLines()
		statsStyle := lipgloss.NewStyle().Foreground(common.ColorMuted())

		addStyle := lipgloss.NewStyle().Foreground(common.ColorSuccess())
		delStyle := lipgloss.NewStyle().Foreground(common.ColorError())

		stats := addStyle.Render("+"+strconv.Itoa(added)) + " " +
			delStyle.Render("-"+strconv.Itoa(deleted))

		if len(m.diff.Hunks) > 0 {
			stats += statsStyle.Render(fmt.Sprintf("  (%d hunks)", len(m.diff.Hunks)))
		}

		b.WriteString(stats)
		b.WriteString("\n")
	}

	// Visible lines
	visibleHeight := m.visibleHeight()
	lines := m.diff.Lines

	// Calculate visible range
	start := m.scroll
	end := start + visibleHeight
	if end > len(lines) {
		end = len(lines)
	}
	if start > len(lines) {
		start = len(lines)
	}

	// Line number width calculation
	lineNumWidth := len(strconv.Itoa(len(lines)))
	if lineNumWidth < 3 {
		lineNumWidth = 3
	}

	// Content width for wrapping
	contentWidth := m.width - lineNumWidth - 3 // minus gutter and padding
	if contentWidth < 20 {
		contentWidth = 20
	}

	// Render visible lines and count actual rows (including wrapped lines)
	actualRows := 0
	for i := start; i < end; i++ {
		line := lines[i]
		rendered := m.renderLine(i, line, lineNumWidth, contentWidth)
		// Count newlines within the rendered line (from wrapping) plus 1 for the line itself
		actualRows += strings.Count(rendered, "\n") + 1
		b.WriteString(rendered)
		if i < end-1 {
			b.WriteString("\n")
		}
	}

	// Pad to fill height using actual rendered rows
	for i := actualRows; i < visibleHeight; i++ {
		b.WriteString("\n")
	}

	// Footer with scroll info and keybindings
	b.WriteString("\n")
	b.WriteString(m.renderFooter())

	return b.String()
}

// renderLine renders a single diff line with colors
func (m *Model) renderLine(lineNum int, line git.DiffLine, numWidth, contentWidth int) string {
	// Line number gutter
	gutterStyle := lipgloss.NewStyle().
		Foreground(common.ColorMuted()).
		Width(numWidth).
		Align(lipgloss.Right)

	lineNumStr := gutterStyle.Render(strconv.Itoa(lineNum + 1))

	// Get line content and style based on type
	content := line.Content
	var contentStyle lipgloss.Style

	switch line.Kind {
	case git.DiffLineAdd:
		contentStyle = lipgloss.NewStyle().
			Foreground(common.ColorSuccess())
	case git.DiffLineDelete:
		contentStyle = lipgloss.NewStyle().
			Foreground(common.ColorError())
	case git.DiffLineHeader:
		contentStyle = lipgloss.NewStyle().
			Foreground(common.ColorInfo()).
			Bold(true)
	default:
		contentStyle = lipgloss.NewStyle().
			Foreground(common.ColorForeground())
	}

	// Handle line wrapping
	if m.wrap && len(content) > contentWidth {
		content = m.wrapLine(content, contentWidth)
	} else if len(content) > contentWidth {
		// Truncate with ellipsis
		if contentWidth > 3 {
			content = content[:contentWidth-3] + "..."
		}
	}

	return lineNumStr + " " + contentStyle.Render(content)
}

// wrapLine wraps a long line to fit within width
func (m *Model) wrapLine(content string, width int) string {
	if len(content) <= width {
		return content
	}

	var wrapped strings.Builder
	for i := 0; i < len(content); i += width {
		end := i + width
		if end > len(content) {
			end = len(content)
		}
		if i > 0 {
			wrapped.WriteString("\n    ") // Indent continuation lines
		}
		wrapped.WriteString(content[i:end])
	}
	return wrapped.String()
}

// renderFooter renders the footer with keybindings and scroll info
func (m *Model) renderFooter() string {
	footerStyle := lipgloss.NewStyle().
		Foreground(common.ColorMuted())

	var parts []string

	// Scroll position
	if m.diff != nil && len(m.diff.Lines) > 0 {
		total := len(m.diff.Lines)
		pos := m.scroll + 1
		if pos > total {
			pos = total
		}
		parts = append(parts, fmt.Sprintf("%d/%d", pos, total))
	}

	// Hunk info
	if m.diff != nil && len(m.diff.Hunks) > 0 {
		parts = append(parts, fmt.Sprintf("hunk %d/%d", m.hunkIdx+1, len(m.diff.Hunks)))
	}

	// Wrap indicator
	if m.wrap {
		parts = append(parts, "[wrap]")
	}

	// Keybindings
	keyStyle := lipgloss.NewStyle().Foreground(common.ColorPrimary())
	helpItems := []string{
		keyStyle.Render("j/k") + ":scroll",
		keyStyle.Render("n/p") + ":hunk",
		keyStyle.Render("w") + ":wrap",
		keyStyle.Render("q") + ":close",
	}

	return footerStyle.Render(strings.Join(parts, " | ")) + "  " + footerStyle.Render(strings.Join(helpItems, " "))
}
