package sidebar

import (
	"errors"
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// These package-level indirections are test seams for terminal attach/bootstrap
// paths. Tests that override them must not use t.Parallel within this package.
var (
	ensureTmuxAvailableFn       = tmux.EnsureAvailable
	sessionStateForFn           = tmux.SessionStateFor
	sessionHasClientsFn         = tmux.SessionHasClients
	sessionClientCountFn        = tmux.SessionClientCount
	sessionActiveWithinFn       = tmux.SessionActiveWithin
	sessionCreatedAtFn          = tmux.SessionCreatedAt
	sessionPaneIDFn             = tmux.SessionPaneID
	sessionPaneSnapshotInfoFn   = tmux.SessionPaneSnapshotInfo
	sessionPaneSizeFn           = tmux.SessionPaneSize
	newPTYWithSizeFn            = pty.NewWithSize
	resizePaneToSizeFn          = tmux.ResizePaneToSize
	capturePaneSnapshotFn       = tmux.CapturePaneSnapshot
	capturePaneFn               = tmux.CapturePane
	verifyTerminalSessionTagsFn = verifyTerminalSessionTags
)

const fullPaneCaptureQuietWindow = 2 * time.Second

type sessionBootstrapCapture = common.SessionBootstrapCapture

func sessionBootstrapFns() common.SessionBootstrapFns {
	return common.SessionBootstrapFns{
		SessionHasClients:       sessionHasClientsFn,
		SessionClientCount:      sessionClientCountFn,
		SessionActiveWithin:     sessionActiveWithinFn,
		SessionLatestActivity:   tmux.SessionLatestActivity,
		SessionCreatedAt:        sessionCreatedAtFn,
		SessionPaneID:           sessionPaneIDFn,
		SessionPaneSnapshotInfo: sessionPaneSnapshotInfoFn,
		SessionPaneSize:         sessionPaneSizeFn,
		ResizePaneToSize:        resizePaneToSizeFn,
		CapturePaneSnapshot:     capturePaneSnapshotFn,
	}
}

func captureExistingSessionBootstrap(sessionName string, cols, rows int, opts tmux.Options) sessionBootstrapCapture {
	return common.CaptureExistingSessionBootstrap(sessionName, cols, rows, fullPaneCaptureQuietWindow, opts, sessionBootstrapFns())
}

func bootstrapSnapshotStillMatchesSession(sessionName string, bootstrap sessionBootstrapCapture, opts tmux.Options) bool {
	return common.BootstrapSnapshotStillMatchesSession(sessionName, bootstrap, opts, sessionBootstrapFns())
}

func rollbackExistingSessionBootstrap(sessionName string, bootstrap sessionBootstrapCapture, opts tmux.Options) {
	common.RollbackExistingSessionBootstrap(sessionName, bootstrap, opts, sessionBootstrapFns())
}

func sessionHistoryCaptureSize(sessionName string, fallbackCols, fallbackRows int, opts tmux.Options) (int, int) {
	return common.SessionHistoryCaptureSize(sessionName, fallbackCols, fallbackRows, opts, sessionBootstrapFns())
}

func captureSessionHistory(sessionName string, fallbackCols, fallbackRows int, opts tmux.Options) ([]byte, int, int) {
	return common.CaptureSessionHistory(sessionName, fallbackCols, fallbackRows, opts, sessionBootstrapFns(), capturePaneFn)
}

func (m *TerminalModel) sessionBootstrapViewportSize() (int, int) {
	if m.width <= 0 || m.height <= 0 {
		return 0, 0
	}
	return m.terminalContentSize()
}

// createTerminalTab creates a new terminal tab for the workspace
func (m *TerminalModel) createTerminalTab(ws *data.Workspace) tea.Cmd {
	wsID := string(ws.ID())
	tabID := generateTerminalTabID()
	termWidth, termHeight := m.sessionBootstrapViewportSize()
	attachWidth, attachHeight := m.terminalContentSize()
	opts := m.getTmuxOptions()
	instanceID := m.instanceID
	root := ws.Root

	return func() tea.Msg {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/bash"
		}
		if err := ensureTmuxAvailableFn(); err != nil {
			return SidebarTerminalCreateFailed{WorkspaceID: wsID, Err: err}
		}

		var scrollback []byte
		var postAttachScrollback []byte
		var snapshot tmux.PaneSnapshot
		var bootstrap sessionBootstrapCapture
		captureFullPane := false
		captureCols := attachWidth
		captureRows := attachHeight
		reuseExistingSession := false
		env := []string{"COLORTERM=truecolor"}
		sessionName := tmux.SessionName("amux", wsID, string(tabID))
		// Reuse scrollback if a prior tmux session with the same name exists
		// (e.g., app restart with persisted tmux session).
		if state, err := sessionStateForFn(sessionName, opts); err == nil && state.Exists && state.HasLivePane {
			reuseExistingSession = true
		}
		if reuseExistingSession {
			bootstrap = captureExistingSessionBootstrap(sessionName, termWidth, termHeight, opts)
			snapshot = bootstrap.Snapshot
			captureFullPane = bootstrap.CaptureFullPane
		}
		tags := tmux.SessionTags{
			WorkspaceID:  wsID,
			TabID:        string(tabID),
			Type:         "terminal",
			Assistant:    "terminal",
			CreatedAt:    time.Now().Unix(),
			InstanceID:   instanceID,
			SessionOwner: instanceID,
			LeaseAtMS:    time.Now().UnixMilli(),
		}
		command := tmux.NewClientCommand(sessionName, tmux.ClientCommandParams{
			WorkDir:        root,
			Command:        fmt.Sprintf("exec %s -l", shell),
			Options:        opts,
			Tags:           tags,
			DetachExisting: true,
		})
		term, err := newPTYWithSizeFn(command, root, env, uint16(attachHeight), uint16(attachWidth))
		if err != nil {
			if reuseExistingSession {
				rollbackExistingSessionBootstrap(sessionName, bootstrap, opts)
			}
			return SidebarTerminalCreateFailed{WorkspaceID: wsID, Err: err}
		}
		if reuseExistingSession {
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
		}
		if err := verifyTerminalSessionTagsFn(sessionName, tags, opts); err != nil {
			logging.Warn("sidebar terminal create: session tag verification failed for %s: %v", sessionName, err)
		}

		return SidebarTerminalCreated{
			WorkspaceID:          wsID,
			TabID:                tabID,
			Terminal:             term,
			SessionName:          sessionName,
			Scrollback:           scrollback,
			PostAttachScrollback: postAttachScrollback,
			CaptureCols:          captureCols,
			CaptureRows:          captureRows,
			CaptureFullPane:      captureFullPane,
			SnapshotCols:         snapshot.Cols,
			SnapshotRows:         snapshot.Rows,
			SnapshotCursorX:      snapshot.CursorX,
			SnapshotCursorY:      snapshot.CursorY,
			SnapshotHasCursor:    snapshot.HasCursor,
			SnapshotModeState:    snapshot.ModeState,
		}
	}
}

