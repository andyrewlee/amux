package sidebar

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
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
// (rendering lives in model_view.go, input in model_input.go).
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

// New creates a new sidebar model.
func New() *Model {
	ti := textinput.New()
	ti.Placeholder = "filter..."
	ti.CharLimit = 100

	return &Model{
		styles:      common.DefaultStyles(),
		filterInput: ti,
	}
}

// rebuildDisplayList rebuilds the flat display list from grouped status.
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
