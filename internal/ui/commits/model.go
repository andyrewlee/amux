package commits

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// Model is the Bubble Tea model for the commit viewer
type Model struct {
	// Data
	commits  []git.Commit
	worktree *data.Worktree

	// State
	cursor       int
	scrollOffset int
	focused      bool
	loading      bool
	err          error

	// Layout
	width  int
	height int
	branch string

	// Dependencies
	styles common.Styles
}

// New creates a new commit viewer model
func New(wt *data.Worktree, width, height int) *Model {
	return &Model{
		worktree: wt,
		width:    width,
		height:   height,
		branch:   "unknown",
		styles:   common.DefaultStyles(),
		loading:  true,
	}
}

// SetFocused sets whether the commit viewer is focused
func (m *Model) SetFocused(focused bool) {
	m.focused = focused
}

// Focus sets the commit viewer as focused
func (m *Model) Focus() {
	m.focused = true
}

// Blur removes focus from the commit viewer
func (m *Model) Blur() {
	m.focused = false
}

// Focused returns whether the commit viewer is focused
func (m *Model) Focused() bool {
	return m.focused
}

// SetSize sets the commit viewer dimensions
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SetStyles updates the component's styles (for theme changes).
func (m *Model) SetStyles(styles common.Styles) {
	m.styles = styles
}

// commitsLoaded is sent when commits have been loaded
type commitsLoaded struct {
	commits []git.Commit
	branch  string
	err     error
}

// Init initializes the model and starts loading commits
func (m *Model) Init() tea.Cmd {
	return m.loadCommits()
}

// loadCommits returns a command that loads commits asynchronously
func (m *Model) loadCommits() tea.Cmd {
	wt := m.worktree
	return func() tea.Msg {
		if wt == nil {
			return commitsLoaded{err: fmt.Errorf("no worktree selected")}
		}

		log, err := git.GetCommitLog(wt.Root, 100)
		if err != nil {
			return commitsLoaded{err: err}
		}

		branch := "unknown"
		if b, err := git.GetCurrentBranch(wt.Root); err == nil {
			branch = b
		}

		return commitsLoaded{commits: log.Commits, branch: branch}
	}
}

// Update handles messages
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	switch msg := msg.(type) {
	case commitsLoaded:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.commits = msg.commits
		if msg.branch != "" {
			m.branch = msg.branch
		}
		return m, nil

	case tea.MouseWheelMsg:
		if !m.focused {
			return m, nil
		}

		// Wheel scroll
		if msg.Button == tea.MouseWheelUp {
			m.moveCursor(-3)
			return m, nil
		}
		if msg.Button == tea.MouseWheelDown {
			m.moveCursor(3)
			return m, nil
		}

		// Click on commit rows (only check visible rows for efficiency)
	case tea.MouseClickMsg:
		if !m.focused {
			return m, nil
		}
		if msg.Button == tea.MouseLeft {
			idx, ok := m.rowIndexAt(msg.Y)
			if !ok {
				return m, nil
			}
			if idx < 0 || idx >= len(m.commits) {
				return m, nil
			}
			if m.commits[idx].ShortHash == "" {
				return m, nil
			}
			m.cursor = idx
			return m, m.openSelectedCommit()
		}

	case tea.KeyPressMsg:
		if !m.focused {
			return m, nil
		}

		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
			m.moveCursor(1)
		case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
			m.moveCursor(-1)
		case key.Matches(msg, key.NewBinding(key.WithKeys("g", "home"))):
			m.cursor = 0
			m.scrollOffset = 0
		case key.Matches(msg, key.NewBinding(key.WithKeys("G", "end"))):
			m.goToEnd()
		case key.Matches(msg, key.NewBinding(key.WithKeys("pgup"))):
			m.moveCursor(-m.visibleHeight())
		case key.Matches(msg, key.NewBinding(key.WithKeys("pgdown"))):
			m.moveCursor(m.visibleHeight())
		case key.Matches(msg, key.NewBinding(key.WithKeys("enter", " "))):
			return m, m.openSelectedCommit()
		case key.Matches(msg, key.NewBinding(key.WithKeys("q", "esc"))):
			return m, func() tea.Msg { return messages.CloseTab{} }
		}
	}

	return m, nil
}

// moveCursor moves the cursor by delta, handling scroll offset
func (m *Model) moveCursor(delta int) {
	// Find next valid cursor position (skip graph-only lines)
	newCursor := m.cursor + delta

	// Find valid commit lines only
	validIndices := m.getValidIndices()
	if len(validIndices) == 0 {
		return
	}

	// Find the closest valid index
	if delta > 0 {
		// Moving down
		for _, idx := range validIndices {
			if idx > m.cursor {
				newCursor = idx
				break
			}
		}
		// If we didn't find one, stay at current or go to last
		if newCursor <= m.cursor && len(validIndices) > 0 {
			newCursor = validIndices[len(validIndices)-1]
		}
	} else {
		// Moving up
		for i := len(validIndices) - 1; i >= 0; i-- {
			if validIndices[i] < m.cursor {
				newCursor = validIndices[i]
				break
			}
		}
		// If we didn't find one, stay at current or go to first
		if newCursor >= m.cursor && len(validIndices) > 0 {
			newCursor = validIndices[0]
		}
	}

	// Clamp to valid range
	if newCursor < 0 {
		newCursor = 0
	}
	if newCursor >= len(m.commits) {
		newCursor = len(m.commits) - 1
	}

	m.cursor = newCursor
	m.adjustScroll()
}

// getValidIndices returns indices of commits (not graph-only lines)
func (m *Model) getValidIndices() []int {
	var indices []int
	for i, c := range m.commits {
		if c.ShortHash != "" {
			indices = append(indices, i)
		}
	}
	return indices
}

// goToEnd moves cursor to the last commit
func (m *Model) goToEnd() {
	valid := m.getValidIndices()
	if len(valid) > 0 {
		m.cursor = valid[len(valid)-1]
		m.adjustScroll()
	}
}

// adjustScroll adjusts scroll offset to keep cursor visible
func (m *Model) adjustScroll() {
	visible := m.visibleHeight()
	if visible <= 0 {
		return
	}

	// Scroll up if cursor is above visible area
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	}

	// Scroll down if cursor is below visible area
	if m.cursor >= m.scrollOffset+visible {
		m.scrollOffset = m.cursor - visible + 1
	}
}

// visibleHeight returns the number of visible commit rows
func (m *Model) visibleHeight() int {
	// Account for header and help bar
	return m.height - 4
}

func (m *Model) rowIndexAt(screenY int) (int, bool) {
	headerLines := 1
	visible := m.visibleHeight()
	if screenY < headerLines || screenY >= headerLines+visible {
		return -1, false
	}
	index := m.scrollOffset + (screenY - headerLines)
	if index < 0 || index >= len(m.commits) {
		return -1, false
	}
	return index, true
}

// openSelectedCommit returns a command to open the selected commit's diff
func (m *Model) openSelectedCommit() tea.Cmd {
	if m.cursor < 0 || m.cursor >= len(m.commits) {
		return nil
	}

	commit := m.commits[m.cursor]
	if commit.ShortHash == "" {
		return nil // Graph-only line
	}

	wt := m.worktree
	hash := commit.FullHash
	if hash == "" {
		hash = commit.ShortHash
	}
	return func() tea.Msg {
		return messages.ViewCommitDiff{
			Worktree: wt,
			Hash:     hash,
		}
	}
}

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