// DetachActiveTab closes the PTY client but keeps the tmux session alive.
func (m *TerminalModel) DetachActiveTab() tea.Cmd {
	tab := m.getActiveTab()
	if tab == nil || tab.State == nil {
		return nil
	}
	m.detachState(tab.State, true)
	return nil
}

// ReattachActiveTab reattaches to a detached tmux session for the active terminal tab.
func (m *TerminalModel) ReattachActiveTab() tea.Cmd {
	tab := m.getActiveTab()
	if tab == nil || tab.State == nil || m.workspace == nil {
		return nil
	}
	ts := tab.State
	ts.mu.Lock()
	running := ts.Running
	sessionName := ts.SessionName
	ts.mu.Unlock()
	if running {
		return func() tea.Msg {
			return messages.Toast{Message: "Terminal is still running", Level: messages.ToastInfo}
		}
	}
	ws := m.workspace
	if sessionName == "" {
		sessionName = tmux.SessionName("amux", string(ws.ID()), string(tab.ID))
	}
	return m.attachToSession(ws, tab.ID, sessionName, true, "reattach")
}

// RestartActiveTab starts a fresh tmux session for the active terminal tab.
func (m *TerminalModel) RestartActiveTab() tea.Cmd {
	tab := m.getActiveTab()
	if tab == nil || tab.State == nil || m.workspace == nil {
		return nil
	}
	ts := tab.State
	ts.mu.Lock()
	running := ts.Running
	sessionName := ts.SessionName
	ts.mu.Unlock()
	if running {
		return func() tea.Msg {
			return messages.Toast{Message: "Terminal is still running", Level: messages.ToastInfo}
		}
	}
	ws := m.workspace
	if sessionName == "" {
		sessionName = tmux.SessionName("amux", string(ws.ID()), string(tab.ID))
	}
	m.detachState(ts, false)
	_ = tmux.KillSession(sessionName, m.getTmuxOptions())
	return m.attachToSession(ws, tab.ID, sessionName, true, "restart")
}

