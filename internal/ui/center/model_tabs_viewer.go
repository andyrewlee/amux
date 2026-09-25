package center

import (
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/diff"
)

// createVimTab creates a new tab that opens a file in vim
func (m *Model) createVimTab(filePath string, ws *data.Workspace) tea.Cmd {
	if ws == nil {
		return func() tea.Msg {
			return messages.Error{Err: errors.New("no workspace selected"), Context: "creating vim viewer"}
		}
	}

	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height
	tabID := generateTabID()
	sessionName := tmux.SessionName("amux", string(ws.ID()), string(tabID))

	return func() tea.Msg {
		logging.Info("Creating viewer tab: file=%s workspace=%s", filePath, ws.Name)

		cmd, label := m.viewerLaunch(filePath)

		tags := tmux.SessionTags{
			WorkspaceID:   string(ws.ID()),
			TabID:         string(tabID),
			Type:          "viewer",
			Assistant:     "viewer",
			CreatedAt:     time.Now().Unix(),
			InstanceID:    m.instanceID,
			SessionOwner:  m.instanceID,
			LeaseAtMS:     time.Now().UnixMilli(),
			WorkspaceName: ws.Name,
			ProjectName:   data.ProjectNameForRepo(ws.Repo),
		}
		ptyRows, ptyCols, _ := appPty.WinsizeFromInts(termHeight, termWidth)
		agent, err := m.agentManager.CreateViewerWithTags(ws, cmd, sessionName, ptyRows, ptyCols, tags)
		if err != nil {
			logging.Error("Failed to create vim viewer: %v", err)
			return messages.Error{Err: err, Context: "creating vim viewer"}
		}

		logging.Info("Vim viewer created, Terminal=%v", agent.Terminal != nil)

		fileName := filePath
		if idx := strings.LastIndex(filePath, "/"); idx >= 0 {
			fileName = fileName[idx+1:]
		}
		displayName := truncateDisplayName(fileName)

		return ptyTabCreateResult{
			Workspace:   ws,
			Assistant:   label,
			DisplayName: displayName,
			Agent:       agent,
			TabID:       tabID,
			Activate:    true,
			Rows:        termHeight,
			Cols:        termWidth,
		}
	}
}

func (m *Model) findOpenDiffTab(ws *data.Workspace, changePath string, mode git.DiffMode) (int, *Tab) {
	if ws == nil {
		return -1, nil
	}
	wsID := string(ws.ID())
	for idx, tab := range m.tabs.ByWorkspace[wsID] {
		if tab == nil || tab.isClosed() {
			continue
		}
		tab.mu.Lock()
		dv := tab.DiffViewer
		tab.mu.Unlock()
		if dv != nil && dv.MatchesSource(changePath, mode) {
			return idx, tab
		}
	}
	return -1, nil
}

// diffResultMsg wraps an async diff-viewer result with the identity of the tab
// that issued the load, so the result routes back to it instead of whichever
// tab happens to be active on arrival.
type diffResultMsg struct {
	WorkspaceID string
	TabID       TabID
	Inner       tea.Msg
}

// wrapDiffResults points a diff viewer's async results at a specific tab.
func wrapDiffResults(dv *diff.Model, wsID string, tabID TabID) {
	dv.SetResultWrapper(func(msg tea.Msg) tea.Msg {
		return diffResultMsg{WorkspaceID: wsID, TabID: tabID, Inner: msg}
	})
}

func (m *Model) reuseDiffTab(ws *data.Workspace, idx int, tab *Tab, change *git.Change, mode git.DiffMode) tea.Cmd {
	if ws == nil || tab == nil {
		return nil
	}
	wsID := string(ws.ID())
	activeChanged := m.tabs.ActiveByWorkspace[wsID] != idx
	m.setActiveTabIdxForWorkspace(wsID, idx)

	var cmds []tea.Cmd
	tab.mu.Lock()
	dv := tab.DiffViewer
	tab.mu.Unlock()
	if dv != nil {
		wrapDiffResults(dv, wsID, tab.ID)
		dv.ResetSource(ws, change, mode)
		cmds = append(cmds, dv.Init())
	}
	if m.workspaceID() == wsID {
		cmds = append(cmds, m.tabSelectionChangedCmd(activeChanged))
	}
	return common.SafeBatch(cmds...)
}

// createDiffTab creates a new native diff viewer tab (no PTY)
func (m *Model) createDiffTab(change *git.Change, mode git.DiffMode, ws *data.Workspace) tea.Cmd {
	if ws == nil {
		return func() tea.Msg {
			return messages.Error{Err: errors.New("no workspace selected"), Context: "creating diff viewer"}
		}
	}

	if idx, tab := m.findOpenDiffTab(ws, change.Path, mode); tab != nil {
		logging.Info("Reusing diff tab: path=%s mode=%d workspace=%s", change.Path, mode, ws.Name)
		return m.reuseDiffTab(ws, idx, tab, change, mode)
	}

	logging.Info("Creating diff tab: path=%s mode=%d workspace=%s", change.Path, mode, ws.Name)

	tm := m.terminalMetrics()
	viewerWidth := tm.Width
	viewerHeight := tm.Height

	dv := diff.New(ws, change, mode, viewerWidth, viewerHeight)
	dv.SetFocused(true)

	wsID := string(ws.ID())
	displayName := truncateDisplayName("Diff: " + common.SanitizeDisplayText(change.Path, 256))

	tab := &Tab{
		ID:            generateTabID(),
		Name:          displayName,
		Assistant:     "diff",
		Workspace:     ws,
		DiffViewer:    dv,
		lastFocusedAt: time.Now(),
	}
	wrapDiffResults(dv, wsID, tab.ID)

	m.tabs.ByWorkspace[wsID] = append(m.tabs.ByWorkspace[wsID], tab)
	m.setActiveTabIdxForWorkspace(wsID, len(m.tabs.ByWorkspace[wsID])-1)
	m.noteTabsChanged()

	return common.SafeBatch(
		dv.Init(),
		func() tea.Msg { return messages.TabCreated{Index: m.tabs.ActiveByWorkspace[wsID], Name: displayName} },
	)
}

// viewerLaunch builds the viewer shell line `<command> -- '<file>'` and the
// tab label. The configured command is a user-provided shell fragment (e.g.
// "vim", "nvim", "less -R") and is deliberately NOT quoted — only the file
// path is. The label is the fragment's first token so "less -R" labels "less".
func (m *Model) viewerLaunch(filePath string) (cmd, label string) {
	viewer := strings.TrimSpace(m.viewerCommand)
	if viewer == "" {
		viewer = "vim"
	}
	escaped := "'" + strings.ReplaceAll(filePath, "'", "'\\''") + "'"
	return viewer + " -- " + escaped, strings.Fields(viewer)[0]
}
