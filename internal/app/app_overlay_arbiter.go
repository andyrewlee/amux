package app

import "github.com/andyrewlee/amux/internal/logging"

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
// time — it may run messages later than the request.
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
// an overlay closing on this message unblocks the queue immediately.
func (a *App) drainPendingOverlayOpens() {
	for len(a.pendingOverlayOpens) > 0 && !a.anyModalOverlayVisible() {
		open := a.pendingOverlayOpens[0]
		a.pendingOverlayOpens[0] = nil
		a.pendingOverlayOpens = a.pendingOverlayOpens[1:]
		open()
	}
}
