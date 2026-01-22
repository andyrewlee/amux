package diff

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// Model is the Bubble Tea model for the native diff viewer
type Model struct {
	// Data
	worktree *data.Worktree
	change   *git.Change
	diff     *git.DiffResult
	mode     git.DiffMode

	// State
	loading bool
	err     error
	scroll  int  // Scroll offset in lines
	hunkIdx int  // Current hunk index for n/p navigation
	wrap    bool // Whether to wrap long lines
	focused bool

	// Layout
	width  int
	height int

	// Styles
	styles common.Styles
}

// diffLoaded is sent when the diff has been loaded
type diffLoaded struct {
	diff *git.DiffResult
	err  error
}

// New creates a new diff viewer model
func New(wt *data.Worktree, change *git.Change, mode git.DiffMode, width, height int) *Model {
	return &Model{
		worktree: wt,
		change:   change,
		mode:     mode,
		loading:  true,
		width:    width,
		height:   height,
		styles:   common.DefaultStyles(),
	}
}

// Init initializes the diff viewer and starts loading the diff
func (m *Model) Init() tea.Cmd {
	return m.loadDiff()
}

// loadDiff returns a command that loads the diff asynchronously
func (m *Model) loadDiff() tea.Cmd {
	wt := m.worktree
	change := m.change
	mode := m.mode

	return func() tea.Msg {
		if wt == nil || change == nil {
			return diffLoaded{err: nil, diff: &git.DiffResult{Empty: true}}
		}

		var diff *git.DiffResult
		var err error

		switch {
		case change.Kind == git.ChangeUntracked:
			diff, err = git.GetUntrackedFileContent(wt.Root, change.Path)
		case mode == git.DiffModeBranch:
			diff, err = git.GetBranchFileDiff(wt.Root, change.Path)
		default:
			diff, err = git.GetFileDiff(wt.Root, change.Path, mode)
		}

		return diffLoaded{diff: diff, err: err}
	}
}

// Update handles messages
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	switch msg := msg.(type) {
	case diffLoaded:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.diff = msg.diff
		return m, nil

	case tea.MouseWheelMsg:
		if !m.focused {
			return m, nil
		}
		if msg.Button == tea.MouseWheelUp {
			m.scrollUp(3)
			return m, nil
		}
		if msg.Button == tea.MouseWheelDown {
			m.scrollDown(3)
			return m, nil
		}

	case tea.KeyPressMsg:
		if !m.focused {
			return m, nil
		}

		switch {
		// Scroll controls
		case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
			m.scrollDown(1)
		case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
			m.scrollUp(1)
		case key.Matches(msg, key.NewBinding(key.WithKeys("pgdown", "ctrl+d"))):
			m.scrollDown(m.visibleHeight() / 2)
		case key.Matches(msg, key.NewBinding(key.WithKeys("pgup", "ctrl+u"))):
			m.scrollUp(m.visibleHeight() / 2)
		case key.Matches(msg, key.NewBinding(key.WithKeys("g", "home"))):
			m.scroll = 0
		case key.Matches(msg, key.NewBinding(key.WithKeys("G", "end"))):
			m.scrollToBottom()

		// Hunk navigation
		case key.Matches(msg, key.NewBinding(key.WithKeys("n"))):
			m.nextHunk()
		case key.Matches(msg, key.NewBinding(key.WithKeys("p"))):
			m.prevHunk()

		// Toggle wrap
		case key.Matches(msg, key.NewBinding(key.WithKeys("w"))):
			m.wrap = !m.wrap

		// Close
		case key.Matches(msg, key.NewBinding(key.WithKeys("q", "esc"))):
			return m, func() tea.Msg { return messages.CloseTab{} }
		}
	}

	return m, nil
}

// scrollUp scrolls up by n lines
func (m *Model) scrollUp(n int) {
	m.scroll -= n
	if m.scroll < 0 {
		m.scroll = 0
	}
}

// scrollDown scrolls down by n lines
func (m *Model) scrollDown(n int) {
	m.scroll += n
	maxScroll := m.maxScroll()
	if m.scroll > maxScroll {
		m.scroll = maxScroll
	}
}

// scrollToBottom scrolls to the bottom of the diff
func (m *Model) scrollToBottom() {
	m.scroll = m.maxScroll()
}

// maxScroll returns the maximum scroll offset
func (m *Model) maxScroll() int {
	if m.diff == nil {
		return 0
	}
	total := len(m.diff.Lines)
	visible := m.visibleHeight()
	if total <= visible {
		return 0
	}
	return total - visible
}

// visibleHeight returns the number of visible lines
func (m *Model) visibleHeight() int {
	h := m.height - 3 // Reserve space for header/stats/footer
	if h < 1 {
		h = 1
	}
	return h
}

// nextHunk moves to the next hunk
func (m *Model) nextHunk() {
	if m.diff == nil || len(m.diff.Hunks) == 0 {
		return
	}

	// Find next hunk after current scroll position
	for i, hunk := range m.diff.Hunks {
		if hunk.StartLine > m.scroll {
			m.hunkIdx = i
			m.scroll = hunk.StartLine
			return
		}
	}

	// Wrap to first hunk
	m.hunkIdx = 0
	if len(m.diff.Hunks) > 0 {
		m.scroll = m.diff.Hunks[0].StartLine
	}
}

// prevHunk moves to the previous hunk
func (m *Model) prevHunk() {
	if m.diff == nil || len(m.diff.Hunks) == 0 {
		return
	}

	// Find previous hunk before current scroll position
	for i := len(m.diff.Hunks) - 1; i >= 0; i-- {
		hunk := m.diff.Hunks[i]
		if hunk.StartLine < m.scroll {
			m.hunkIdx = i
			m.scroll = hunk.StartLine
			return
		}
	}

	// Wrap to last hunk
	m.hunkIdx = len(m.diff.Hunks) - 1
	if m.hunkIdx >= 0 {
		m.scroll = m.diff.Hunks[m.hunkIdx].StartLine
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

// GetPath returns the file path being viewed
func (m *Model) GetPath() string {
	if m.change != nil {
		return m.change.Path
	}
	return ""
}

// View is defined in view.go
