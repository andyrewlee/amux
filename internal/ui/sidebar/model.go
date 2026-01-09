package sidebar

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"

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
	width  int
	height int

	// Styles
	styles common.Styles
	zone   *zone.Manager
}

// New creates a new sidebar model
func New() *Model {
	return &Model{
		styles: common.DefaultStyles(),
	}
}

// SetZone sets the shared zone manager for click targets.
func (m *Model) SetZone(z *zone.Manager) {
	m.zone = z
}

// Init initializes the sidebar
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.MouseMsg:
		if !m.focused {
			return m, nil
		}

		if msg.Action == tea.MouseActionPress {
			if msg.Button == tea.MouseButtonWheelUp {
				m.moveCursor(-1)
				return m, nil
			}
			if msg.Button == tea.MouseButtonWheelDown {
				m.moveCursor(1)
				return m, nil
			}
		}

		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if m.zone != nil {
				if z := m.zone.Get("sidebar-help-up"); z != nil && z.InBounds(msg) {
					m.moveCursor(-1)
					return m, nil
				}
				if z := m.zone.Get("sidebar-help-down"); z != nil && z.InBounds(msg) {
					m.moveCursor(1)
					return m, nil
				}
				if z := m.zone.Get("sidebar-help-refresh"); z != nil && z.InBounds(msg) {
					cmds = append(cmds, m.refreshStatus())
					return m, tea.Batch(cmds...)
				}
				if z := m.zone.Get("sidebar-help-home"); z != nil && z.InBounds(msg) {
					return m, func() tea.Msg { return messages.ShowWelcome{} }
				}
				if z := m.zone.Get("sidebar-help-monitor"); z != nil && z.InBounds(msg) {
					return m, func() tea.Msg { return messages.ToggleMonitor{} }
				}
				if z := m.zone.Get("sidebar-help-help"); z != nil && z.InBounds(msg) {
					return m, func() tea.Msg { return messages.ToggleHelp{} }
				}
				if z := m.zone.Get("sidebar-help-quit"); z != nil && z.InBounds(msg) {
					return m, func() tea.Msg { return messages.ShowQuitDialog{} }
				}
				if z := m.zone.Get("sidebar-help-new-agent"); z != nil && z.InBounds(msg) {
					return m, func() tea.Msg { return messages.ShowSelectAssistantDialog{} }
				}
			}

			if m.zone != nil && m.gitStatus != nil {
				for i := range m.gitStatus.Files {
					id := fmt.Sprintf("sidebar-file-%d", i)
					if z := m.zone.Get(id); z != nil && z.InBounds(msg) {
						m.cursor = i
						return m, nil
					}
				}
			}
		}

	case tea.KeyMsg:
		if !m.focused {
			return m, nil
		}

		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
			m.moveCursor(1)
		case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
			m.moveCursor(-1)
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
		if m.zone != nil {
			line = m.zone.Mark(fmt.Sprintf("sidebar-file-%d", i), line)
		}
		b.WriteString(line + "\n")
	}

	return b.String()
}

func (m *Model) helpItem(id, key, desc string) string {
	item := common.RenderHelpItem(m.styles, key, desc)
	if id == "" || m.zone == nil {
		return item
	}
	return m.zone.Mark(id, item)
}

func (m *Model) helpLines(contentWidth int) []string {
	items := []string{
		m.helpItem("sidebar-help-up", "k/↑", "up"),
		m.helpItem("sidebar-help-down", "j/↓", "down"),
	}
	return common.WrapHelpItems(items, contentWidth)
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
