package sidebar

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// Model is the Bubbletea model for the sidebar pane
type Model struct {
	// State
	worktree     *data.Worktree
	focused      bool
	gitStatus    *git.StatusResult
	cursor       int
	scrollOffset int

	// Layout
	width           int
	height          int
	showKeymapHints bool

	// Styles
	styles common.Styles
}

// New creates a new sidebar model
func New() *Model {
	return &Model{
		styles: common.DefaultStyles(),
	}
}

// SetShowKeymapHints controls whether helper text is rendered.
func (m *Model) SetShowKeymapHints(show bool) {
	m.showKeymapHints = show
}

// SetStyles updates the component's styles (for theme changes).
func (m *Model) SetStyles(styles common.Styles) {
	m.styles = styles
}

// Init initializes the sidebar
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		if !m.focused {
			return m, nil
		}
		if msg.Button == tea.MouseWheelUp {
			m.moveCursor(-1)
			return m, nil
		}
		if msg.Button == tea.MouseWheelDown {
			m.moveCursor(1)
			return m, nil
		}

	case tea.MouseClickMsg:
		if !m.focused {
			return m, nil
		}
		if msg.Button == tea.MouseLeft {
			idx, ok := m.rowIndexAt(msg.Y)
			if !ok {
				return m, nil
			}
			m.cursor = idx
			if m.gitStatus != nil && idx < len(m.gitStatus.Files) {
				file := m.gitStatus.Files[idx]
				return m, func() tea.Msg {
					return messages.OpenDiff{File: file.Path, StatusCode: file.Code, Worktree: m.worktree}
				}
			}
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
		case key.Matches(msg, key.NewBinding(key.WithKeys("enter", "space", "o"))):
			if m.gitStatus != nil && m.cursor >= 0 && m.cursor < len(m.gitStatus.Files) {
				file := m.gitStatus.Files[m.cursor]
				cmds = append(cmds, func() tea.Msg {
					return messages.OpenDiff{File: file.Path, StatusCode: file.Code, Worktree: m.worktree}
				})
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("g"))):
			cmds = append(cmds, m.refreshStatus())
		}
	}

	return m, tea.Batch(cmds...)
}

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

// renderChanges renders the git changes
func (m *Model) renderChanges() string {
	if m.gitStatus == nil {
		return m.styles.Muted.Render("No status loaded")
	}

	var b strings.Builder

	// Show branch info if available
	if m.worktree != nil && m.worktree.Branch != "" {
		b.WriteString(m.styles.Muted.Render("branch: "))
		b.WriteString(m.styles.BranchName.Render(m.worktree.Branch))
		b.WriteString("\n")
	}

	if m.gitStatus.Clean {
		b.WriteString("\n")
		b.WriteString(m.styles.StatusClean.Render(common.Icons.Clean + " Working tree clean"))
		return b.String()
	}

	// Show file count
	b.WriteString(m.styles.Muted.Render(strconv.Itoa(len(m.gitStatus.Files)) + " changed files"))
	b.WriteString("\n\n")

	visibleHeight := m.visibleHeight()

	// Adjust scroll
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	}
	if m.cursor >= m.scrollOffset+visibleHeight {
		m.scrollOffset = m.cursor - visibleHeight + 1
	}

	for i, file := range m.gitStatus.Files {
		if i < m.scrollOffset {
			continue
		}
		if i >= m.scrollOffset+visibleHeight {
			break
		}

		cursor := common.Icons.CursorEmpty + " "
		if i == m.cursor {
			cursor = common.Icons.Cursor + " "
		}

		// Status indicator with color
		var statusStyle lipgloss.Style
		switch {
		case file.IsModified():
			statusStyle = m.styles.StatusModified
		case file.IsAdded():
			statusStyle = m.styles.StatusAdded
		case file.IsDeleted():
			statusStyle = m.styles.StatusDeleted
		case file.IsUntracked():
			statusStyle = m.styles.StatusUntracked
		default:
			statusStyle = m.styles.Muted
		}

		// Use display code (shows "A " for untracked instead of "??")
		displayCode := file.DisplayCode()

		// Build the prefix (cursor + status code)
		prefix := cursor + statusStyle.Render(displayCode) + " "
		prefixWidth := lipgloss.Width(prefix)

		// Calculate max path width, leaving room for prefix
		maxPathWidth := m.width - prefixWidth
		if maxPathWidth < 5 {
			maxPathWidth = 5
		}

		// Truncate path from left to fit, showing end of path (most relevant part)
		displayPath := file.Path
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

	return b.String()
}

func (m *Model) helpItem(key, desc string) string {
	return common.RenderHelpItem(m.styles, key, desc)
}

func (m *Model) helpLines(contentWidth int) []string {
	items := []string{
		m.helpItem("k/↑", "up"),
		m.helpItem("j/↓", "down"),
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

func (m *Model) listHeaderLines() int {
	if m.gitStatus == nil || m.gitStatus.Clean {
		return 0
	}
	header := 0
	if m.worktree != nil && m.worktree.Branch != "" {
		header++
	}
	header += 2 // "changed files" + blank line
	return header
}

func (m *Model) visibleHeight() int {
	header := m.listHeaderLines()
	help := m.helpLineCount()
	visible := m.height - header - help
	if visible < 1 {
		visible = 1
	}
	return visible
}

func (m *Model) rowIndexAt(screenY int) (int, bool) {
	if m.gitStatus == nil || m.gitStatus.Clean {
		return -1, false
	}
	header := m.listHeaderLines()
	help := m.helpLineCount()
	contentHeight := m.height - help
	if screenY < header || screenY >= contentHeight {
		return -1, false
	}
	rowY := screenY - header
	if rowY < 0 || rowY >= m.visibleHeight() {
		return -1, false
	}
	index := m.scrollOffset + rowY
	if index < 0 || index >= len(m.gitStatus.Files) {
		return -1, false
	}
	return index, true
}

// moveCursor moves the cursor
func (m *Model) moveCursor(delta int) {
	maxLen := 0
	if m.gitStatus != nil {
		maxLen = len(m.gitStatus.Files)
	}

	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= maxLen {
		m.cursor = maxLen - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// refreshStatus refreshes the git status
func (m *Model) refreshStatus() tea.Cmd {
	if m.worktree == nil {
		return nil
	}

	root := m.worktree.Root
	return func() tea.Msg {
		status, err := git.GetStatus(root)
		return messages.GitStatusResult{Root: root, Status: status, Err: err}
	}
}

// SetSize sets the sidebar size
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Focus sets the focus state
func (m *Model) Focus() {
	m.focused = true
}

// Blur removes focus
func (m *Model) Blur() {
	m.focused = false
}

// Focused returns whether the sidebar is focused
func (m *Model) Focused() bool {
	return m.focused
}

// SetWorktree sets the active worktree
func (m *Model) SetWorktree(wt *data.Worktree) {
	m.worktree = wt
	m.cursor = 0
	m.scrollOffset = 0
}

// SetGitStatus sets the git status
func (m *Model) SetGitStatus(status *git.StatusResult) {
	m.gitStatus = status
}
