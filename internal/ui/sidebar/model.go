package sidebar

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/keymap"
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
	width  int
	height int

	// Styles
	styles common.Styles

	keymap keymap.KeyMap
}

// New creates a new sidebar model
func New(km keymap.KeyMap) *Model {
	return &Model{
		styles: common.DefaultStyles(),
		keymap: km,
	}
}

// Init initializes the sidebar
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if !m.focused {
			return m, nil
		}

		switch {
		case key.Matches(msg, m.keymap.SidebarDown):
			m.moveCursor(1)
		case key.Matches(msg, m.keymap.SidebarUp):
			m.moveCursor(-1)
		case key.Matches(msg, m.keymap.SidebarRefresh):
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
	helpItems := []string{
		m.styles.HelpKey.Render(keymap.PairHint(m.keymap.SidebarDown, m.keymap.SidebarUp)) + m.styles.HelpDesc.Render(":nav"),
		m.styles.HelpKey.Render(keymap.PrimaryKey(m.keymap.SidebarRefresh)) + m.styles.HelpDesc.Render(":refresh"),
	}

	// Padding
	contentHeight := strings.Count(b.String(), "\n") + 1
	targetHeight := m.height - 1
	if targetHeight < 0 {
		targetHeight = 0
	}
	if targetHeight > contentHeight {
		b.WriteString(strings.Repeat("\n", targetHeight-contentHeight))
	}
	b.WriteString(strings.Join(helpItems, "  "))

	return b.String()
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

	visibleHeight := m.height - 10
	if visibleHeight < 1 {
		visibleHeight = 1
	}

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

		// Truncate path to fit within available width
		prefixWidth := lipgloss.Width(cursor + file.Code + " ")
		maxPathLen := m.width - prefixWidth
		if maxPathLen < 10 {
			maxPathLen = 10
		}
		displayPath := file.Path
		if len(displayPath) > maxPathLen {
			displayPath = "..." + displayPath[len(displayPath)-maxPathLen+3:]
		}

		line := cursor + statusStyle.Render(file.Code) + " " + m.styles.FilePath.Render(displayPath)
		b.WriteString(line + "\n")
	}

	return b.String()
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
