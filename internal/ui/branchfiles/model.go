package branchfiles

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// Model is the Bubble Tea model for the branch files view
// Shows GitHub-style "Files Changed" for the current branch vs base
type Model struct {
	// Data
	worktree   *data.Worktree
	branchDiff *git.BranchDiff
	baseBranch string

	// State
	cursor  int
	scroll  int
	loading bool
	err     error
	focused bool

	// Layout
	width  int
	height int

	// Styles
	styles common.Styles
}

// filesLoaded is sent when the branch files have been loaded
type filesLoaded struct {
	diff *git.BranchDiff
	err  error
}

// New creates a new branch files model
func New(wt *data.Worktree, width, height int) *Model {
	return &Model{
		worktree: wt,
		loading:  true,
		width:    width,
		height:   height,
		styles:   common.DefaultStyles(),
	}
}

// Init initializes the model and starts loading files
func (m *Model) Init() tea.Cmd {
	return m.loadFiles()
}

// loadFiles returns a command that loads branch files asynchronously
func (m *Model) loadFiles() tea.Cmd {
	wt := m.worktree

	return func() tea.Msg {
		if wt == nil {
			return filesLoaded{err: nil, diff: &git.BranchDiff{}}
		}

		diff, err := git.GetBranchFiles(wt.Root)
		return filesLoaded{diff: diff, err: err}
	}
}

// Update handles messages
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	switch msg := msg.(type) {
	case filesLoaded:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.branchDiff = msg.diff
		if msg.diff != nil {
			m.baseBranch = msg.diff.BaseBranch
		}
		return m, nil

	case tea.MouseWheelMsg:
		if !m.focused {
			return m, nil
		}
		if msg.Button == tea.MouseWheelUp {
			m.moveCursor(-3)
			return m, nil
		}
		if msg.Button == tea.MouseWheelDown {
			m.moveCursor(3)
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
			return m, m.openSelectedFile()
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
			m.scroll = 0
		case key.Matches(msg, key.NewBinding(key.WithKeys("G", "end"))):
			m.goToEnd()
		case key.Matches(msg, key.NewBinding(key.WithKeys("pgup"))):
			m.moveCursor(-m.visibleHeight())
		case key.Matches(msg, key.NewBinding(key.WithKeys("pgdown"))):
			m.moveCursor(m.visibleHeight())
		case key.Matches(msg, key.NewBinding(key.WithKeys("enter", " "))):
			return m, m.openSelectedFile()
		case key.Matches(msg, key.NewBinding(key.WithKeys("q", "esc"))):
			return m, func() tea.Msg { return messages.CloseTab{} }
		case key.Matches(msg, key.NewBinding(key.WithKeys("r"))):
			// Refresh
			m.loading = true
			return m, m.loadFiles()
		}
	}

	return m, nil
}

// moveCursor moves the cursor by delta, clamping to valid range
func (m *Model) moveCursor(delta int) {
	if m.branchDiff == nil || len(m.branchDiff.Files) == 0 {
		return
	}

	m.cursor += delta
	maxCursor := len(m.branchDiff.Files) - 1

	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > maxCursor {
		m.cursor = maxCursor
	}

	// Adjust scroll to keep cursor visible
	visibleHeight := m.visibleHeight()
	if m.cursor < m.scroll {
		m.scroll = m.cursor
	}
	if m.cursor >= m.scroll+visibleHeight {
		m.scroll = m.cursor - visibleHeight + 1
	}
}

// goToEnd moves cursor to the last item
func (m *Model) goToEnd() {
	if m.branchDiff == nil || len(m.branchDiff.Files) == 0 {
		return
	}
	m.cursor = len(m.branchDiff.Files) - 1
	visibleHeight := m.visibleHeight()
	if m.cursor >= visibleHeight {
		m.scroll = m.cursor - visibleHeight + 1
	}
}

// visibleHeight returns the number of visible file rows
func (m *Model) visibleHeight() int {
	h := m.height - 5 // Reserve for header, stats, and footer
	if h < 1 {
		h = 1
	}
	return h
}

// rowIndexAt returns the file index at the given screen Y position
func (m *Model) rowIndexAt(screenY int) (int, bool) {
	if m.branchDiff == nil || len(m.branchDiff.Files) == 0 {
		return -1, false
	}

	// Header takes 3 lines (branch info + stats + blank)
	headerLines := 3
	if screenY < headerLines {
		return -1, false
	}

	rowY := screenY - headerLines
	idx := m.scroll + rowY

	if idx < 0 || idx >= len(m.branchDiff.Files) {
		return -1, false
	}

	return idx, true
}

// openSelectedFile opens the diff for the selected file
func (m *Model) openSelectedFile() tea.Cmd {
	if m.branchDiff == nil || m.cursor < 0 || m.cursor >= len(m.branchDiff.Files) {
		return nil
	}

	file := m.branchDiff.Files[m.cursor]
	wt := m.worktree

	// Create a Change from the BranchFile for the diff viewer
	change := &git.Change{
		Path:    file.Path,
		OldPath: file.OldPath,
		Kind:    file.Kind,
		Staged:  false,
	}

	return func() tea.Msg {
		return messages.OpenDiff{
			Change:   change,
			Mode:     git.DiffModeBranch,
			Worktree: wt,
		}
	}
}

// SetFocused sets the focused state
func (m *Model) SetFocused(focused bool) {
	m.focused = focused
}

// Focus sets the component as focused
func (m *Model) Focus() {
	m.focused = true
}

// Blur removes focus
func (m *Model) Blur() {
	m.focused = false
}

// Focused returns whether the component is focused
func (m *Model) Focused() bool {
	return m.focused
}

// SetSize sets the component dimensions
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SetStyles updates the component's styles
func (m *Model) SetStyles(styles common.Styles) {
	m.styles = styles
}

// View is defined in view.go
