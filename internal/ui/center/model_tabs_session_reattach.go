package center

import (
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

// These package-level indirections are test seams for reattach/bootstrap
// paths. The bootstrap seams themselves live in ptyio (ptyio.ProbeSessionFn
// et al.); tests that override any of them must not use t.Parallel within
// this package.
var (
	sessionStateForFn     = tmux.SessionStateFor
	sessionOwnedFn        = ptyio.SessionOwned
	killSessionFn         = tmux.KillSession
	capturePaneFn         = tmux.CapturePane
	createAgentWithTagsFn = func(
		manager *appPty.AgentManager,
		ws *data.Workspace,
		agentType appPty.AgentType,
		sessionName string,
		rows, cols uint16,
		tags tmux.SessionTags,
	) (*appPty.Agent, error) {
		return manager.CreateAgentWithTags(ws, agentType, sessionName, rows, cols, tags)
	}
)

func (m *Model) sessionBootstrapViewportSize() (int, int) {
	if m.width <= 0 || m.height <= 0 {
		return 0, 0
	}
	tm := m.terminalMetrics()
	return tm.Width, tm.Height
}

// ReattachActiveTab reattaches to a detached/stopped tmux session.
func (m *Model) ReattachActiveTab() tea.Cmd {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return nil
	}
	tab := tabs[activeIdx]
	if tab == nil || tab.Workspace == nil {
		return nil
	}
	tab.mu.Lock()
	running := tab.Running
	detached := tab.Detached
	reattachInFlight := tab.Reattach.InFlight
	sessionName := tab.SessionName
	canReattach := detached || !running
	if canReattach && !reattachInFlight {
		_ = tab.beginReattachLocked()
	}
	epoch := tab.reattachEpochLocked()
	tab.mu.Unlock()
	if !canReattach {
		return nil
	}
	if reattachInFlight {
		// Say so rather than no-op. A reattach that is genuinely in flight
		// resolves on its own or is released by SweepStalledReattaches, but a
		// silent keybinding makes a stuck tab look like a broken one.
		return func() tea.Msg {
			return messages.Toast{
				Message: "Reattach already in progress",
				Level:   messages.ToastInfo,
			}
		}
	}
	if m.config == nil || m.config.Assistants == nil {
		tab.mu.Lock()
		tab.endReattachLocked()
		tab.mu.Unlock()
		return func() tea.Msg {
			return messages.Toast{
				Message: "Tab cannot be reattached",
				Level:   messages.ToastInfo,
			}
		}
	}
	if _, ok := m.config.Assistants[tab.Assistant]; !ok {
		tab.mu.Lock()
		tab.endReattachLocked()
		tab.mu.Unlock()
		return func() tea.Msg {
			return messages.Toast{
				Message: "Only assistant tabs can be reattached",
				Level:   messages.ToastInfo,
			}
		}
	}
	termWidth, termHeight := m.sessionBootstrapViewportSize()
	tm := m.terminalMetrics()
	attachWidth := tm.Width
	attachHeight := tm.Height
	if sessionName == "" {
		sessionName = tmux.SessionName("amux", string(tab.Workspace.ID()), string(tab.ID))
	}
	assistant := tab.Assistant
	ws := tab.Workspace
	tabID := tab.ID
	opts := m.tmuxOpts
	return func() tea.Msg {
		state, err := sessionStateForFn(sessionName, opts)
		if err != nil {
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Epoch:       epoch,
				Err:         err,
				Action:      "reattach",
			}
		}
		// Dead-session policy is ptyio.SessionAttachable, shared with sidebar:
		// reattach only attaches to a live session — a missing or dead session
		// reports Stopped so the user chooses restart explicitly. (This used to
		// kill the dead session and silently recreate, losing the distinction.)
		if !ptyio.SessionAttachable(state) {
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Epoch:       epoch,
				Err:         errors.New("tmux session ended"),
				Stopped:     true,
				Action:      "reattach",
			}
		}
		// Ownership policy is ptyio.SessionOwned, shared with sidebar: the
		// live session must carry our tags. A foreign session squatting the
		// name reports Stopped so the user restarts explicitly — restart
		// kills the squatter by name before recreating.
		owned, ownErr := sessionOwnedFn(sessionName, data.WorkspaceIdentityStrings(ws), opts)
		if ownErr != nil {
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Epoch:       epoch,
				Err:         ownErr,
				Action:      "reattach",
			}
		}
		if !owned {
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Epoch:       epoch,
				Err:         errors.New("tmux session is not owned by this workspace"),
				Stopped:     true,
				Action:      "reattach",
			}
		}
		tags := ptyio.AttachSessionTags(ws, string(tabID), "agent", assistant, m.instanceID, false)
		bootstrap := ptyio.DefaultBootstrap().CaptureExisting(sessionName, termWidth, termHeight, opts)
		ptyRows, ptyCols, _ := appPty.WinsizeFromInts(attachHeight, attachWidth)
		agent, err := createAgentWithTagsFn(
			m.agentManager,
			ws,
			appPty.AgentType(assistant),
			sessionName,
			ptyRows,
			ptyCols,
			tags,
		)
		if err != nil {
			ptyio.DefaultBootstrap().Rollback(sessionName, bootstrap, opts)
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Epoch:       epoch,
				Err:         err,
				Action:      "reattach",
			}
		}
		scrollback, postAttachScrollback, captureFullPane, snapshot, captureCols, captureRows := ptyio.FinalizeAttachScrollback(sessionName, bootstrap, attachWidth, attachHeight, opts, capturePaneFn)
		return ptyTabReattachResult{
			WorkspaceID: string(ws.ID()),
			TabID:       tabID,
			Epoch:       epoch,
			Agent:       agent,
			Rows:        captureRows,
			Cols:        captureCols,
			SessionRestoreCapture: ptyio.SessionRestoreCapture{
				ScrollbackCapture:           scrollback,
				PostAttachScrollbackCapture: postAttachScrollback,
				CaptureFullPane:             captureFullPane,
				SnapshotCols:                snapshot.Cols,
				SnapshotRows:                snapshot.Rows,
				SnapshotCursorX:             snapshot.CursorX,
				SnapshotCursorY:             snapshot.CursorY,
				SnapshotHasCursor:           snapshot.HasCursor,
				SnapshotModeState:           snapshot.ModeState,
			},
		}
	}
}

