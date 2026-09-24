package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
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

// requestRunScriptStatus emits the run-script status check as a Cmd rather
// than running it inline: RunScriptStatus shells `tmux list-sessions` plus a
// display-message per session, and a wedged tmux would otherwise freeze the
// Update loop for up to the tmux timeout every tick. It returns nil when a
// check is already in flight so a wedged server degrades to stale indicators
// instead of piling up goroutines.
func (a *App) requestRunScriptStatus() tea.Cmd {
	if a.sidebar == nil || a.activeWorkspace == nil || a.workspaceService == nil {
		return nil
	}
	if a.runScriptStatusInFlight {
		return nil
	}
	ws := a.activeWorkspace
	a.runScriptStatusInFlight = true
	stampedIDs := workspacesvc.WorkspaceIDStrings(ws)
	svc := a.workspaceService
	return func() tea.Msg {
		alive, lastExit := svc.RunScriptStatus(ws)
		return messages.RunScriptStatusResult{
			Workspace:    ws,
			WorkspaceIDs: stampedIDs,
			Alive:        alive,
			LastExit:     lastExit,
		}
	}
}

// handleRunScriptStatusResult reconciles the sidebar's [run] marker with the
// status result. Stale results — the user switched or shelved the workspace
// while the check was in flight — are dropped by identity so they cannot
// clobber the current workspace's indicator.
//
// Start and stop both report their outcome, but a run script can also end on
// its own — a dev server that crashes, or a command that simply finishes — and
// nothing tells the UI when it does. Without this the marker would claim a live
// script indefinitely. It rides the existing git-status tick rather than adding
// a ticker of its own.
func (a *App) handleRunScriptStatusResult(msg messages.RunScriptStatusResult) tea.Cmd {
	a.runScriptStatusInFlight = false
	if a.sidebar == nil || a.activeWorkspace == nil || msg.Workspace == nil {
		return nil
	}
	current := workspacesvc.WorkspaceIDsOrComputed(a.activeWorkspace, nil)
	relevant := false
	for _, want := range current {
		for _, got := range msg.WorkspaceIDs {
			if got == want {
				relevant = true
				break
			}
		}
	}
	if !relevant {
		return nil
	}
	ws := msg.Workspace
	alive, lastExit := msg.Alive, msg.LastExit
	a.sidebar.SetScriptRunning(ws.Root, alive)

	// Surface a crashed run instead of silently clearing the badge: when the
	// tracked state flips running→stopped for this workspace with a non-zero
	// exit, say so and point at the captured output. Tracked per-root so
	// switching workspaces reseeds rather than misfiring.
	root := data.NormalizePath(ws.Root)
	if a.lastRunScriptRoot != root {
		a.lastRunScriptRoot = root
		a.lastRunScriptAlive = alive
		return nil
	}
	if a.lastRunScriptAlive && !alive && lastExit > 0 {
		a.lastRunScriptAlive = false
		return a.toast.ShowWarning(fmt.Sprintf(
			"Run script for %s exited with status %d — press R for output", ws.Name, lastExit))
	}
	a.lastRunScriptAlive = alive
	return nil
}

// handleShowRunScriptOutput opens the read-only viewer with the workspace's
// current run output — the live pane tail while running, or the
// remain-on-exit tail after it exits. Under the subprocess fallback (tests
// without a host) there is nothing to show, so it says so rather than opening
// an empty dialog. While the session is alive the viewer re-captures the tail
// on runOutputTickInterval — a poll-follow pair to the dialog's `f` toggle.
func (a *App) handleShowRunScriptOutput(msg messages.ShowRunScriptOutput) tea.Cmd {
	if msg.Workspace == nil || a.workspaceService == nil {
		return nil
	}
	// The reads are tmux subprocess calls — never run them on the Update
	// loop (a wedged tmux would freeze the whole TUI for ~2 timeouts, the
	// defect class the status-indicator fix addressed). Bump the token
	// at request time so a second R press invalidates an in-flight open.
	a.overlays.runOutputToken++
	token, ws, svc := a.overlays.runOutputToken, msg.Workspace, a.workspaceService
	return func() tea.Msg {
		content, alive, _ := svc.RunScriptOutputAndStatus(ws, 400)
		return runOutputOpenedMsg{token: token, ws: ws, content: content, alive: alive}
	}
}

// runOutputOpenedMsg delivers an R-open fetch result: the tail content and
// whether the run session was alive at fetch time.
type runOutputOpenedMsg struct {
	token   int
	ws      *data.Workspace
	content string
	alive   bool
}

