package commits

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

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

		// Wheel scroll: use viewport-proportional delta
		delta := common.ScrollDeltaForHeight(m.visibleHeight(), 10)
		if msg.Button == tea.MouseWheelUp {
			m.moveCursor(-delta)
			return m, nil
		}
		if msg.Button == tea.MouseWheelDown {
			m.moveCursor(delta)
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
