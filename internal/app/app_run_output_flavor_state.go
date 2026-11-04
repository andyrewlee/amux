package app

import (
	"errors"
	"fmt"

	"github.com/andyrewlee/amux/internal/fsm"
	"github.com/andyrewlee/amux/internal/logging"
)

// runOutputFlavor is which of the four OutputDialog flavors currently owns
// the shared runOutput slot. It replaces the former flavor flags — the
// runOutputAttachable/runOutputLifecycle booleans, the runOutputSession
// ""-sentinel's flavor meaning, and runOutputReleaseCount's 0-means-disabled
// sentinel — with one enum, so "which dialog is this" can no longer drift
// from "which intercepts are armed". The hazard the flags invited: one
// flavor-open path forgetting a single reset left the next viewer armed with
// the previous flavor's key intercept (e.g. `a`-attach live on a transcript
// viewer). With a single enum that class of bug is unwritable: flavor and
// intercepts are the same value.
type runOutputFlavor uint8

const (
	// runOutputClosed: no viewer in the slot.
	runOutputClosed runOutputFlavor = iota
	// runOutputFlavorRun: the live-run viewer (`R`) — pinned to one tmux run
	// session, `a` attaches an interactive tab to it, refresh ticks re-tail
	// while the session lives.
	runOutputFlavorRun
	// runOutputFlavorStatus: the workspace-status panel (`i`) — `R` releases
	// orphaned port reservations and `U` revokes script trust when the
	// status data arms them.
	runOutputFlavorStatus
	// runOutputFlavorScript: the lifecycle-transcript viewer (`O`) — recorded
	// setup/archive/on-done transcripts plus the live setup tail; no session,
	// no attach.
	runOutputFlavorScript
	// runOutputFlavorEvents: the session notification recall surface — a
	// static document; no session, no attach, no refresh tick.
	runOutputFlavorEvents
)

func (f runOutputFlavor) String() string {
	switch f {
	case runOutputFlavorRun:
		return "run"
	case runOutputFlavorStatus:
		return "status"
	case runOutputFlavorScript:
		return "script"
	case runOutputFlavorEvents:
		return "events"
	default:
		return "closed"
	}
}

// runOutputFlavorEvent is the flavor machine's alphabet: one open event per
// flavor plus close. The token (runOutputToken) is extended state — it
// fences in-flight opens and refresh ticks and stays at the call sites.
type runOutputFlavorEvent uint8

const (
	runOutputEvtOpenRun runOutputFlavorEvent = iota
	runOutputEvtOpenStatus
	runOutputEvtOpenScript
	runOutputEvtOpenEvents
	runOutputEvtClose
)

func (e runOutputFlavorEvent) String() string {
	switch e {
	case runOutputEvtOpenRun:
		return "open-run"
	case runOutputEvtOpenStatus:
		return "open-status"
	case runOutputEvtOpenScript:
		return "open-script"
	case runOutputEvtOpenEvents:
		return "open-events"
	default:
		return "close"
	}
}

// runOutputFlavorEventFor maps a flavor to its open event.
func runOutputFlavorEventFor(f runOutputFlavor) runOutputFlavorEvent {
	switch f {
	case runOutputFlavorRun:
		return runOutputEvtOpenRun
	case runOutputFlavorStatus:
		return runOutputEvtOpenStatus
	case runOutputFlavorScript:
		return runOutputEvtOpenScript
	case runOutputFlavorEvents:
		return runOutputEvtOpenEvents
	default:
		return runOutputEvtClose
	}
}