// handleRunOutputOpened applies an open fetch under the token guard — a
// stale open (superseded by a later R/O press or close) is dropped, not
// applied to the newer dialog state.
func (a *App) handleRunOutputOpened(msg runOutputOpenedMsg) tea.Cmd {
	if msg.token != a.overlays.runOutputToken || msg.ws == nil {
		return nil
	}
	if msg.content == "" {
		return a.toast.ShowInfo("No run output for " + msg.ws.Name)
	}
	a.requestRunOutputOpen(func() {
		a.overlays.runOutputWorkspace = msg.ws
		a.overlays.runOutputAttachable = true
		a.overlays.runOutput = common.NewOutputDialog("Run output — "+msg.ws.Name, msg.content)
		a.overlays.runOutput.SetAttachHint(true)
		a.overlays.runOutput.SetSize(a.width, a.height)
		a.overlays.runOutput.Show()
	})
	if msg.alive {
		return a.scheduleRunOutputTick(a.overlays.runOutputToken)
	}
	return nil
}

// runOutputTickMsg fires the periodic re-capture while the run-output viewer
// is open. Token matches the dialog instance it was scheduled for — a stale
// tick (dialog closed or reopened since) is dropped, not re-issued.
type runOutputTickMsg struct{ token int }

// runOutputRefreshedMsg carries one fetch's result back to the loop: the new
// tail content plus whether the session was still alive at fetch time (the
// last alive→dead fetch captures the remain-on-exit tail, then the loop
// stops).
type runOutputRefreshedMsg struct {
	token   int
	content string
	alive   bool
}

// scheduleRunOutputTick arms the next refresh for this dialog instance.
func (a *App) scheduleRunOutputTick(token int) tea.Cmd {
	return common.SafeTick(runOutputTickInterval, func(time.Time) tea.Msg {
		return runOutputTickMsg{token: token}
	})
}

// handleRunOutputTick turns a live tick into an off-loop fetch: tmux capture
// is a subprocess, so it must not run inside Update.
func (a *App) handleRunOutputTick(msg runOutputTickMsg) tea.Cmd {
	if msg.token != a.overlays.runOutputToken || a.overlays.runOutput == nil || !a.overlays.runOutput.Visible() {
		return nil
	}
	ws := a.overlays.runOutputWorkspace
	if ws == nil || a.workspaceService == nil {
		return nil
	}
	token, svc := a.overlays.runOutputToken, a.workspaceService
	return func() tea.Msg {
		content, alive, _ := svc.RunScriptOutputAndStatus(ws, 400)
		return runOutputRefreshedMsg{token: token, content: content, alive: alive}
	}
}

// handleRunOutputRefreshed applies a fetch result: update the snapshot, then
// re-arm the tick only while the session is alive. A dead session's fetch is
// the final one — it already captured the remain-on-exit tail.
func (a *App) handleRunOutputRefreshed(msg runOutputRefreshedMsg) tea.Cmd {
	if msg.token != a.overlays.runOutputToken || a.overlays.runOutput == nil || !a.overlays.runOutput.Visible() {
		return nil
	}
	a.overlays.runOutput.SetContent(msg.content)
	if msg.alive {
		return a.scheduleRunOutputTick(a.overlays.runOutputToken)
	}
	return nil
}

// closeRunOutputDialog clears the viewer state and kills its refresh loop by
// bumping the token — an in-flight fetch or a scheduled tick then drops as
// stale instead of mutating a closed (or next) dialog.
func (a *App) closeRunOutputDialog() {
	a.overlays.runOutput = nil
	a.overlays.runOutputWorkspace = nil
	a.overlays.runOutputAttachable = false
	a.overlays.runOutputToken++
}

// handleShowScriptOutput opens the read-only viewer with the workspace's
// recorded lifecycle-script transcripts — the last setup, archive, and
// on-done run, each bounded by the runner's tail buffer. Unlike the run
// viewer the content is static (the scripts already finished), so no
// refresh tick is armed.
func (a *App) handleShowScriptOutput(msg messages.ShowScriptOutput) tea.Cmd {
	if msg.Workspace == nil || a.workspaceService == nil {
		return nil
	}
	content := composeScriptOutputView(a.workspaceService.LastScriptOutputs(msg.Workspace))
	if content == "" {
		return a.toast.ShowInfo("No lifecycle script output for " + msg.Workspace.Name)
	}
	a.requestRunOutputOpen(func() {
		a.overlays.runOutputWorkspace = msg.Workspace
		a.overlays.runOutputAttachable = false
		a.overlays.runOutputToken++
		a.overlays.runOutput = common.NewOutputDialog("Script output — "+msg.Workspace.Name, content)
		a.overlays.runOutput.SetSize(a.width, a.height)
		a.overlays.runOutput.Show()
	})
	return nil
}

