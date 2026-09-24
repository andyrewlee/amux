package sidebar

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
)

// loadBranchChanges returns a command that fetches BranchChangesVsBase for
// the current workspace. Bumps branchLoadID so a stale result (e.g. from a
// rapid toggle-off/toggle-on) is dropped on arrival.
func (m *Model) loadBranchChanges() tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	root := m.workspace.Root
	m.branchLoadID++
	loadID := m.branchLoadID
	return func() tea.Msg {
		changes, err := git.BranchChangesVsBase(root)
		return messages.BranchChangesLoaded{Root: root, LoadID: loadID, Changes: changes, Err: err}
	}
}

// refreshAheadBehind returns a command that fetches AheadBehind for the
// current workspace. Bumps aheadBehindLoadID so a stale result is dropped.
func (m *Model) refreshAheadBehind() tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	root := m.workspace.Root
	m.aheadBehindLoadID++
	loadID := m.aheadBehindLoadID
	return func() tea.Msg {
		ahead, behind, err := git.AheadBehind(root)
		return messages.AheadBehindLoaded{Root: root, LoadID: loadID, Ahead: ahead, Behind: behind, Err: err}
	}
}

// handleBranchChangesLoaded applies a BranchChangesLoaded result, dropping it
// if it's stale (superseded by a newer toggle or a workspace switch).
func (m *Model) handleBranchChangesLoaded(msg messages.BranchChangesLoaded) {
	if msg.LoadID != m.branchLoadID {
		return
	}
	if m.workspace == nil || msg.Root != m.workspace.Root {
		return
	}
	m.branchLoading = false
	m.branchErr = msg.Err
	m.branchChanges = msg.Changes
	if m.branchMode {
		m.rebuildDisplayList()
	}
}

// handleAheadBehindLoaded applies an AheadBehindLoaded result, dropping it if
// it's stale.
func (m *Model) handleAheadBehindLoaded(msg messages.AheadBehindLoaded) {
	if msg.LoadID != m.aheadBehindLoadID {
		return
	}
	if m.workspace == nil || msg.Root != m.workspace.Root {
		return
	}
	m.aheadBehindErr = msg.Err
	if msg.Err == nil {
		m.ahead = msg.Ahead
		m.behind = msg.Behind
	}
}

// toggleBranchMode flips branch mode. Turning it on (re-)triggers a fetch;
// turning it off just falls back to the staged/unstaged/untracked list built
// from the already-loaded gitStatus — no fetch needed.
func (m *Model) toggleBranchMode() tea.Cmd {
	m.branchMode = !m.branchMode
	var cmd tea.Cmd
	if m.branchMode {
		m.branchLoading = true
		m.branchErr = nil
		cmd = m.loadBranchChanges()
	}
	m.rebuildDisplayList()
	return cmd
}

// rebuildBranchDisplayList populates displayItems from branchChanges,
// honoring the same filter query as the staged/unstaged/untracked lists.
func (m *Model) rebuildBranchDisplayList() {
	if len(m.branchChanges) == 0 {
		return
	}

	matchesFilter := func(c *git.Change) bool {
		if m.filterQuery == "" {
			return true
		}
		return strings.Contains(strings.ToLower(c.Path), strings.ToLower(m.filterQuery))
	}

	count := 0
	for i := range m.branchChanges {
		if matchesFilter(&m.branchChanges[i]) {
			count++
		}
	}
	if count == 0 {
		return
	}

	m.displayItems = append(m.displayItems, displayItem{
		isHeader: true,
		header:   "Vs base (" + strconv.Itoa(count) + ")",
	})
	for i := range m.branchChanges {
		if matchesFilter(&m.branchChanges[i]) {
			m.displayItems = append(m.displayItems, displayItem{
				change: &m.branchChanges[i],
				mode:   git.DiffModeBranch,
			})
		}
	}
}
