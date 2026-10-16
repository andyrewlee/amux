package sidebar

import (
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/pty"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/ptyio"
)

// These package-level indirections are test seams for terminal attach/bootstrap
// paths. The bootstrap seams themselves live in ptyio (ptyio.ProbeSessionFn
// et al.); tests that override any of them must not use t.Parallel within
// this package.
var (
	ensureTmuxAvailableFn       = tmux.EnsureAvailable
	sessionStateForFn           = tmux.SessionStateFor
	sessionOwnedFn              = ptyio.SessionOwned
	newPTYWithSizeFn            = pty.NewTmuxClientWithSize
	capturePaneFn               = tmux.CapturePane
	verifyTerminalSessionTagsFn = verifyTerminalSessionTags
)

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
	opts := m.tmuxOpts
	instanceID := m.instanceID
	root := ws.Root
	envFn := m.sessionEnvProvider

	return func() tea.Msg {
		loginShellCommand, err := pty.LoginShellCommandFromEnv()
		if err != nil {
			return SidebarTerminalCreateFailed{WorkspaceID: wsID, Err: err}
		}
		sessionName := tmux.SessionName("amux", wsID, string(tabID))
		// Reuse scrollback if a prior tmux session with the same name exists
		// (e.g., app restart with persisted tmux session). The reuse check also
		// verifies ownership tags: a live session under our name that isn't
		// amux-tagged is a foreign squatter, and attaching would both hand it a
		// client and launder it by re-tagging it as ours.
		precheck := func() (bool, error) {
			state, err := sessionStateForFn(sessionName, opts)
			if err != nil || !ptyio.SessionAttachable(state) {
				return false, nil
			}
			owned, ownErr := sessionOwnedFn(sessionName, data.WorkspaceIdentityStrings(ws), opts)
			if ownErr != nil {
				return false, ownErr
			}
			if !owned {
				return false, errors.New("tmux session name already in use by a non-amux session")
			}
			return true, nil
		}
		res, err := spawnSidebarTerminal(sidebarSpawnParams{
			ws:             ws,
			sessionName:    sessionName,
			loginShell:     loginShellCommand,
			root:           root,
			envFn:          envFn,
			tags:           ptyio.AttachSessionTags(ws, string(tabID), "terminal", "terminal", instanceID, true),
			detachExisting: true,
			opts:           opts,
			attachWidth:    attachWidth,
			attachHeight:   attachHeight,
			termWidth:      termWidth,
			termHeight:     termHeight,
			precheck:       precheck,
			verifyLabel:    "create",
		})
		if err != nil {
			return SidebarTerminalCreateFailed{WorkspaceID: wsID, Err: err}
		}
		return SidebarTerminalCreated{
			WorkspaceID: wsID,
			TabID:       tabID,
			Terminal:    res.term,
			SessionName: sessionName,
			CaptureCols: res.captureCols,
			CaptureRows: res.captureRows,
			SessionRestoreCapture: ptyio.SessionRestoreCapture{
				ScrollbackCapture:           res.scrollback,
				PostAttachScrollbackCapture: res.postAttachScrollback,
				CaptureFullPane:             res.captureFullPane,
				SnapshotCols:                res.snapshot.Cols,
				SnapshotRows:                res.snapshot.Rows,
				SnapshotCursorX:             res.snapshot.CursorX,
				SnapshotCursorY:             res.snapshot.CursorY,
				SnapshotHasCursor:           res.snapshot.HasCursor,
				SnapshotModeState:           res.snapshot.ModeState,
			},
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
	var running, began bool
	var sessionName string
	var epoch uint64
	withMu(&ts.mu, func() {
		running = ts.Running
		sessionName = ts.SessionName
		began = !running && ts.beginReattachLocked()
		if began {
			// An explicit reattach is consent to attach again — the attempt's own
			// outcome now owns clearing the flag.
			ts.UserDetached = false
		}
		epoch = ts.reattachEpoch
	})
	if running {
		return func() tea.Msg {
			return messages.Toast{Message: "Terminal is still running", Level: messages.ToastInfo}
		}
	}
	if !began {
		// An attach is already in flight; say so rather than silently
		// dispatching a second one — its late result would be rejected anyway.
		return func() tea.Msg {
			return messages.Toast{Message: "Reattach already in progress", Level: messages.ToastInfo}
		}
	}
	ws := m.workspace
	if sessionName == "" {
		sessionName = tmux.SessionName("amux", string(ws.ID()), string(tab.ID))
	}
	return m.attachToSession(ws, tab.ID, sessionName, true, "reattach", epoch)
}

// RestartActiveTab starts a fresh tmux session for the active terminal tab.
func (m *TerminalModel) RestartActiveTab() tea.Cmd {
	tab := m.getActiveTab()
	if tab == nil || tab.State == nil || m.workspace == nil {
		return nil
	}
	ts := tab.State
	var running bool
	var sessionName string
	withMu(&ts.mu, func() {
		running = ts.Running
		sessionName = ts.SessionName
	})
	if running {
		return func() tea.Msg {
			return messages.Toast{Message: "Terminal is still running", Level: messages.ToastInfo}
		}
	}
	ws := m.workspace
	if sessionName == "" {
		sessionName = tmux.SessionName("amux", string(ws.ID()), string(tab.ID))
	}
	// detachState invalidates any in-flight attach before the new attempt
	// begins, so its late result cannot overwrite the restarted terminal.
	m.detachState(ts, false)
	if err := ptyio.KillSessionFn(sessionName, m.tmuxOpts); err != nil {
		// The attach path below is has-session||new-session, so a surviving
		// session is silently reattached — a "restart" that wasn't must be
		// diagnosable.
		logging.Error("Failed to kill tmux session %s during sidebar terminal restart: %v", sessionName, err)
	}
	var began bool
	var epoch uint64
	withMu(&ts.mu, func() {
		began = ts.beginReattachLocked()
		epoch = ts.reattachEpoch
	})
	if !began {
		return nil
	}
	return m.attachToSession(ws, tab.ID, sessionName, true, "restart", epoch)
}

// attachFailure builds the epoch-stamped failure result for an attach
// attempt: every early exit reports the attempt it was dispatched under so a
// superseded attempt's failure is dropped rather than applied.
func attachFailure(wsID string, tabID TerminalTabID, epoch uint64, action string, err error, stopped bool) SidebarTerminalReattachFailed {
	return SidebarTerminalReattachFailed{
		WorkspaceID: wsID,
		TabID:       tabID,
		Epoch:       epoch,
		Err:         err,
		Stopped:     stopped,
		Action:      action,
	}
}

// reattachTargetCheck validates a "reattach" target: the session must exist,
// be attachable, and be owned by this workspace. Dead-session policy is
// ptyio.SessionAttachable and ownership is ptyio.SessionOwned, both shared
// with center. A foreign session squatting the name reports Stopped so the
// user restarts explicitly — restart kills the squatter before recreating.
// Returns nil when the session may be attached.
func reattachTargetCheck(ws *data.Workspace, sessionName string, opts tmux.Options, wsID string, tabID TerminalTabID, epoch uint64) *SidebarTerminalReattachFailed {
	state, err := sessionStateForFn(sessionName, opts)
	if err != nil {
		f := attachFailure(wsID, tabID, epoch, "reattach", err, false)
		return &f
	}
	if !ptyio.SessionAttachable(state) {
		f := attachFailure(wsID, tabID, epoch, "reattach", errors.New("tmux session ended"), true)
		return &f
	}
	owned, ownErr := sessionOwnedFn(sessionName, data.WorkspaceIdentityStrings(ws), opts)
	if ownErr != nil {
		f := attachFailure(wsID, tabID, epoch, "reattach", ownErr, false)
		return &f
	}
	if !owned {
		f := attachFailure(wsID, tabID, epoch, "reattach", errors.New("tmux session is not owned by this workspace"), true)
		return &f
	}
	return nil
}

func (m *TerminalModel) attachToSession(ws *data.Workspace, tabID TerminalTabID, sessionName string, detachExisting bool, action string, epoch uint64) tea.Cmd {
	if ws == nil {
		return nil
	}
	// Snapshot model-dependent values so the async cmd doesn't race on TerminalModel fields.
	opts := m.tmuxOpts
	termWidth, termHeight := m.sessionBootstrapViewportSize()
	attachWidth, attachHeight := m.terminalContentSize()
	loginShellCommand, shellErr := pty.LoginShellCommandFromEnv()
	wsID := string(ws.ID())
	root := ws.Root
	instanceID := m.instanceID
	envFn := m.sessionEnvProvider
	return func() tea.Msg {
		if shellErr != nil {
			return attachFailure(wsID, tabID, epoch, action, shellErr, false)
		}
		if action == "reattach" {
			if failed := reattachTargetCheck(ws, sessionName, opts, wsID, tabID, epoch); failed != nil {
				return *failed
			}
		}
		res, err := spawnSidebarTerminal(sidebarSpawnParams{
			ws:             ws,
			sessionName:    sessionName,
			loginShell:     loginShellCommand,
			root:           root,
			envFn:          envFn,
			tags:           ptyio.AttachSessionTags(ws, string(tabID), "terminal", "terminal", instanceID, action != "reattach"),
			detachExisting: detachExisting,
			opts:           opts,
			attachWidth:    attachWidth,
			attachHeight:   attachHeight,
			termWidth:      termWidth,
			termHeight:     termHeight,
			precheck: func() (bool, error) {
				// The reattach path's existence/ownership check ran above;
				// here the flag only gates the bootstrap-capture lifecycle.
				return action == "reattach", nil
			},
			restartCapture: action != "reattach",
			verifyLabel:    action,
		})
		if err != nil {
			return attachFailure(wsID, tabID, epoch, action, err, false)
		}
		return SidebarTerminalReattachResult{
			WorkspaceID: wsID,
			TabID:       tabID,
			Epoch:       epoch,
			Terminal:    res.term,
			SessionName: sessionName,
			CaptureCols: res.captureCols,
			CaptureRows: res.captureRows,
			SessionRestoreCapture: ptyio.SessionRestoreCapture{
				ScrollbackCapture:           res.scrollback,
				PostAttachScrollbackCapture: res.postAttachScrollback,
				CaptureFullPane:             res.captureFullPane,
				SnapshotCols:                res.snapshot.Cols,
				SnapshotRows:                res.snapshot.Rows,
				SnapshotCursorX:             res.snapshot.CursorX,
				SnapshotCursorY:             res.snapshot.CursorY,
				SnapshotHasCursor:           res.snapshot.HasCursor,
				SnapshotModeState:           res.snapshot.ModeState,
			},
		}
	}
}
