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
		logging.Info("Creating vim tab: file=%s workspace=%s", filePath, ws.Name)

		escapedFile := "'" + strings.ReplaceAll(filePath, "'", "'\\''") + "'"
		cmd := "vim -- " + escapedFile

		tags := tmux.SessionTags{
			WorkspaceID: string(ws.ID()),
			TabID:       string(tabID),
			Type:        "viewer",
			Assistant:   "viewer",
			CreatedAt:   time.Now().Unix(),
			InstanceID:  m.instanceID,
		}
		agent, err := m.agentProvider.CreateViewerWithTags(ws, cmd, sessionName, uint16(termHeight), uint16(termWidth), tags)
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
			Assistant:   "vim",
			DisplayName: displayName,
			Agent:       agent,
			TabID:       tabID,
			Activate:    true,
			Rows:        termHeight,
			Cols:        termWidth,
		}
	}
}

// createDiffTab creates a new native diff viewer tab (no PTY)
func (m *Model) createDiffTab(change *git.Change, mode git.DiffMode, ws *data.Workspace) tea.Cmd {
	if ws == nil {
		return func() tea.Msg {
			return messages.Error{Err: errors.New("no workspace selected"), Context: "creating diff viewer"}
		}
	}

	logging.Info("Creating diff tab: path=%s mode=%d workspace=%s", change.Path, mode, ws.Name)

	tm := m.terminalMetrics()
	viewerWidth := tm.Width
	viewerHeight := tm.Height

	dv := diff.New(ws, change, mode, viewerWidth, viewerHeight)
	dv.SetFocused(true)

	wsID := string(ws.ID())
	displayName := "Diff: " + change.Path
	if len(displayName) > 20 {
		displayName = "..." + displayName[len(displayName)-17:]
	}

	tab := &Tab{
		ID:            generateTabID(),
		Name:          displayName,
		Assistant:     "diff",
		Workspace:     ws,
		DiffViewer:    dv,
		lastFocusedAt: time.Now(),
	}

	m.tabsByWorkspace[wsID] = append(m.tabsByWorkspace[wsID], tab)
	m.setActiveTabIdxForWorkspace(wsID, len(m.tabsByWorkspace[wsID])-1)
	m.noteTabsChanged()

	return common.SafeBatch(
		dv.Init(),
		func() tea.Msg { return messages.TabCreated{Index: m.activeTabByWorkspace[wsID], Name: displayName} },
	)
}
