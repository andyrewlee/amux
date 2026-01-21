package branchfiles

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// View renders the branch files view
func (m *Model) View() string {
	if m.loading {
		return m.renderLoading()
	}

	if m.err != nil {
		return m.renderError()
	}

	if m.branchDiff == nil || len(m.branchDiff.Files) == 0 {
		return m.renderEmpty()
	}

	return m.renderFiles()
}

// renderLoading shows loading state
func (m *Model) renderLoading() string {
	var b strings.Builder

	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")

	loadingStyle := lipgloss.NewStyle().
		Foreground(common.ColorMuted).
		Italic(true)
	b.WriteString(loadingStyle.Render("  Loading branch files..."))

	return b.String()
}

// renderError shows error message
func (m *Model) renderError() string {
	var b strings.Builder

	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")

	errorStyle := lipgloss.NewStyle().
		Foreground(common.ColorError)
	b.WriteString(errorStyle.Render("  Error: " + m.err.Error()))

	return b.String()
}

// renderEmpty shows empty state
func (m *Model) renderEmpty() string {
	var b strings.Builder

	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")

	emptyStyle := lipgloss.NewStyle().
		Foreground(common.ColorMuted)
	b.WriteString(emptyStyle.Render("  No files changed on this branch"))

	return b.String()
}

// renderHeader renders the header with branch info
func (m *Model) renderHeader() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(common.ColorPrimary)

	branchStyle := lipgloss.NewStyle().
		Foreground(common.ColorSecondary)

	mutedStyle := lipgloss.NewStyle().
		Foreground(common.ColorMuted)

	branchName := "current branch"
	if m.worktree != nil && m.worktree.Branch != "" {
		branchName = m.worktree.Branch
	}

	baseName := "main"
	if m.baseBranch != "" {
		baseName = m.baseBranch
	}

	return titleStyle.Render("Files Changed") + " " +
		mutedStyle.Render("(") +
		branchStyle.Render(branchName) +
		mutedStyle.Render(" vs ") +
		branchStyle.Render(baseName) +
		mutedStyle.Render(")")
}

// renderFiles renders the list of changed files
func (m *Model) renderFiles() string {
	var b strings.Builder

	// Header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Summary stats
	if m.branchDiff != nil {
		statsStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
		addStyle := lipgloss.NewStyle().Foreground(common.ColorSuccess)
		delStyle := lipgloss.NewStyle().Foreground(common.ColorError)

		stats := statsStyle.Render(fmt.Sprintf("%d files changed, ", len(m.branchDiff.Files))) +
			addStyle.Render(fmt.Sprintf("+%d", m.branchDiff.TotalAdded)) +
			statsStyle.Render(" ") +
			delStyle.Render(fmt.Sprintf("-%d", m.branchDiff.TotalDeleted))
		b.WriteString(stats)
	}
	b.WriteString("\n\n")

	// File list
	visibleHeight := m.visibleHeight()
	files := m.branchDiff.Files

	start := m.scroll
	end := start + visibleHeight
	if end > len(files) {
		end = len(files)
	}

	for i := start; i < end; i++ {
		file := files[i]
		b.WriteString(m.renderFileRow(i, file))
		if i < end-1 {
			b.WriteString("\n")
		}
	}

	// Pad to fill height
	renderedRows := end - start
	for i := renderedRows; i < visibleHeight; i++ {
		b.WriteString("\n")
	}

	// Footer
	b.WriteString("\n")
	b.WriteString(m.renderFooter())

	return b.String()
}

// renderFileRow renders a single file row
func (m *Model) renderFileRow(idx int, file git.BranchFile) string {
	// Cursor indicator
	cursor := "  "
	if idx == m.cursor {
		cursor = common.Icons.Cursor + " "
	}

	// Status indicator with color
	var statusStyle lipgloss.Style
	var statusChar string
	switch file.Kind {
	case git.ChangeAdded:
		statusStyle = m.styles.StatusAdded
		statusChar = "A"
	case git.ChangeDeleted:
		statusStyle = m.styles.StatusDeleted
		statusChar = "D"
	case git.ChangeRenamed:
		statusStyle = m.styles.StatusRenamed
		statusChar = "R"
	case git.ChangeCopied:
		statusStyle = m.styles.StatusRenamed
		statusChar = "C"
	default:
		statusStyle = m.styles.StatusModified
		statusChar = "M"
	}

	status := statusStyle.Render(statusChar)

	// File path
	pathStyle := m.styles.FilePath
	if idx == m.cursor {
		pathStyle = pathStyle.Bold(true)
	}

	path := file.Path
	if file.OldPath != "" && file.OldPath != file.Path {
		path = file.OldPath + " → " + file.Path
	}

	// Truncate path if needed
	maxPathWidth := m.width - 20 // Leave room for stats
	if len(path) > maxPathWidth && maxPathWidth > 10 {
		path = "..." + path[len(path)-maxPathWidth+3:]
	}

	pathStr := pathStyle.Render(path)

	// Line stats (right-aligned)
	addStyle := lipgloss.NewStyle().Foreground(common.ColorSuccess)
	delStyle := lipgloss.NewStyle().Foreground(common.ColorError)

	addStr := ""
	delStr := ""
	if file.AddedLines > 0 {
		addStr = addStyle.Render("+" + strconv.Itoa(file.AddedLines))
	}
	if file.DeletedLines > 0 {
		delStr = delStyle.Render("-" + strconv.Itoa(file.DeletedLines))
	}

	stats := ""
	if addStr != "" || delStr != "" {
		stats = " " + addStr
		if addStr != "" && delStr != "" {
			stats += " "
		}
		stats += delStr
	}

	return cursor + status + "  " + pathStr + stats
}

// renderFooter renders the footer with keybindings
func (m *Model) renderFooter() string {
	footerStyle := lipgloss.NewStyle().
		Foreground(common.ColorMuted)

	var parts []string

	// Position indicator
	if m.branchDiff != nil && len(m.branchDiff.Files) > 0 {
		parts = append(parts, fmt.Sprintf("%d/%d", m.cursor+1, len(m.branchDiff.Files)))
	}

	// Keybindings
	keyStyle := lipgloss.NewStyle().Foreground(common.ColorPrimary)
	helpItems := []string{
		keyStyle.Render("j/k") + ":move",
		keyStyle.Render("Enter") + ":open",
		keyStyle.Render("r") + ":refresh",
		keyStyle.Render("q") + ":close",
	}

	return footerStyle.Render(strings.Join(parts, " | ")) + "  " + footerStyle.Render(strings.Join(helpItems, " "))
}
