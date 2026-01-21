package sidebar

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// displayItem represents a single item in the flat display list
// This combines section headers and file entries
type displayItem struct {
	isHeader bool
	header   string // For section headers like "Staged (2)"
	change   *git.Change
	mode     git.DiffMode // Which diff mode to use for this item
}

// Model is the Bubbletea model for the sidebar pane
type Model struct {
	// State
	worktree     *data.Worktree
	focused      bool
	gitStatus    *git.StatusResult
	cursor       int
	scrollOffset int

	// Filter mode
	filterMode  bool
	filterQuery string
	filterInput textinput.Model

	// Display list (flattened from grouped status)
	displayItems []displayItem

	// Layout
	width           int
	height          int
	showKeymapHints bool

	// Styles
	styles common.Styles
}

// New creates a new sidebar model
func New() *Model {
	ti := textinput.New()
	ti.Placeholder = "filter..."
	ti.CharLimit = 100

	return &Model{
		styles:      common.DefaultStyles(),
		filterInput: ti,
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

	// Handle filter input when in filter mode
	if m.filterMode {
		switch msg := msg.(type) {
		case tea.KeyPressMsg:
			switch {
			case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
				m.filterMode = false
				m.filterQuery = ""
				m.filterInput.SetValue("")
				m.filterInput.Blur()
				m.rebuildDisplayList()
				return m, nil
			case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
				m.filterMode = false
				m.filterInput.Blur()
				return m, nil
			default:
				newInput, cmd := m.filterInput.Update(msg)
				m.filterInput = newInput
				m.filterQuery = m.filterInput.Value()
				m.rebuildDisplayList()
				return m, cmd
			}
		}
	}

	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		if !m.focused {
			return m, nil
		}
		delta := common.ScrollDeltaForHeight(m.visibleHeight(), 10) // ~10% of visible
		if msg.Button == tea.MouseWheelUp {
			m.moveCursor(-delta)
			return m, nil
		}
		if msg.Button == tea.MouseWheelDown {
			m.moveCursor(delta)
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
			return m, m.openCurrentItem()
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
			cmds = append(cmds, m.openCurrentItem())
		case key.Matches(msg, key.NewBinding(key.WithKeys("g"))):
			cmds = append(cmds, m.refreshStatus())
		case key.Matches(msg, key.NewBinding(key.WithKeys("/"))):
			// Enter filter mode
			m.filterMode = true
			m.filterInput.Focus()
			return m, m.filterInput.Focus()
		}
	}

	return m, tea.Batch(cmds...)
}

// openCurrentItem opens the diff for the currently selected item
func (m *Model) openCurrentItem() tea.Cmd {
	if m.cursor < 0 || m.cursor >= len(m.displayItems) {
		return nil
	}

	item := m.displayItems[m.cursor]
	if item.isHeader || item.change == nil {
		return nil
	}

	change := item.change
	mode := item.mode
	wt := m.worktree

	return func() tea.Msg {
		return messages.OpenDiff{
			Change:   change,
			Mode:     mode,
			Worktree: wt,
		}
	}
}

