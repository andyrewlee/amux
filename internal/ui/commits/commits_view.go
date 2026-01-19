package commits

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// View renders the commit viewer
func (m *Model) View() string {
	if m.loading {
		return m.renderLoading()
	}

	if m.err != nil {
		return m.renderError()
	}

	if len(m.commits) == 0 {
		return m.renderEmpty()
	}

	return m.renderCommits()
}

func (m *Model) renderLoading() string {
	content := lipgloss.NewStyle().
		Foreground(common.ColorMuted).
		Render("Loading commits...")

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

func (m *Model) renderError() string {
	content := lipgloss.NewStyle().
		Foreground(common.ColorError).
		Render(fmt.Sprintf("Error: %v", m.err))

	// Center the error message in available space (minus help bar)
	centered := lipgloss.Place(m.width, m.height-1, lipgloss.Center, lipgloss.Center, content)
	return centered + "\n" + m.renderMinimalHelp()
}

func (m *Model) renderEmpty() string {
	content := lipgloss.NewStyle().
		Foreground(common.ColorMuted).
		Render("No commits found")

	// Center the message in available space (minus help bar)
	centered := lipgloss.Place(m.width, m.height-1, lipgloss.Center, lipgloss.Center, content)
	return centered + "\n" + m.renderMinimalHelp()
}

// renderMinimalHelp renders abbreviated help for empty/error states
func (m *Model) renderMinimalHelp() string {
	items := []common.HelpBinding{
		{Key: "q", Desc: "close"},
	}
	return common.RenderHelpBarItems(m.styles, items)
}

func (m *Model) renderCommits() string {
	var b strings.Builder

	// Header
	header := m.renderHeader()
	b.WriteString(header)
	b.WriteString("\n")

	// Commit list
	visible := m.visibleHeight()
	end := m.scrollOffset + visible
	if end > len(m.commits) {
		end = len(m.commits)
	}

	for i := m.scrollOffset; i < end; i++ {
		commit := m.commits[i]
		line := m.renderCommitLine(i, commit)
		b.WriteString(line)
		b.WriteString("\n")
	}

	// Pad remaining space
	for i := end - m.scrollOffset; i < visible; i++ {
		b.WriteString("\n")
	}

	// Help bar
	help := m.renderHelp()
	b.WriteString(help)

	return b.String()
}

func (m *Model) renderHeader() string {
	branch := m.branch
	if branch == "" {
		branch = "unknown"
	}

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(common.ColorPrimary).
		Render("Commits")

	branchStyle := lipgloss.NewStyle().
		Foreground(common.ColorSecondary).
		Render(fmt.Sprintf("(%s)", branch))

	countStyle := lipgloss.NewStyle().
		Foreground(common.ColorMuted).
		Render(fmt.Sprintf(" - %d commits", len(m.getValidIndices())))

	return title + " " + branchStyle + countStyle
}

func (m *Model) renderCommitLine(index int, commit git.Commit) string {
	// Graph-only line
	if commit.ShortHash == "" {
		graphStyle := lipgloss.NewStyle().
			Foreground(common.ColorMuted)
		return graphStyle.Render(commit.GraphLine)
	}

	isSelected := index == m.cursor
	separator := " "
	if isSelected {
		separator = lipgloss.NewStyle().
			Background(common.ColorSelection).
			Render(" ")
	}

	// Build the line
	var parts []string

	// Graph prefix
	graphStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
	if isSelected {
		graphStyle = graphStyle.Background(common.ColorSelection)
	}
	if commit.GraphLine != "" {
		parts = append(parts, graphStyle.Render(commit.GraphLine))
	}

	// Hash
	hashStyle := lipgloss.NewStyle().Foreground(common.ColorWarning)
	if isSelected {
		hashStyle = hashStyle.
			Bold(true).
			Background(common.ColorSelection)
	}
	parts = append(parts, hashStyle.Render(commit.ShortHash))

	// Subject (truncate if needed)
	subjectWidth := m.width - 50 // Leave room for other columns
	if subjectWidth < 20 {
		subjectWidth = 20
	}
	subject := truncateToWidth(commit.Subject, subjectWidth)
	subjectStyle := lipgloss.NewStyle().Foreground(common.ColorForeground)
	if isSelected {
		subjectStyle = subjectStyle.
			Bold(true).
			Background(common.ColorSelection)
	}
	parts = append(parts, subjectStyle.Render(subject))

	// Refs (branch/tag names)
	if commit.Refs != "" {
		refStyle := lipgloss.NewStyle().
			Foreground(common.ColorSecondary).
			Bold(true)
		if isSelected {
			refStyle = refStyle.Background(common.ColorSelection)
		}
		parts = append(parts, refStyle.Render(fmt.Sprintf("(%s)", commit.Refs)))
	}

	// Date
	dateStyle := lipgloss.NewStyle().Foreground(common.ColorMuted)
	if isSelected {
		dateStyle = dateStyle.Background(common.ColorSelection)
	}
	parts = append(parts, dateStyle.Render(commit.Date))

	// Author
	authorStyle := lipgloss.NewStyle().Foreground(common.ColorInfo)
	if isSelected {
		authorStyle = authorStyle.Background(common.ColorSelection)
	}
	parts = append(parts, authorStyle.Render("<"+commit.Author+">"))

	line := strings.Join(parts, separator)

	// Apply selection background
	if isSelected && m.width > 0 {
		lineWidth := lipgloss.Width(line)
		if lineWidth < m.width {
			padding := strings.Repeat(" ", m.width-lineWidth)
			line += lipgloss.NewStyle().
				Background(common.ColorSelection).
				Render(padding)
		}
	}

	return line
}

func (m *Model) renderHelp() string {
	items := []common.HelpBinding{
		{Key: "j/k", Desc: "navigate"},
		{Key: "Enter", Desc: "view diff"},
		{Key: "g/G", Desc: "top/bottom"},
		{Key: "q", Desc: "close"},
	}

	return common.RenderHelpBarItems(m.styles, items)
}

// truncateToWidth truncates a string to fit within maxWidth visual columns.
// Uses lipgloss.Width for proper UTF-8 and wide character handling.
func truncateToWidth(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}

	width := lipgloss.Width(s)
	if width <= maxWidth {
		return s
	}

	// Need to truncate - account for "..." suffix
	ellipsis := "..."
	ellipsisWidth := lipgloss.Width(ellipsis)
	targetWidth := maxWidth - ellipsisWidth
	if targetWidth <= 0 {
		return ellipsis[:maxWidth]
	}

	// Truncate rune by rune until we fit
	runes := []rune(s)
	for i := len(runes); i > 0; i-- {
		truncated := string(runes[:i])
		if lipgloss.Width(truncated) <= targetWidth {
			return truncated + ellipsis
		}
	}

	return ellipsis
}
