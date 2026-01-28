package sidebar

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

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
	workspace    *data.Workspace
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

	return m, common.SafeBatch(cmds...)
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
	ws := m.workspace

	return func() tea.Msg {
		return messages.OpenDiff{
			Change:    change,
			Mode:      mode,
			Workspace: ws,
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
	for i := range m.gitStatus.Untracked {
		if matchesFilter(&m.gitStatus.Untracked[i]) {
			unstagedCount++
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

	// Add Unstaged section (includes both modified and untracked files)
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

func (m *Model) listHeaderLines() int {
	if m.gitStatus == nil || m.gitStatus.Clean {
		return 0
	}
	header := 0
	if m.workspace != nil && m.workspace.Branch != "" {
		header++
	}
	if m.filterMode || m.filterQuery != "" {
		header++
	}
	header += 1 // "changed files"
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
	if m.workspace == nil {
		return nil
	}

	root := m.workspace.Root
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

// SetWorkspace sets the active workspace
func (m *Model) SetWorkspace(ws *data.Workspace) {
	m.workspace = ws
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
