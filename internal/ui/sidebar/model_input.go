package sidebar

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// Update handles messages.
//
//nolint:gocyclo,funlen // Key-dispatch switch; each branch is a shallow handler, so the size is breadth rather than nested depth.
func (m *ChangesModel) Update(msg tea.Msg) (*ChangesModel, tea.Cmd) {
	// Handled messages below mutate cursor/scroll/filter/branch state that
	// View renders — marking at the funnel guarantees coverage; a message
	// that early-returns untouched still bumps, which only costs one extra
	// build. (See the contentVersion invariant.)
	defer m.markContentDirty()
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
	case messages.BranchChangesLoaded:
		m.handleBranchChangesLoaded(msg)
		return m, nil

	case messages.AheadBehindLoaded:
		m.handleAheadBehindLoaded(msg)
		return m, nil

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
			// Ignore clicks on section headers: they aren't actionable, and
			// moving the cursor onto one makes the selection visually vanish
			// (headers render no cursor) while opening nothing.
			if idx < 0 || idx >= len(m.displayItems) || m.displayItems[idx].isHeader {
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
			cmds = append(cmds, m.refreshStatus(), m.refreshAheadBehind())
		case key.Matches(msg, key.NewBinding(key.WithKeys("c"))):
			cmds = append(cmds, m.commitWorkspace())
		case key.Matches(msg, key.NewBinding(key.WithKeys("b"))):
			cmds = append(cmds, m.toggleBranchMode())
		case key.Matches(msg, key.NewBinding(key.WithKeys("e"))):
			cmds = append(cmds, m.openEnvDialog())
		case key.Matches(msg, key.NewBinding(key.WithKeys("E"))):
			cmds = append(cmds, m.openProjectEnvDialog())
		case key.Matches(msg, key.NewBinding(key.WithKeys("s"))):
			cmds = append(cmds, m.openScriptsDialog())
		case key.Matches(msg, key.NewBinding(key.WithKeys("r"))):
			cmds = append(cmds, m.toggleRunScript())
		case key.Matches(msg, key.NewBinding(key.WithKeys("R"))):
			cmds = append(cmds, m.openRunOutput())
		case key.Matches(msg, key.NewBinding(key.WithKeys("O"))):
			cmds = append(cmds, m.openScriptOutput())
		case key.Matches(msg, key.NewBinding(key.WithKeys("u"))):
			cmds = append(cmds, m.rerunSetupScript())
		case key.Matches(msg, key.NewBinding(key.WithKeys("i"))):
			cmds = append(cmds, m.openWorkspaceStatus())
		case key.Matches(msg, key.NewBinding(key.WithKeys("/"))):
			// Enter filter mode
			m.filterMode = true
			m.filterInput.Focus()
			return m, m.filterInput.Focus()
		}
	}

	return m, common.SafeBatch(cmds...)
}