// composeScriptOutputView renders the recorded lifecycle transcripts as one
// scrollable document: a section per script type in lifecycle order, each
// headed by its outcome and finish time.
func composeScriptOutputView(outputs map[process.ScriptType]process.ScriptOutput) string {
	var b strings.Builder
	for _, st := range []process.ScriptType{process.ScriptSetup, process.ScriptArchive, process.ScriptOnDone} {
		entry, ok := outputs[st]
		if !ok {
			continue
		}
		outcome := "ok"
		if entry.Err != "" {
			outcome = "failed — " + entry.Err
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "── %s ── %s · %s\n\n", st, outcome, entry.FinishedAt.Format("15:04:05"))
		b.WriteString(entry.Text)
	}
	return b.String()
}

// lifecycleScriptExitedMsg reports a detached lifecycle script's non-zero
// exit — the runner's exit-listener callback enqueues it through the
// external pump. Today only on-done is detached (setup/archive return their
// outcome to a synchronous caller); the scriptType is carried so any future
// detached hook routes correctly.
type lifecycleScriptExitedMsg struct {
	workspace  *data.Workspace
	scriptType process.ScriptType
	err        error
}

// handleLifecycleScriptExited surfaces a detached lifecycle failure the
// user would otherwise never see: an on-done hook that exits non-zero used
// to die in a Debug log. The transcript is already recorded, so the toast
// points at the O viewer — phrased for whether the failed workspace is the
// one the sidebar is showing.
func (a *App) handleLifecycleScriptExited(msg lifecycleScriptExitedMsg) tea.Cmd {
	name := "workspace"
	active := false
	if msg.workspace != nil {
		name = msg.workspace.Name
		active = a.isActiveWorkspace(msg.workspace)
	}
	hint := "select it and press O for output"
	if active {
		hint = "press O for output"
	}
	return a.toast.ShowWarning(fmt.Sprintf("%s hook failed for %s (%s) — %s",
		msg.scriptType, name, msg.err, hint))
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

// handleWorkspaceOnDoneResult surfaces an on-done hook spawn failure. The
// trust case mirrors the run script: skipping an unapproved repo hook is a
// deliberate security outcome, not an error — warn and offer the trust
// dialog so the user can approve it for the next edge. A genuine spawn
// failure gets the standard error path.
func (a *App) handleWorkspaceOnDoneResult(msg messages.WorkspaceOnDoneResult) tea.Cmd {
	if msg.Err == nil {
		return nil
	}
	if errors.Is(msg.Err, process.ErrScriptsNotTrusted) {
		var trustErr *process.ScriptsNotTrustedError
		var configHash string
		if errors.As(msg.Err, &trustErr) {
			configHash = trustErr.ConfigHash
		}
		name := "workspace"
		ws := msg.Workspace
		if ws != nil {
			name = ws.Name
		}
		var cmds []tea.Cmd
		cmds = append(cmds, a.toast.ShowWarning(fmt.Sprintf(
			"Skipped the on-done hook for %s: repo not trusted yet", name)))
		if ws != nil {
			cmds = append(cmds, func() tea.Msg {
				return messages.ShowTrustScriptsDialog{Workspace: ws, ConfigHash: configHash}
			})
		}
		return common.SafeBatch(cmds...)
	}
	return common.ReportError(
		errorContext(errorServiceWorkspace, "running on-done hook"),
		msg.Err,
		"on-done hook failed: "+msg.Err.Error(),
	)
}

// attachRunViewerCmd resolves the newest alive run session for ws and reports
// the target back for dispatch — the lookup is a tmux sweep, so it runs off
// the UI goroutine.
func (a *App) attachRunViewerCmd(ws *data.Workspace) tea.Cmd {
	svc := a.workspaceService
	if ws == nil || svc == nil {
		return nil
	}
	return func() tea.Msg {
		name, ok := svc.RunScriptAttachTarget(ws)
		return runAttachTargetMsg{ws: ws, name: name, ok: ok}
	}
}

// runAttachTargetMsg carries the attach-target lookup result back to the UI.
type runAttachTargetMsg struct {
	ws   *data.Workspace
	name string
	ok   bool
}

// handleRunAttachTarget closes the overlay and opens the interactive
// run-viewer tab — the tab replaces the read-only viewer's job.
func (a *App) handleRunAttachTarget(msg runAttachTargetMsg) tea.Cmd {
	if msg.ws == nil {
		return nil
	}
	if !msg.ok || msg.name == "" {
		return a.toast.ShowInfo("No live run session for " + msg.ws.Name)
	}
	a.closeRunOutputDialog()
	if a.center == nil {
		return nil
	}
	newCenter, cmd := a.center.Update(messages.AttachRunSession{Workspace: msg.ws, SessionName: msg.name})
	a.center = newCenter
	return cmd
}