// runOutputFlavorMachine is the shared-slot flavor table. Every open event
// is legal from every state: requestRunOutputOpen applies a fetch for the
// already-visible viewer immediately (queueing it behind itself would
// deadlock), so any flavor may replace any flavor. Close returns to closed
// from every open flavor — the R/U intercepts and Esc all route through
// closeRunOutputDialog.
var runOutputFlavorMachine = fsm.MustCompile(fsm.Machine[runOutputFlavor, runOutputFlavorEvent]{
	Name:    "run-output-flavor",
	Initial: runOutputClosed,
	Steps: []fsm.Step[runOutputFlavor, runOutputFlavorEvent]{
		{From: runOutputClosed, Event: runOutputEvtOpenRun, To: runOutputFlavorRun},
		{From: runOutputClosed, Event: runOutputEvtOpenStatus, To: runOutputFlavorStatus},
		{From: runOutputClosed, Event: runOutputEvtOpenScript, To: runOutputFlavorScript},
		{From: runOutputClosed, Event: runOutputEvtOpenEvents, To: runOutputFlavorEvents},
		{From: runOutputFlavorRun, Event: runOutputEvtOpenRun, To: runOutputFlavorRun},
		{From: runOutputFlavorRun, Event: runOutputEvtOpenStatus, To: runOutputFlavorStatus},
		{From: runOutputFlavorRun, Event: runOutputEvtOpenScript, To: runOutputFlavorScript},
		{From: runOutputFlavorRun, Event: runOutputEvtOpenEvents, To: runOutputFlavorEvents},
		{From: runOutputFlavorRun, Event: runOutputEvtClose, To: runOutputClosed},
		{From: runOutputFlavorStatus, Event: runOutputEvtOpenRun, To: runOutputFlavorRun},
		{From: runOutputFlavorStatus, Event: runOutputEvtOpenStatus, To: runOutputFlavorStatus},
		{From: runOutputFlavorStatus, Event: runOutputEvtOpenScript, To: runOutputFlavorScript},
		{From: runOutputFlavorStatus, Event: runOutputEvtOpenEvents, To: runOutputFlavorEvents},
		{From: runOutputFlavorStatus, Event: runOutputEvtClose, To: runOutputClosed},
		{From: runOutputFlavorScript, Event: runOutputEvtOpenRun, To: runOutputFlavorRun},
		{From: runOutputFlavorScript, Event: runOutputEvtOpenStatus, To: runOutputFlavorStatus},
		{From: runOutputFlavorScript, Event: runOutputEvtOpenScript, To: runOutputFlavorScript},
		{From: runOutputFlavorScript, Event: runOutputEvtOpenEvents, To: runOutputFlavorEvents},
		{From: runOutputFlavorScript, Event: runOutputEvtClose, To: runOutputClosed},
		{From: runOutputFlavorEvents, Event: runOutputEvtOpenRun, To: runOutputFlavorRun},
		{From: runOutputFlavorEvents, Event: runOutputEvtOpenStatus, To: runOutputFlavorStatus},
		{From: runOutputFlavorEvents, Event: runOutputEvtOpenScript, To: runOutputFlavorScript},
		{From: runOutputFlavorEvents, Event: runOutputEvtOpenEvents, To: runOutputFlavorEvents},
		{From: runOutputFlavorEvents, Event: runOutputEvtClose, To: runOutputClosed},
		// Idempotent close: closeRunOutputDialog runs from paths (Esc, the
		// R/U dialog swaps) that do not first check the slot is open.
		{From: runOutputClosed, Event: runOutputEvtClose, To: runOutputClosed},
	},
})

// runOutputFlavorCheck formally verifies the flavor table: every flavor is
// reachable, close always lands on closed, every open event lands on exactly
// its own flavor (an open path can never leave another flavor's intercept
// data in place — the state fully determines the flavor), and the open
// events are total so a replace never wedges the slot. Mutual exclusivity of
// the flavors is structural (one enum replaced the four flag fields).
func runOutputFlavorCheck() error {
	return runOutputFlavorMachine.Check(fsm.Checker[runOutputFlavor, runOutputFlavorEvent]{
		Alphabet: []runOutputFlavorEvent{
			runOutputEvtOpenRun, runOutputEvtOpenStatus, runOutputEvtOpenScript,
			runOutputEvtOpenEvents, runOutputEvtClose,
		},
		AllDeclaredReachable: true,
		Edge: func(_ runOutputFlavor, event runOutputFlavorEvent, to runOutputFlavor) error {
			if event == runOutputEvtClose {
				if to != runOutputClosed {
					return errors.New("closing must always land on closed")
				}
				return nil
			}
			want := runOutputClosed
			switch event {
			case runOutputEvtOpenRun:
				want = runOutputFlavorRun
			case runOutputEvtOpenStatus:
				want = runOutputFlavorStatus
			case runOutputEvtOpenScript:
				want = runOutputFlavorScript
			case runOutputEvtOpenEvents:
				want = runOutputFlavorEvents
			}
			if to != want {
				return fmt.Errorf("open event %s must land on flavor %s, got %s", event, want, to)
			}
			return nil
		},
	})
}

// beginRunOutputFlavor is the single choke point every open path runs: it
// steps the flavor machine to the new flavor and performs the complete
// cross-flavor reset — every flavor-scoped field is cleared here, then the
// caller sets only its own flavor's data and dialog. A flavor-open path can
// therefore never inherit another flavor's session pin, release count,
// untrust arm, or dialog. The token is deliberately NOT bumped here: open
// paths bump it at request time or present time per their existing fencing
// contracts.
func (o *overlayState) beginRunOutputFlavor(f runOutputFlavor) {
	next, ok := runOutputFlavorMachine.Next(o.runOutputFlavor, runOutputFlavorEventFor(f))
	if !ok {
		// Unreachable: every open event is total in the checked table. Keep
		// the slot untouched rather than half-reset on a rejected step.
		logging.Warn("run output flavor: rejected open %s from %s", f, o.runOutputFlavor)
		return
	}
	o.runOutputFlavor = next
	o.runOutput = nil
	o.runOutputWorkspace = nil
	o.runOutputSession = ""
	o.runOutputReleaseCount = 0
	o.runOutputUntrustable = false
}

// closeRunOutputFlavor steps the flavor machine back to closed and clears
// every flavor-scoped field. The caller (closeRunOutputDialog) owns the
// token bump that kills in-flight refresh ticks.
func (o *overlayState) closeRunOutputFlavor() {
	if next, ok := runOutputFlavorMachine.Next(o.runOutputFlavor, runOutputEvtClose); ok {
		o.runOutputFlavor = next
	}
	o.runOutput = nil
	o.runOutputWorkspace = nil
	o.runOutputSession = ""
	o.runOutputReleaseCount = 0
	o.runOutputUntrustable = false
}
