package center

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	appPty "github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

// These package-level indirections are test seams for reattach/bootstrap
// paths. Tests that override them must not use t.Parallel within this package.
var (
	sessionStateForFn         = tmux.SessionStateFor
	sessionHasClientsFn       = tmux.SessionHasClients
	sessionClientCountFn      = tmux.SessionClientCount
	sessionActiveWithinFn     = tmux.SessionActiveWithin
	sessionLatestActivityFn   = tmux.SessionLatestActivity
	sessionCreatedAtFn        = tmux.SessionCreatedAt
	sessionPaneIDFn           = tmux.SessionPaneID
	sessionPaneSnapshotInfoFn = tmux.SessionPaneSnapshotInfo
	sessionPaneSizeFn         = tmux.SessionPaneSize
	killSessionFn             = tmux.KillSession
	resizePaneToSizeFn        = tmux.ResizePaneToSize
	capturePaneSnapshotFn     = tmux.CapturePaneSnapshot
	capturePaneFn             = tmux.CapturePane
	createAgentWithTagsFn     = func(
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

type sessionBootstrapCapture = ptyio.SessionBootstrapCapture

// sessionBootstrap builds a ptyio.SessionBootstrap from this package's seam
// vars. It is rebuilt per call (reading the seam vars each time) so a test that
// overrides a seam var still flows through the next bootstrap operation.
func sessionBootstrap() ptyio.SessionBootstrap {
	return ptyio.SessionBootstrap{
		Fns: ptyio.SessionBootstrapFns{
			SessionHasClients:       sessionHasClientsFn,
			SessionClientCount:      sessionClientCountFn,
			SessionActiveWithin:     sessionActiveWithinFn,
			SessionLatestActivity:   sessionLatestActivityFn,
			SessionCreatedAt:        sessionCreatedAtFn,
			SessionPaneID:           sessionPaneIDFn,
			SessionPaneSnapshotInfo: sessionPaneSnapshotInfoFn,
			SessionPaneSize:         sessionPaneSizeFn,
			ResizePaneToSize:        resizePaneToSizeFn,
			CapturePaneSnapshot:     capturePaneSnapshotFn,
		},
		CapturePane: capturePaneFn,
	}
}

func captureExistingSessionBootstrap(sessionName string, cols, rows int, opts tmux.Options) sessionBootstrapCapture {
	return sessionBootstrap().CaptureExisting(sessionName, cols, rows, opts)
}

func bootstrapSnapshotStillMatchesSession(sessionName string, bootstrap sessionBootstrapCapture, opts tmux.Options) bool {
	return sessionBootstrap().SnapshotStillMatches(sessionName, bootstrap, opts)
}

func rollbackExistingSessionBootstrap(sessionName string, bootstrap sessionBootstrapCapture, opts tmux.Options) {
	sessionBootstrap().Rollback(sessionName, bootstrap, opts)
}

func sessionHistoryCaptureSize(sessionName string, fallbackCols, fallbackRows int, opts tmux.Options) (int, int) {
	return sessionBootstrap().HistoryCaptureSize(sessionName, fallbackCols, fallbackRows, opts)
}

func captureSessionHistory(sessionName string, fallbackCols, fallbackRows int, opts tmux.Options) ([]byte, int, int) {
	return sessionBootstrap().CaptureHistory(sessionName, fallbackCols, fallbackRows, opts)
}

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
	reattachInFlight := tab.reattachInFlight
	sessionName := tab.SessionName
	canReattach := detached || !running
	if canReattach && !reattachInFlight {
		_ = tab.beginReattachLocked()
	}
	tab.mu.Unlock()
	if !canReattach {
		return nil
	}
	if reattachInFlight {
		return nil
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
				Err:         err,
				Action:      "reattach",
			}
		}
		if !state.Exists || !state.HasLivePane {
			if state.Exists && !state.HasLivePane {
				_ = killSessionFn(sessionName, opts)
			}
			tags := tmux.SessionTags{
				WorkspaceID:  string(ws.ID()),
				TabID:        string(tabID),
				Type:         "agent",
				Assistant:    assistant,
				CreatedAt:    time.Now().Unix(),
				InstanceID:   m.instanceID,
				SessionOwner: m.instanceID,
				LeaseAtMS:    time.Now().UnixMilli(),
			}
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
				return ptyTabReattachFailed{
					WorkspaceID: string(ws.ID()),
					TabID:       tabID,
					Err:         err,
					Stopped:     true,
					Action:      "reattach",
				}
			}
			captureCols, captureRows := sessionHistoryCaptureSize(sessionName, attachWidth, attachHeight, opts)
			scrollback, _ := capturePaneFn(sessionName, opts)
			return ptyTabReattachResult{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Agent:       agent,
				Rows:        captureRows,
				Cols:        captureCols,
				SessionRestoreCapture: ptyio.SessionRestoreCapture{
					ScrollbackCapture: scrollback,
					CaptureFullPane:   false,
					SnapshotCols:      attachWidth,
					SnapshotRows:      attachHeight,
				},
			}
		}
		tags := tmux.SessionTags{
			WorkspaceID:  string(ws.ID()),
			TabID:        string(tabID),
			Type:         "agent",
			Assistant:    assistant,
			InstanceID:   m.instanceID,
			SessionOwner: m.instanceID,
			LeaseAtMS:    time.Now().UnixMilli(),
		}
		bootstrap := captureExistingSessionBootstrap(sessionName, termWidth, termHeight, opts)
		snapshot := bootstrap.Snapshot
		captureFullPane := bootstrap.CaptureFullPane
		var scrollback []byte
		captureCols := termWidth
		captureRows := termHeight
		var postAttachScrollback []byte
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
			rollbackExistingSessionBootstrap(sessionName, bootstrap, opts)
			return ptyTabReattachFailed{
				WorkspaceID: string(ws.ID()),
				TabID:       tabID,
				Err:         err,
				Action:      "reattach",
			}
		}
		if captureFullPane && bootstrapSnapshotStillMatchesSession(sessionName, bootstrap, opts) {
			scrollback = snapshot.Data
			postAttachScrollback, _ = capturePaneFn(sessionName, opts)
		} else {
			if captureFullPane {
				captureFullPane = false
				snapshot = tmux.PaneSnapshot{}
			}
			scrollback, captureCols, captureRows = captureSessionHistory(sessionName, attachWidth, attachHeight, opts)
		}
		return ptyTabReattachResult{
			WorkspaceID: string(ws.ID()),
			TabID:       tabID,
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
	reattachInFlight := tab.reattachInFlight
	sessionName := tab.SessionName
	if sessionName == "" && tab.Agent != nil {
		sessionName = tab.Agent.Session
	}
	if !running && !reattachInFlight {
		_ = tab.beginReattachLocked()
	}
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

		tags := tmux.SessionTags{
			WorkspaceID:  string(ws.ID()),
			TabID:        string(tabID),
			Type:         "agent",
			Assistant:    assistant,
			CreatedAt:    time.Now().Unix(),
			InstanceID:   m.instanceID,
			SessionOwner: m.instanceID,
			LeaseAtMS:    time.Now().UnixMilli(),
		}
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
				Err:         err,
				Stopped:     true,
				Action:      "restart",
			}
		}
		// Fresh restarts must avoid seeding the visible screen before the PTY
		// reader drains unread startup bytes from the newly attached client.
		captureCols, captureRows := sessionHistoryCaptureSize(sessionName, termWidth, termHeight, tmuxOpts)
		scrollback, _ := capturePaneFn(sessionName, tmuxOpts)
		return ptyTabReattachResult{
			WorkspaceID: string(ws.ID()),
			TabID:       tabID,
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
