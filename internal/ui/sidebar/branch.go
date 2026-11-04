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
func (m *ChangesModel) loadBranchChanges() tea.Cmd {
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
func (m *ChangesModel) refreshAheadBehind() tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	root := m.workspace.Root
	m.aheadBehindLoadID++
	loadID := m.aheadBehindLoadID
	// From loaded or error this is a plain re-entry; from loading the new
	// fetch supersedes the old one, whose late result drops via load ID.
	// Starting from error, the stale counts are dropped too: the pre-machine
	// code kept the badge hidden from the first failure until a fresh
	// success, and re-showing pre-error numbers for the whole retry window
	// would be new behavior.
	if m.aheadBehind == fetchError {
		m.ahead, m.behind = 0, 0
	}
	m.aheadBehind.step(evFetchStart)
	return func() tea.Msg {
		ahead, behind, err := git.AheadBehind(root)
		return messages.AheadBehindLoaded{Root: root, LoadID: loadID, Ahead: ahead, Behind: behind, Err: err}
	}
}

// handleBranchChangesLoaded applies a BranchChangesLoaded result, dropping it
// if it's stale (superseded by a newer toggle or a workspace switch).
func (m *ChangesModel) handleBranchChangesLoaded(msg messages.BranchChangesLoaded) {
	if msg.LoadID != m.branchLoadID {
		return
	}
	if m.workspace == nil || msg.Root != m.workspace.Root {
		return
	}
	// The load guards above proved this result belongs to the in-flight
	// fetch; the phase step records its outcome.
	if msg.Err != nil {
		m.branch.step(evFetchFail)
	} else {
		m.branch.step(evFetchSucceed)
	}
	m.branchErr = msg.Err
	m.branchChanges = msg.Changes
	if m.branchModeOn() {
		m.rebuildDisplayList()
	}
}

// handleAheadBehindLoaded applies an AheadBehindLoaded result, dropping it if
// it's stale.
func (m *ChangesModel) handleAheadBehindLoaded(msg messages.AheadBehindLoaded) {
	if msg.LoadID != m.aheadBehindLoadID {
		return
	}
	if m.workspace == nil || msg.Root != m.workspace.Root {
		return
	}
	if msg.Err != nil {
		m.aheadBehind.step(evFetchFail)
		return
	}
	m.aheadBehind.step(evFetchSucceed)
	m.ahead = msg.Ahead
	m.behind = msg.Behind
}

// toggleBranchMode flips branch mode. Turning it on (re-)triggers a fetch;
// turning it off just falls back to the staged/unstaged/untracked list built
// from the already-loaded gitStatus — no fetch needed. A fetch still in
// flight when the mode turns off is reset: its late result is dropped by the
// phase machine rather than applied invisibly.
func (m *ChangesModel) toggleBranchMode() tea.Cmd {
	var cmd tea.Cmd
	if m.branch == fetchIdle {
		m.branch.step(evFetchStart)
		m.branchErr = nil
		cmd = m.loadBranchChanges()
	} else {
		m.branch.step(evFetchReset)
	}
	m.rebuildDisplayList()
	return cmd
}

// rebuildBranchDisplayList populates displayItems from branchChanges,
// honoring the same filter query as the staged/unstaged/untracked lists.
func (m *ChangesModel) rebuildBranchDisplayList() {
	if len(m.branchChanges) == 0 {
		return
	}

	matchesFilter := func(c *git.Change) bool {
		query := m.filter.Query()
		if query == "" {
			return true
		}
		return strings.Contains(strings.ToLower(c.Path), strings.ToLower(query))
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