func (m *TerminalModel) attachToSession(ws *data.Workspace, tabID TerminalTabID, sessionName string, detachExisting bool, action string) tea.Cmd {
	if ws == nil {
		return nil
	}
	// Snapshot model-dependent values so the async cmd doesn't race on TerminalModel fields.
	opts := m.getTmuxOptions()
	termWidth, termHeight := m.sessionBootstrapViewportSize()
	attachWidth, attachHeight := m.terminalContentSize()
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	env := []string{"COLORTERM=truecolor"}
	wsID := string(ws.ID())
	root := ws.Root
	instanceID := m.instanceID
	return func() tea.Msg {
		if err := ensureTmuxAvailableFn(); err != nil {
			return SidebarTerminalReattachFailed{
				WorkspaceID: wsID,
				TabID:       tabID,
				Err:         err,
				Action:      action,
			}
		}
		if action == "reattach" {
			state, err := sessionStateForFn(sessionName, opts)
			if err != nil {
				return SidebarTerminalReattachFailed{
					WorkspaceID: wsID,
					TabID:       tabID,
					Err:         err,
					Action:      action,
				}
			}
			if !state.Exists || !state.HasLivePane {
				return SidebarTerminalReattachFailed{
					WorkspaceID: wsID,
					TabID:       tabID,
					Err:         errors.New("tmux session ended"),
					Stopped:     true,
					Action:      action,
				}
			}
		}
		tags := tmux.SessionTags{
			WorkspaceID:  wsID,
			TabID:        string(tabID),
			Type:         "terminal",
			Assistant:    "terminal",
			InstanceID:   instanceID,
			SessionOwner: instanceID,
			LeaseAtMS:    time.Now().UnixMilli(),
		}
		if action == "restart" {
			tags.CreatedAt = time.Now().Unix()
		}
		var err error
		var scrollback []byte
		var postAttachScrollback []byte
		captureFullPane := false
		captureCols := attachWidth
		captureRows := attachHeight
		var snapshot tmux.PaneSnapshot
		var bootstrap sessionBootstrapCapture
		if action == "reattach" {
			bootstrap = captureExistingSessionBootstrap(sessionName, termWidth, termHeight, opts)
			snapshot = bootstrap.Snapshot
			captureFullPane = bootstrap.CaptureFullPane
		}
		command := tmux.NewClientCommand(sessionName, tmux.ClientCommandParams{
			WorkDir:        root,
			Command:        fmt.Sprintf("exec %s -l", shell),
			Options:        opts,
			Tags:           tags,
			DetachExisting: detachExisting,
		})
		term, err := newPTYWithSizeFn(command, root, env, uint16(attachHeight), uint16(attachWidth))
		if err != nil {
			if action == "reattach" {
				rollbackExistingSessionBootstrap(sessionName, bootstrap, opts)
			}
			return SidebarTerminalReattachFailed{
				WorkspaceID: wsID,
				TabID:       tabID,
				Err:         err,
				Action:      action,
			}
		}
		if action == "reattach" {
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
		}
		if err := verifyTerminalSessionTagsFn(sessionName, tags, opts); err != nil {
			logging.Warn("sidebar terminal %s: session tag verification failed for %s: %v", action, sessionName, err)
		}
		if action != "reattach" {
			captureCols, captureRows = sessionHistoryCaptureSize(sessionName, attachWidth, attachHeight, opts)
			scrollback, _ = capturePaneFn(sessionName, opts)
		}
		return SidebarTerminalReattachResult{
			WorkspaceID:          wsID,
			TabID:                tabID,
			Terminal:             term,
			SessionName:          sessionName,
			Scrollback:           scrollback,
			PostAttachScrollback: postAttachScrollback,
			CaptureCols:          captureCols,
			CaptureRows:          captureRows,
			CaptureFullPane:      captureFullPane,
			SnapshotCols:         snapshot.Cols,
			SnapshotRows:         snapshot.Rows,
			SnapshotCursorX:      snapshot.CursorX,
			SnapshotCursorY:      snapshot.CursorY,
			SnapshotHasCursor:    snapshot.HasCursor,
			SnapshotModeState:    snapshot.ModeState,
		}
	}
}
