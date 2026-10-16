package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/bulkops"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// pendingOverlayOpenCap bounds the deferred-open queue. Opens beyond the cap
// are logged and dropped — an unbounded queue is a leak vector if an overlay
// wedges open; eight concurrent modal requests already exceed any realistic
// interaction.
const pendingOverlayOpenCap = 8

// anyModalOverlayVisible reports whether any modal overlay — a registry
// dialog, the file picker, or a bespoke overlay — is currently visible. The
// compose/input chains traverse the same set in chain order, so anything
// visible here is what a new overlay would hide beneath while still
// receiving input.
func (a *App) anyModalOverlayVisible() bool {
	if a.dialogOpen() || (a.filePicker != nil && a.filePicker.Visible()) {
		return true
	}
	o := a.overlays
	return (o.settings != nil && o.settings.Visible()) ||
		(o.env != nil && o.env.Visible()) ||
		(o.projectEnv != nil && o.projectEnv.Visible()) ||
		(o.scripts != nil && o.scripts.Visible()) ||
		(o.runOutput != nil && o.runOutput.Visible())
}

// requestOverlayOpen is the single arbitration point for modal overlay
// opens. When no modal overlay is visible the open runs immediately (the
// synchronous fast path — existing behavior). When one IS visible the open
// defers until the visible overlay closes: opening now would stack the new
// dialog beneath the visible overlay where it would still receive input the
// user can't see — keystrokes swallowed by an invisible dialog.
//
// The closure must capture its inputs (workspace, project, content) at call
// time — it may run messages later than the request. Anything captured that
// can drift while queued (a workspace the user may delete, a dialog token, a
// marked set) must be re-validated INSIDE the closure at drain time — the
// queue establishes open-order only, never subject validity. A future async
// bespoke-editor producer rides this same contract.
func (a *App) requestOverlayOpen(open func()) {
	if !a.anyModalOverlayVisible() {
		open()
		return
	}
	if len(a.pendingOverlayOpens) >= pendingOverlayOpenCap {
		logging.Error("overlay open queue full (%d); dropping pending open", pendingOverlayOpenCap)
		return
	}
	a.pendingOverlayOpens = append(a.pendingOverlayOpens, open)
}

// requestRunOutputOpen is requestOverlayOpen with refresh semantics for the
// run-output slot: a fetch result for the ALREADY-VISIBLE viewer replaces
// its content immediately (queueing it behind itself would deadlock), while
// a fresh open behind a different overlay defers like any other open.
func (a *App) requestRunOutputOpen(open func()) {
	if a.overlays.runOutput != nil && a.overlays.runOutput.Visible() {
		open()
		return
	}
	a.requestOverlayOpen(open)
}

// drainPendingOverlayOpens runs deferred opens while nothing modal is
// visible. Called once per Update (after the message is fully handled), so
// an overlay closing on this message unblocks the queue immediately. Opens
// run as func() — a deferred open that needs a follow-up cmd (arming the
// run-output refresh tick once the viewer actually presents) appends it to
// drainedOverlayCmds, which this returns for Update to batch.
func (a *App) drainPendingOverlayOpens() tea.Cmd {
	for len(a.pendingOverlayOpens) > 0 && !a.anyModalOverlayVisible() {
		open := a.pendingOverlayOpens[0]
		a.pendingOverlayOpens[0] = nil
		a.pendingOverlayOpens = a.pendingOverlayOpens[1:]
		open()
	}
	if len(a.drainedOverlayCmds) == 0 {
		return nil
	}
	cmds := a.drainedOverlayCmds
	a.drainedOverlayCmds = nil
	return common.SafeBatch(cmds...)
}

// workspaceLoaded reports whether ws is currently resolvable against the
// loaded projects — the request-time membership check for reResolveOpenTarget's
// required flag.
func (a *App) workspaceLoaded(ws *data.Workspace) bool {
	if ws == nil {
		return false
	}
	live, _ := a.findWorkspaceAndProjectByID(string(ws.ID()))
	return live != nil
}

