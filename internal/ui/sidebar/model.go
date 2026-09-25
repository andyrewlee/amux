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

// ChangesModel is the Bubbletea model for the sidebar's Changes tab content —
// the changed-files list, not the sidebar itself (that is TabbedSidebar).
// (rendering lives in model_view.go, input in model_input.go; branch-mode
// fetch/render helpers live in branch.go).
type ChangesModel struct {
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

	// Branch mode: when true, displayItems lists files changed vs base
	// (BranchChangesVsBase) instead of working-tree status, feeding the
	// existing DiffModeBranch diff viewer. Additive to staged/unstaged/
	// untracked — toggled independently, off by default.
	branchMode    bool
	branchChanges []git.Change
	branchLoading bool
	branchErr     error
	branchLoadID  int // guards a stale toggle-triggered fetch from clobbering a newer one

	// scriptRunning mirrors whether the workspace's `run` script is live, as
	// last reported by the app (which owns the ScriptRunner). It is keyed by
	// workspace root rather than held as a bare bool so a state change that
	// lands after the user has switched workspaces cannot mislabel the new one.
	scriptRunningRoot string
	scriptRunning     bool

	// Ahead/behind vs base (git.AheadBehind), refreshed on workspace switch
	// and manual refresh; rendered as a badge regardless of branchMode.
	ahead             int
	behind            int
	aheadBehindErr    error
	aheadBehindLoadID int // guards a stale refresh from clobbering a newer one

	// Display list (flattened from grouped status, or from branchChanges when
	// branchMode is active)
	displayItems []displayItem

	// Layout
	width           int
	height          int
	showKeymapHints bool

	// Styles
	styles common.Styles

	// contentVersion is a monotonic version of every input that shapes View
	// output. INVARIANT: every update path that changes what View renders
	// MUST call markContentDirty (Update marks at the funnel), or the
	// compose-time gate in internal/app will keep reusing a stale drawable.
	contentVersion uint64
	// contentBuilds counts View invocations; test instrumentation for the
	// compose-time skip gate in internal/app.
	contentBuilds uint64
}

// markContentDirty bumps contentVersion; see the field's invariant.
func (m *ChangesModel) markContentDirty() { m.contentVersion++ }

// ContentVersion returns the monotonic version of the inputs to View. The
// compose layer in internal/app skips rebuilding the content string while
// this version and the compose geometry are unchanged.
func (m *ChangesModel) ContentVersion() uint64 { return m.contentVersion }

// ContentBuildCount reports how many times View has been invoked. Test
// instrumentation for the compose-time skip gate; not for production use.
func (m *ChangesModel) ContentBuildCount() uint64 { return m.contentBuilds }

// NewChangesModel creates a new Changes-tab content model.
func NewChangesModel() *ChangesModel {
	ti := textinput.New()
	ti.Placeholder = "filter..."
	ti.CharLimit = 100

	return &ChangesModel{
		styles:      common.DefaultStyles(),
		filterInput: ti,
	}
}

// rebuildDisplayList rebuilds the flat display list from grouped status, or
// from branchChanges when branch mode is active (see branch.go).
func (m *ChangesModel) rebuildDisplayList() {
	m.displayItems = nil

	if m.branchMode {
		m.rebuildBranchDisplayList()
		m.clampCursorToDisplayItems()
		return
	}

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

	m.clampCursorToDisplayItems()
}

// clampCursorToDisplayItems keeps the cursor in bounds and off section
// headers after displayItems changes shape (rebuild, filter, mode toggle).
func (m *ChangesModel) clampCursorToDisplayItems() {
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

func (m *ChangesModel) listHeaderLines() int {
	if !m.branchMode && (m.gitStatus == nil || m.gitStatus.Clean) {
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

func (m *ChangesModel) visibleHeight() int {
	header := m.listHeaderLines()
	help := m.helpLineCount()
	visible := m.height - header - help
	if visible < 1 {
		visible = 1
	}
	return visible
}
