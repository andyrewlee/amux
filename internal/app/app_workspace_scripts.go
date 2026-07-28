package app

import (
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// handleToggleWorkspaceScript starts the workspace's `run` script, or stops it
// when one is already running. The decision lives here rather than in the
// sidebar because the App owns the ScriptRunner and so is the only component
// that can answer "is it running right now?" without duplicating that state.
func (a *App) handleToggleWorkspaceScript(msg messages.ToggleWorkspaceScript) tea.Cmd {
	if msg.Workspace == nil || a.workspaceService == nil {
		return nil
	}
	return a.workspaceService.ToggleScriptAsync(msg.Workspace)
}

// isActiveWorkspace reports whether ws is the workspace the UI is showing,
// matching on root the way the rest of the app does so a rebound pointer for
// the same workspace still counts.
func (a *App) isActiveWorkspace(ws *data.Workspace) bool {
	if ws == nil || a.activeWorkspace == nil {
		return false
	}
	return rootsReferToSameWorkspace(ws.Root, a.activeWorkspace.Root)
}

// syncRunScriptIndicator reconciles the sidebar's [run] marker with whether a
// script is really still running for the active workspace.
//
// Start and stop both report their outcome, but a run script can also end on
// its own — a dev server that crashes, or a command that simply finishes — and
// nothing tells the UI when it does. Without this the marker would claim a live
// script indefinitely. It rides the existing git-status tick rather than adding
// a ticker of its own; the check is a single map lookup.
func (a *App) syncRunScriptIndicator() {
	if a.sidebar == nil || a.activeWorkspace == nil || a.workspaceService == nil {
		return
	}
	a.sidebar.SetScriptRunning(
		a.activeWorkspace.Root,
		a.workspaceService.IsScriptRunning(a.activeWorkspace),
	)
}

// handleWorkspaceScriptStateChanged reports a run-script start/stop outcome and
// syncs the sidebar indicator to the state the workspace actually ended up in.
//
// An untrusted repo script is not a failure: nothing ran, deliberately, so it
// gets the same warning-plus-trust-dialog treatment as setup rather than an
// error. A workspace with no run script configured is also not a failure — the
// user pressed a key for a feature this repo does not use, so it just says so.
func (a *App) handleWorkspaceScriptStateChanged(msg messages.WorkspaceScriptStateChanged) tea.Cmd {
	if msg.Workspace == nil {
		// Every producer names a workspace, but this handler is reachable from
		// the message dispatch, so it must not assume that.
		return nil
	}

	var cmds []tea.Cmd
	// The sidebar tracks one workspace's script state — the one it is showing —
	// so an outcome for any other workspace must not overwrite it. Toggles from
	// the sidebar always name the active workspace; this guards the case where a
	// state change lands after the user has already switched away, which would
	// otherwise blank a still-correct indicator until the next reconcile.
	if a.sidebar != nil && a.isActiveWorkspace(msg.Workspace) {
		a.sidebar.SetScriptRunning(msg.Workspace.Root, msg.Running)
	}

	switch {
	case msg.Err == nil:
		if msg.Running {
			cmds = append(cmds, a.toast.ShowSuccess("Started run script"))
		} else {
			cmds = append(cmds, a.toast.ShowSuccess("Stopped run script"))
		}

	case errors.Is(msg.Err, process.ErrNoScriptConfigured):
		cmds = append(cmds, a.toast.ShowWarning(
			"No run script configured (set \"run\" in .amux/workspaces.json)"))

	case errors.Is(msg.Err, process.ErrScriptsNotTrusted):
		var trustErr *process.ScriptsNotTrustedError
		var configHash string
		if errors.As(msg.Err, &trustErr) {
			configHash = trustErr.ConfigHash
		}
		cmds = append(cmds, a.toast.ShowWarning(fmt.Sprintf(
			"Skipped the run script for %s: repo not trusted yet", msg.Workspace.Name)))
		ws := msg.Workspace
		cmds = append(cmds, func() tea.Msg {
			return messages.ShowTrustScriptsDialog{Workspace: ws, ConfigHash: configHash}
		})

	default:
		cmds = append(cmds, common.ReportError(
			errorContext(errorServiceWorkspace, "running workspace script"),
			msg.Err,
			"Run script failed: "+msg.Err.Error(),
		))
	}

	return common.SafeBatch(cmds...)
}