// rebuildDisplayList rebuilds the flat display list from grouped status
func (m *Model) rebuildDisplayList() {
	m.displayItems = nil

	if m.gitStatus == nil || m.gitStatus.Clean {
		return
	}

	// Filter function
	matchesFilter := func(c *git.Change) bool {
		if m.filterQuery == "" {
			return true
		}
		return strings.Contains(strings.ToLower(c.Path), strings.ToLower(m.filterQuery))
	}

	// Count matching items
	stagedCount := 0
	for i := range m.gitStatus.Staged {
		if matchesFilter(&m.gitStatus.Staged[i]) {
			stagedCount++
		}
	}
	unstagedCount := 0
	for i := range m.gitStatus.Unstaged {
		if matchesFilter(&m.gitStatus.Unstaged[i]) {
			unstagedCount++
		}
	}
	untrackedCount := 0
	for i := range m.gitStatus.Untracked {
		if matchesFilter(&m.gitStatus.Untracked[i]) {
			untrackedCount++
		}
	}

	// Add Staged section
	if stagedCount > 0 {
		m.displayItems = append(m.displayItems, displayItem{
			isHeader: true,
			header:   "Staged (" + strconv.Itoa(stagedCount) + ")",
		})
		for i := range m.gitStatus.Staged {
			if matchesFilter(&m.gitStatus.Staged[i]) {
				m.displayItems = append(m.displayItems, displayItem{
					change: &m.gitStatus.Staged[i],
					mode:   git.DiffModeStaged,
				})
			}
		}
	}

	// Add Unstaged section
	if unstagedCount > 0 {
		m.displayItems = append(m.displayItems, displayItem{
			isHeader: true,
			header:   "Unstaged (" + strconv.Itoa(unstagedCount) + ")",
		})
		for i := range m.gitStatus.Unstaged {
			if matchesFilter(&m.gitStatus.Unstaged[i]) {
				m.displayItems = append(m.displayItems, displayItem{
					change: &m.gitStatus.Unstaged[i],
					mode:   git.DiffModeUnstaged,
				})
			}
		}
	}

	// Add Untracked section
	if untrackedCount > 0 {
		m.displayItems = append(m.displayItems, displayItem{
			isHeader: true,
			header:   "Untracked (" + strconv.Itoa(untrackedCount) + ")",
		})
		for i := range m.gitStatus.Untracked {
			if matchesFilter(&m.gitStatus.Untracked[i]) {
				m.displayItems = append(m.displayItems, displayItem{
					change: &m.gitStatus.Untracked[i],
					mode:   git.DiffModeUnstaged,
				})
			}
		}
	}

	// Reset cursor if it's out of bounds
	if m.cursor >= len(m.displayItems) {
		m.cursor = len(m.displayItems) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}

	// Skip to first non-header item
	for m.cursor < len(m.displayItems) && m.displayItems[m.cursor].isHeader {
		m.cursor++
	}
	if m.cursor >= len(m.displayItems) && len(m.displayItems) > 0 {
		m.cursor = len(m.displayItems) - 1
	}
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

// renderChanges renders the git changes with grouped display
func (m *Model) renderChanges() string {
	if m.gitStatus == nil {
		return m.styles.Muted.Render("No status loaded")
	}

	var b strings.Builder

	// Show branch info
	if m.worktree != nil && m.worktree.Branch != "" {
		b.WriteString(m.styles.Muted.Render("branch: "))
		b.WriteString(m.styles.BranchName.Render(m.worktree.Branch))
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
	b.WriteString("\n\n")

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

func (m *Model) listHeaderLines() int {
	if m.gitStatus == nil || m.gitStatus.Clean {
		return 0
	}
	header := 0
	if m.worktree != nil && m.worktree.Branch != "" {
		header++
	}
	if m.filterMode || m.filterQuery != "" {
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
	if len(m.displayItems) == 0 {
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
	if index < 0 || index >= len(m.displayItems) {
		return -1, false
	}
	return index, true
}

// moveCursor moves the cursor, skipping section headers
func (m *Model) moveCursor(delta int) {
	if len(m.displayItems) == 0 {
		return
	}

	newCursor := m.cursor + delta

	// Skip headers when moving
	for newCursor >= 0 && newCursor < len(m.displayItems) && m.displayItems[newCursor].isHeader {
		if delta > 0 {
			newCursor++
		} else {
			newCursor--
		}
	}

	// Clamp to valid range
	if newCursor < 0 {
		newCursor = 0
		// Find first non-header
		for newCursor < len(m.displayItems) && m.displayItems[newCursor].isHeader {
			newCursor++
		}
	}
	if newCursor >= len(m.displayItems) {
		newCursor = len(m.displayItems) - 1
		// Find last non-header
		for newCursor >= 0 && m.displayItems[newCursor].isHeader {
			newCursor--
		}
	}

	if newCursor >= 0 && newCursor < len(m.displayItems) {
		m.cursor = newCursor
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
	// Exit filter mode when losing focus
	if m.filterMode {
		m.filterMode = false
		m.filterInput.Blur()
	}
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
	m.filterQuery = ""
	m.filterInput.SetValue("")
	m.rebuildDisplayList()
}

// SetGitStatus sets the git status
func (m *Model) SetGitStatus(status *git.StatusResult) {
	m.gitStatus = status
	m.rebuildDisplayList()
}