// RestartActiveTab restarts a stopped or detached agent tab by creating a fresh tmux client.
func (m *Model) RestartActiveTab() tea.Cmd {
	tabs := m.getTabs()
	activeIdx := m.getActiveTabIdx()
	if len(tabs) == 0 || activeIdx >= len(tabs) {
		return nil
	}
	tab := tabs[activeIdx]
	if tab == nil || tab.Workspace == nil {
		return nil
	}
	if m.config == nil || m.config.Assistants == nil {
		return nil
	}
	if _, ok := m.config.Assistants[tab.Assistant]; !ok {
		return nil
	}
	tab.mu.Lock()
	running := tab.Running
	reattachInFlight := tab.Reattach.InFlight
	sessionName := tab.SessionName
	if sessionName == "" && tab.Agent != nil {
		sessionName = tab.Agent.Session
	}
	if !running && !reattachInFlight {
		_ = tab.beginReattachLocked()
	}
	epoch := tab.reattachEpochLocked()
	tab.mu.Unlock()
	if running {
		return func() tea.Msg {
			return messages.Toast{
				Message: "Tab is still running",
				Level:   messages.ToastInfo,
			}
		}
	}
	if reattachInFlight {
		return nil
	}
	ws := tab.Workspace
	tabID := tab.ID
	if sessionName == "" {
		sessionName = tmux.SessionName("amux", string(ws.ID()), string(tabID))
	}
	m.stopPTYReader(tab)
	var existingAgent *appPty.Agent
	tab.mu.Lock()
	existingAgent = tab.Agent
	tab.Agent = nil
	tab.mu.Unlock()
	if existingAgent != nil {
		_ = m.agentManager.CloseAgent(existingAgent)
	}
	tmuxOpts := m.tmuxOpts

	tm := m.terminalMetrics()
	termWidth := tm.Width
	termHeight := tm.Height
	assistant := tab.Assistant

	return func() tea.Msg {
		// KillSession is synchronous: it calls cmd.Run() which blocks until the
		// tmux server processes the kill and returns. By the time it completes,
		// the session is fully removed from tmux's perspective.
		// The subsequent CreateAgentWithTags uses `new-session -Ads` which is
		// atomic (attach-if-exists, create-if-not), providing an additional
		// safety net in the unlikely event of cleanup lag.
		_ = killSessionFn(sessionName, tmuxOpts)

		tags := ptyio.AttachSessionTags(ws, string(tabID), "agent", assistant, m.instanceID, true)
		ptyRows, ptyCols, _ := appPty.WinsizeFromInts(termHeight, termWidth)
		agent, err := createAgentWithTagsFn(
			m.agentManager,
			ws,
			appPty.AgentType(assistant),
			sessionName,
			ptyRows,
			ptyCols,
			tags,
		)
		if err != nil {
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Epoch:       epoch,
				Err:         err,
				Stopped:     true,
				Action:      "restart",
			}
		}
		// Fresh restarts must avoid seeding the visible screen before the PTY
		// reader drains unread startup bytes from the newly attached client.
		captureCols, captureRows := ptyio.DefaultBootstrap().HistoryCaptureSize(sessionName, termWidth, termHeight, tmuxOpts)
		scrollback, _ := capturePaneFn(sessionName, tmuxOpts)
		return ptyTabReattachResult{
			WorkspaceID: string(ws.ID()),
			TabID:       tabID,
			Epoch:       epoch,
			Agent:       agent,
			Rows:        captureRows,
			Cols:        captureCols,
			SessionRestoreCapture: ptyio.SessionRestoreCapture{
				ScrollbackCapture: scrollback,
				CaptureFullPane:   false,
				SnapshotCols:      termWidth,
				SnapshotRows:      termHeight,
			},
		}
	}
}