// reResolveOpenTarget re-resolves a queued open's captured workspace against
// the loaded projects at drain time (requestOverlayOpen's contract). It
// returns the live workspace and owning project — not the captured pointers —
// so the dialog presents and binds current state.
//
// required must carry whether the subject was loaded at REQUEST time
// (workspaceLoaded evaluated by the handler before queueing). A subject that
// was loaded then and is gone now is a stale open — ok=false and the caller
// skips: a confirmation whose subject no longer exists must never present.
// Subjects that were never loaded (ok still true, live is the captured
// pointer) present as before — the fence is drift, not existence.
func (a *App) reResolveOpenTarget(ws *data.Workspace, required bool) (live *data.Workspace, proj *data.Project, ok bool) {
	if ws == nil {
		return nil, nil, !required
	}
	live, proj = a.findWorkspaceAndProjectByID(string(ws.ID()))
	if live != nil {
		return live, proj, true
	}
	if required {
		logging.Debug("Dropping stale overlay open: workspace %s no longer loaded", ws.Name)
		return nil, nil, false
	}
	return ws, nil, true
}

// presentWorkspaceDialog owns the deferred-open contract for
// workspace-subject dialogs — the prologue every such handler used to
// re-perform by hand: workspaceLoaded captured at request time →
// requestOverlayOpen arbitration → reResolveOpenTarget at drain →
// clearPendingWorkspaceCreate → build(live ws, proj). build receives the
// re-resolved subject — reading the captured message's workspace inside it
// would bypass the staleness fence this helper exists to provide.
//
// The pending-create clear runs on present only: a dropped stale open never
// tears down an unrelated in-flight create handoff.
//
// New workspace-subject Show* handlers must call this rather than
// requestOverlayOpen directly. Non-workspace subjects (project env, session
// pickers, file pickers, bespoke overlays) keep requestOverlayOpen plus
// their own drain-time fence — see handleProjectEnvReady's token re-check
// for the pattern.
func (a *App) presentWorkspaceDialog(ws *data.Workspace, build func(live *data.Workspace, proj *data.Project)) {
	required := a.workspaceLoaded(ws)
	a.requestOverlayOpen(func() {
		live, proj, ok := a.reResolveOpenTarget(ws, required)
		if !ok {
			return
		}
		a.clearPendingWorkspaceCreate()
		build(live, proj)
	})
}

// presentWorkspaceOverlay is presentWorkspaceDialog's bespoke-overlay
// sibling for workspace subjects — same re-resolve contract, arbitrated
// through requestRunOutputOpen (the runOutput slot's refresh semantics)
// instead, and without the pending-create clear these non-dialog overlays
// never ran.
func (a *App) presentWorkspaceOverlay(ws *data.Workspace, build func(live *data.Workspace)) {
	required := a.workspaceLoaded(ws)
	a.requestRunOutputOpen(func() {
		live, _, ok := a.reResolveOpenTarget(ws, required)
		if !ok {
			return
		}
		build(live)
	})
}

// presentBulkTargetsDialog is presentWorkspaceDialog's marked-set sibling —
// the same contract with a bulk target set as the subject and
// resolveBulkTargets as the drain-time fence: a fully-loaded set whose
// membership drifted while queued invalidates the whole open, since the
// typed count and row list were computed from the request-time set.
func (a *App) presentBulkTargetsDialog(targets []bulkops.Target, shelved bool, build func(resolved []bulkops.Target)) {
	required := a.bulkTargetsRequired(targets, shelved)
	a.requestOverlayOpen(func() {
		resolved, ok := a.resolveBulkTargets(targets, shelved, required)
		if !ok {
			return
		}
		a.clearPendingWorkspaceCreate()
		build(resolved)
	})
}

// clearPendingHandoffs drops every staged dialog handoff — the create→
// agent-picker binding and the picker→task binding. Called on paths that
// abandon all in-flight flows at once (panic recovery); per-field clears
// that run conditionally (the displaced-result path binds each to its own
// dialog ID) stay manual at their sites.
func (a *App) clearPendingHandoffs() {
	a.pendingWorkspaceCreate = pendingWorkspaceCreateState{}
	a.pendingLaunchTask = pendingLaunchTaskState{}
}