// openCurrentItem opens the diff for the currently selected item.
func (m *ChangesModel) openCurrentItem() tea.Cmd {
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

func (m *ChangesModel) rowIndexAt(screenY int) (int, bool) {
	if !m.branchMode && (m.gitStatus == nil || m.gitStatus.Clean) {
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

// moveCursor moves the cursor, skipping section headers.
func (m *ChangesModel) moveCursor(delta int) {
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

// commitWorkspace opens the commit-all dialog for the focused workspace. It
// pre-checks the tree per the write-back design: a nil or clean status has
// nothing to commit (git commit on an empty index errors), so it shows a note
// instead of opening the dialog. Otherwise it emits ShowCommitWorkspaceDialog;
// the actual git.CommitAll runs only after the user confirms with a message.
func (m *ChangesModel) commitWorkspace() tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	if m.gitStatus == nil || m.gitStatus.Clean {
		return func() tea.Msg {
			return messages.Toast{Message: "Nothing to commit", Level: messages.ToastInfo}
		}
	}
	ws := m.workspace
	return func() tea.Msg {
		return messages.ShowCommitWorkspaceDialog{Workspace: ws}
	}
}

// openEnvDialog opens the workspace environment-variable editor for the
// focused workspace. Unlike commitWorkspace, it has no git-status
// precondition -- env vars are independent of the working tree, so there is
// nothing to pre-check before showing the dialog.
func (m *ChangesModel) openEnvDialog() tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	ws := m.workspace
	return func() tea.Msg {
		return messages.ShowWorkspaceEnvDialog{Workspace: ws}
	}
}

// openScriptsDialog opens the workspace scripts editor (user-entered
// setup/run/archive commands + run mode) for the focused workspace — the
// sibling of openEnvDialog, with the same no-precondition shape.
func (m *ChangesModel) openScriptsDialog() tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	ws := m.workspace
	return func() tea.Msg {
		return messages.ShowWorkspaceScriptsDialog{Workspace: ws}
	}
}

// openProjectEnvDialog opens the per-project env editor keyed on the
// focused workspace's repo — same no-precondition shape as openEnvDialog;
// the app derives the project identity from ws.Repo.
func (m *ChangesModel) openProjectEnvDialog() tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	ws := m.workspace
	return func() tea.Msg {
		return messages.ShowProjectEnvDialog{Workspace: ws}
	}
}

// toggleRunScript asks the app to start the workspace's `run` script, or stop
// it when it is already running. The sidebar does not decide which: it has no
// ScriptRunner, and the mirrored scriptRunning flag is a display hint that can
// lag a start/stop still in flight, so the app resolves the direction against
// live state and reports back via WorkspaceScriptStateChanged.
func (m *ChangesModel) toggleRunScript() tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	ws := m.workspace
	return func() tea.Msg {
		return messages.ToggleWorkspaceScript{Workspace: ws}
	}
}

// rerunSetupScript asks the app to re-run the workspace's `setup` — the retry
// for a setup that failed transiently or was edited after creation. The app
// owns the ScriptRunner, so the request travels as a message like the other
// script keys; trust re-gating happens inside RunSetup itself.
func (m *ChangesModel) rerunSetupScript() tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	ws := m.workspace
	return func() tea.Msg {
		return messages.RerunWorkspaceScript{Workspace: ws, Script: process.ScriptSetup}
	}
}

// openRunOutput asks the app to show the workspace's captured run-script
// output — the live pane tail while running, the post-exit tail after —
// mirroring the same fire-and-forget shape as the other dialog openers.
func (m *ChangesModel) openRunOutput() tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	ws := m.workspace
	return func() tea.Msg {
		return messages.ShowRunScriptOutput{Workspace: ws}
	}
}

// openScriptOutput asks the app to show the workspace's recorded lifecycle
// script transcripts (setup/archive/on-done) — the sibling surface to run
// output for the scripts that otherwise have no visible output.
func (m *ChangesModel) openScriptOutput() tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	ws := m.workspace
	return func() tea.Msg {
		return messages.ShowScriptOutput{Workspace: ws}
	}
}

// openWorkspaceStatus asks the app to show the workspace's operational
// snapshot — port allocation, run state, script config + trust, env key
// names, lifecycle state — in a read-only dialog.
func (m *ChangesModel) openWorkspaceStatus() tea.Cmd {
	if m.workspace == nil {
		return nil
	}
	ws := m.workspace
	return func() tea.Msg {
		return messages.ShowWorkspaceStatus{Workspace: ws}
	}
}

// refreshStatus asks the app for a git status refresh. Status is shared
// data — the app owns the cache and dedups concurrent refreshes — so the
// request travels as a message and the result returns as GitStatusResult.
func (m *ChangesModel) refreshStatus() tea.Cmd {
	if m.workspace == nil {
		return nil
	}

	root := m.workspace.Root
	return func() tea.Msg {
		return messages.GitStatusRequest{Root: root}
	}
}
