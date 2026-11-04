package app

import (
	"errors"
	"fmt"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/fsm"
	"github.com/andyrewlee/amux/internal/logging"
)

// Staged dialog handoffs are two-slot control state: the create→agent-picker
// handoff (pendingWorkspaceCreate) and the picker→task-dialog handoff
// (pendingLaunchTask). Each crosses two dialog results by design, which is
// exactly why they are dangerous as bare data: a stale binding the next
// unrelated dialog would consume — e.g. an agent pick silently creating a
// workspace. dialogHandoffState is the checked control state; the project
// pointer, name, base ref, assistant, and workspace the slots carry are
// extended state written only through the stage/consume/cancel helpers.
type dialogHandoffState uint8

const (
	// handoffNone: neither handoff is armed.
	handoffNone dialogHandoffState = iota
	// handoffCreateStaged: a confirmed create dialog armed the picker
	// handoff (pendingWorkspaceCreate), awaiting the agent-picker result.
	handoffCreateStaged
	// handoffLaunchStaged: a ctrl+t picker pick armed the task-dialog
	// handoff (pendingLaunchTask), awaiting the launch-task result.
	handoffLaunchStaged
)

func (s dialogHandoffState) String() string {
	switch s {
	case handoffCreateStaged:
		return "create-staged"
	case handoffLaunchStaged:
		return "launch-staged"
	default:
		return "none"
	}
}

// dialogHandoffEvent is the handoff machine's alphabet. "Consumed" collapses
// back to none inside the same Update turn — the consuming result handler
// clears the slot and emits the follow-up message (CreateWorkspace /
// LaunchAgent) atomically, so there is no lingering consumed state to model.
type dialogHandoffEvent uint8

const (
	handoffEvtStageCreate dialogHandoffEvent = iota
	handoffEvtConsumeCreate
	handoffEvtCancelCreate
	handoffEvtStageLaunch
	handoffEvtConsumeLaunch
	handoffEvtCancelLaunch
	handoffEvtClearAll
)

func (e dialogHandoffEvent) String() string {
	switch e {
	case handoffEvtStageCreate:
		return "stage-create"
	case handoffEvtConsumeCreate:
		return "consume-create"
	case handoffEvtCancelCreate:
		return "cancel-create"
	case handoffEvtStageLaunch:
		return "stage-launch"
	case handoffEvtConsumeLaunch:
		return "consume-launch"
	case handoffEvtCancelLaunch:
		return "cancel-launch"
	default:
		return "clear-all"
	}
}

// dialogHandoffMachine is the staged-handoff table. Staging is legal only
// from none: re-staging is a re-entry bug (a second create confirm or ctrl+t
// pick while a handoff is armed), and cross-staging (arming one handoff
// while the other is armed) is the stale-binding hazard the guards exist to
// prevent — both are rejected by omission, fail-closed. Cancels from none
// are declared no-op self-loops because the clear paths run unconditionally
// (every dialog present clears the create handoff; panic recovery clears
// both). Consumes are strict: a consume arriving with nothing staged is a
// control bug worth a warning, not a silent pass.
var dialogHandoffMachine = fsm.MustCompile(fsm.Machine[dialogHandoffState, dialogHandoffEvent]{
	Name:    "dialog-handoff",
	Initial: handoffNone,
	Steps: []fsm.Step[dialogHandoffState, dialogHandoffEvent]{
		{From: handoffNone, Event: handoffEvtStageCreate, To: handoffCreateStaged},
		{From: handoffNone, Event: handoffEvtStageLaunch, To: handoffLaunchStaged},
		{From: handoffCreateStaged, Event: handoffEvtConsumeCreate, To: handoffNone},
		{From: handoffCreateStaged, Event: handoffEvtCancelCreate, To: handoffNone},
		{From: handoffCreateStaged, Event: handoffEvtClearAll, To: handoffNone},
		{From: handoffLaunchStaged, Event: handoffEvtConsumeLaunch, To: handoffNone},
		{From: handoffLaunchStaged, Event: handoffEvtCancelLaunch, To: handoffNone},
		{From: handoffLaunchStaged, Event: handoffEvtClearAll, To: handoffNone},
		{From: handoffNone, Event: handoffEvtCancelCreate, To: handoffNone},
		{From: handoffNone, Event: handoffEvtCancelLaunch, To: handoffNone},
		{From: handoffNone, Event: handoffEvtClearAll, To: handoffNone},
	},
})

// dialogHandoffCheck formally verifies the handoff table: every handoff
// state is reachable, staging lands on exactly its own slot, and every
// consume/cancel/clear edge lands on none — a handoff never half-arms or
// morphs into the other handoff.
func dialogHandoffCheck() error {
	return dialogHandoffMachine.Check(fsm.Checker[dialogHandoffState, dialogHandoffEvent]{
		Alphabet: []dialogHandoffEvent{
			handoffEvtStageCreate, handoffEvtConsumeCreate, handoffEvtCancelCreate,
			handoffEvtStageLaunch, handoffEvtConsumeLaunch, handoffEvtCancelLaunch,
			handoffEvtClearAll,
		},
		AllDeclaredReachable: true,
		Edge: func(_ dialogHandoffState, event dialogHandoffEvent, to dialogHandoffState) error {
			switch event {
			case handoffEvtStageCreate:
				if to != handoffCreateStaged {
					return fmt.Errorf("staging a create must arm the create slot, got %s", to)
				}
			case handoffEvtStageLaunch:
				if to != handoffLaunchStaged {
					return fmt.Errorf("staging a launch must arm the launch slot, got %s", to)
				}
			default:
				if to != handoffNone {
					return fmt.Errorf("event %s must return to none, got %s", event, to)
				}
			}
			return nil
		},
	})
}

// stepDialogHandoff applies one handoff event. A rejection is a control-flow
// bug (staging over an armed handoff, consuming an unarmed one); the state
// is left untouched and the caller is told so it can skip the data write —
// writing the slot anyway would be the stale binding this machine exists to
// make impossible.
func (a *App) stepDialogHandoff(e dialogHandoffEvent) bool {
	next, ok := dialogHandoffMachine.Next(a.dialogHandoff, e)
	if !ok {
		logging.Warn("dialog handoff: rejected event %s in state %s", e, a.dialogHandoff)
		return false
	}
	a.dialogHandoff = next
	return true
}

// stagePendingWorkspaceCreate arms the create→picker handoff. The data write
// happens only when the table accepts the staging.
func (a *App) stagePendingWorkspaceCreate(project *data.Project, name, base string) {
	if !a.stepDialogHandoff(handoffEvtStageCreate) {
		return
	}
	a.pendingWorkspaceCreate = pendingWorkspaceCreateState{project: project, name: name, base: base}
}

// consumePendingWorkspaceCreate drains the staged create handoff (the picker
// result accepted it). The data slot is cleared unconditionally to preserve
// the historical defensive behavior; the machine step warns if nothing was
// staged.
func (a *App) consumePendingWorkspaceCreate() pendingWorkspaceCreateState {
	pending := a.pendingWorkspaceCreate
	a.pendingWorkspaceCreate = pendingWorkspaceCreateState{}
	a.stepDialogHandoff(handoffEvtConsumeCreate)
	return pending
}

// stagePendingLaunch arms the picker→task-dialog handoff. The data write
// happens only when the table accepts the staging.
func (a *App) stagePendingLaunch(assistant string, workspace *data.Workspace) {
	if !a.stepDialogHandoff(handoffEvtStageLaunch) {
		return
	}
	a.pendingLaunchTask = pendingLaunchTaskState{assistant: assistant, workspace: workspace}
}

// consumePendingLaunch drains the staged launch handoff (the task dialog
// resolved). The data slot is cleared unconditionally; the machine step warns
// if nothing was staged.
func (a *App) consumePendingLaunch() pendingLaunchTaskState {
	pending := a.pendingLaunchTask
	a.pendingLaunchTask = pendingLaunchTaskState{}
	a.stepDialogHandoff(handoffEvtConsumeLaunch)
	return pending
}

// cancelPendingLaunch drops the staged launch handoff and reports whether
// one was armed (the task-dialog cancel path reopens the picker only when a
// pick was actually dropped).
func (a *App) cancelPendingLaunch() (armed bool) {
	armed = a.pendingLaunchTask.assistant != ""
	a.pendingLaunchTask = pendingLaunchTaskState{}
	a.stepDialogHandoff(handoffEvtCancelLaunch)
	return armed
}

// clearPendingHandoffs drops every staged dialog handoff — the create→
// agent-picker binding and the picker→task binding. Called on paths that
// abandon all in-flight flows at once (panic recovery); per-field clears
// that run conditionally (the displaced-result path binds each to its own
// dialog ID) stay manual at their sites.
func (a *App) clearPendingHandoffs() {
	a.pendingWorkspaceCreate = pendingWorkspaceCreateState{}
	a.pendingLaunchTask = pendingLaunchTaskState{}
	a.stepDialogHandoff(handoffEvtClearAll)
}

// dialogSlotState is the registry-dialog slot's lifecycle. dialogSeq and
// dialogOpenSeq (the instance stamp a bound result must match) are extended
// state; this is the control shape around them.
type dialogSlotState uint8

const (
	// dialogSlotClosed: no app dialog visible (dialogOpenSeq == 0).
	dialogSlotClosed dialogSlotState = iota
	// dialogSlotOpen: an app dialog is visible under one instance's seq.
	dialogSlotOpen
)

func (s dialogSlotState) String() string {
	if s == dialogSlotOpen {
		return "open"
	}
	return "closed"
}

// dialogSlotEvent is the dialog-slot machine's alphabet.
type dialogSlotEvent uint8

const (
	// slotEvtPresent: a dialog is shown while the slot is closed.
	slotEvtPresent dialogSlotEvent = iota
	// slotEvtReplace: a dialog is shown over an open one (seq bumped — the
	// displaced instance's results become stale).
	slotEvtReplace
	// slotEvtResultBound: a result whose seq matches the open instance —
	// the only event that may close the slot.
	slotEvtResultBound
	// slotEvtResultStale: a result whose seq does not match — dropped; the
	// live dialog must survive it.
	slotEvtResultStale
	// slotEvtResultUnbound: a result produced outside the dialog slot (the
	// FilePicker's DialogAddProject); the slot is untouched either way.
	slotEvtResultUnbound
)

func (e dialogSlotEvent) String() string {
	switch e {
	case slotEvtPresent:
		return "present"
	case slotEvtReplace:
		return "replace"
	case slotEvtResultBound:
		return "result-bound"
	case slotEvtResultStale:
		return "result-stale"
	default:
		return "result-unbound"
	}
}

// dialogSlotMachine is the dialog open/replace/stale-drop table: presenting
// opens or replaces; only a bound (seq-matching) result closes the slot; a
// stale result leaves the live dialog exactly as it was. The closed
// stale-drop self-loop is the late-arriving straggler: a result landing
// after its dialog already resolved is dropped, not applied.
var dialogSlotMachine = fsm.MustCompile(fsm.Machine[dialogSlotState, dialogSlotEvent]{
	Name:    "dialog-slot",
	Initial: dialogSlotClosed,
	Steps: []fsm.Step[dialogSlotState, dialogSlotEvent]{
		{From: dialogSlotClosed, Event: slotEvtPresent, To: dialogSlotOpen},
		{From: dialogSlotOpen, Event: slotEvtReplace, To: dialogSlotOpen},
		{From: dialogSlotOpen, Event: slotEvtResultBound, To: dialogSlotClosed},
		{From: dialogSlotOpen, Event: slotEvtResultStale, To: dialogSlotOpen},
		{From: dialogSlotOpen, Event: slotEvtResultUnbound, To: dialogSlotOpen},
		{From: dialogSlotClosed, Event: slotEvtResultUnbound, To: dialogSlotClosed},
		{From: dialogSlotClosed, Event: slotEvtResultStale, To: dialogSlotClosed},
	},
})

// dialogSlotCheck formally verifies the dialog-slot table: only a bound
// result closes the slot, a stale result never does (the live dialog must
// survive a displaced instance's late result), and a replace keeps the slot
// open under the newer instance's seq.
func dialogSlotCheck() error {
	return dialogSlotMachine.Check(fsm.Checker[dialogSlotState, dialogSlotEvent]{
		Alphabet: []dialogSlotEvent{
			slotEvtPresent, slotEvtReplace, slotEvtResultBound,
			slotEvtResultStale, slotEvtResultUnbound,
		},
		AllDeclaredReachable: true,
		Edge: func(from dialogSlotState, event dialogSlotEvent, to dialogSlotState) error {
			switch event {
			case slotEvtResultStale, slotEvtResultUnbound:
				if to != from {
					return errors.New("a stale or unbound result must leave the dialog slot untouched")
				}
			case slotEvtResultBound:
				if to != dialogSlotClosed {
					return errors.New("a bound result must close the dialog slot")
				}
			case slotEvtReplace:
				if to != dialogSlotOpen {
					return errors.New("a replace must keep the slot open under the newer seq")
				}
			case slotEvtPresent:
				if to != dialogSlotOpen {
					return errors.New("presenting must open the dialog slot")
				}
			}
			return nil
		},
	})
}

// stepDialogSlot applies one dialog-slot event, warning on a rejected move
// (a control bug such as a bound result arriving while the slot is closed).
func (a *App) stepDialogSlot(e dialogSlotEvent) {
	if next, ok := dialogSlotMachine.Next(a.dialogSlot, e); ok {
		a.dialogSlot = next
	} else {
		logging.Warn("dialog slot: rejected event %s in state %s", e, a.dialogSlot)
	}
}

// overlayArbiterState is the deferred-open arbiter's control state: whether
// opens are queued, and whether any modal overlay is currently visible. The
// queue itself (and the drainedOverlayCmds follow-up batch) is extended
// state; this is the legality shape around them.
type overlayArbiterState uint8

const (
	// arbFreeHidden: queue empty, nothing modal visible — opens apply now.
	arbFreeHidden overlayArbiterState = iota
	// arbFreeShown: queue empty, a modal is visible — a new open defers.
	arbFreeShown
	// arbQueuedShown: opens queued behind a visible modal.
	arbQueuedShown
	// arbQueuedHidden: opens queued but nothing visible — the transient
	// window between a modal closing and the once-per-Update drain.
	arbQueuedHidden
)

func (s overlayArbiterState) String() string {
	switch s {
	case arbFreeShown:
		return "free-shown"
	case arbQueuedShown:
		return "queued-shown"
	case arbQueuedHidden:
		return "queued-hidden"
	default:
		return "free-hidden"
	}
}

// overlayArbiterEvent is the arbiter machine's alphabet. Open events are
// split by whether the applied open presented a modal, because both outcomes
// are legal and the machine must stay deterministic.
type overlayArbiterEvent uint8

const (
	// arbEvtOpenPresented: an open applied immediately and showed a modal.
	arbEvtOpenPresented overlayArbiterEvent = iota
	// arbEvtOpenQuiet: an open applied immediately and showed nothing (a
	// no-op closure or an open that failed its own drain-time fence).
	arbEvtOpenQuiet
	// arbEvtDefer: an open queued behind a visible modal.
	arbEvtDefer
	// arbEvtOverflowDrop: the queue was at pendingOverlayOpenCap, so the
	// open was dropped — never applied.
	arbEvtOverflowDrop
	// arbEvtModalClosed: the visible modal went away (drain may proceed).
	arbEvtModalClosed
	// arbEvtDrainCompleted: a drain pass applied the queued opens and
	// emptied the queue.
	arbEvtDrainCompleted
	// arbEvtDrainBlocked: a drain pass applied at least one open but one of
	// them presented a modal, so the remainder stays queued.
	arbEvtDrainBlocked
	// arbEvtSlotRefresh: a runOutput-slot open arrived while its own viewer
	// was visible and applied immediately without queueing — the
	// self-deadlock avoidance in requestRunOutputOpen.
	arbEvtSlotRefresh
)

func (e overlayArbiterEvent) String() string {
	switch e {
	case arbEvtOpenPresented:
		return "open-presented"
	case arbEvtOpenQuiet:
		return "open-quiet"
	case arbEvtDefer:
		return "defer"
	case arbEvtOverflowDrop:
		return "overflow-drop"
	case arbEvtModalClosed:
		return "modal-closed"
	case arbEvtDrainCompleted:
		return "drain-completed"
	case arbEvtDrainBlocked:
		return "drain-blocked"
	default:
		return "slot-refresh"
	}
}

// overlayArbiterMachine is the overlay-arbitration table: an open applies
// immediately only when nothing modal is visible, defers behind a visible
// modal, is dropped at the cap, and drains once nothing is visible. A
// runOutput-slot refresh never queues (it would wait behind itself).
var overlayArbiterMachine = fsm.MustCompile(fsm.Machine[overlayArbiterState, overlayArbiterEvent]{
	Name:    "overlay-arbiter",
	Initial: arbFreeHidden,
	Steps: []fsm.Step[overlayArbiterState, overlayArbiterEvent]{
		{From: arbFreeHidden, Event: arbEvtOpenPresented, To: arbFreeShown},
		{From: arbFreeHidden, Event: arbEvtOpenQuiet, To: arbFreeHidden},
		{From: arbFreeShown, Event: arbEvtDefer, To: arbQueuedShown},
		{From: arbQueuedShown, Event: arbEvtDefer, To: arbQueuedShown},
		{From: arbQueuedShown, Event: arbEvtOverflowDrop, To: arbQueuedShown},
		{From: arbQueuedHidden, Event: arbEvtOpenPresented, To: arbQueuedShown},
		{From: arbQueuedHidden, Event: arbEvtOpenQuiet, To: arbQueuedHidden},
		{From: arbFreeShown, Event: arbEvtModalClosed, To: arbFreeHidden},
		{From: arbQueuedShown, Event: arbEvtModalClosed, To: arbQueuedHidden},
		{From: arbQueuedHidden, Event: arbEvtDrainCompleted, To: arbFreeHidden},
		{From: arbQueuedHidden, Event: arbEvtDrainBlocked, To: arbQueuedShown},
		{From: arbFreeHidden, Event: arbEvtSlotRefresh, To: arbFreeHidden},
		{From: arbFreeShown, Event: arbEvtSlotRefresh, To: arbFreeShown},
		{From: arbQueuedShown, Event: arbEvtSlotRefresh, To: arbQueuedShown},
		{From: arbQueuedHidden, Event: arbEvtSlotRefresh, To: arbQueuedHidden},
	},
})

// overlayArbiterCheck formally verifies the arbiter table: a deferred open
// always stays queued (never applied under a visible modal), a drop can only
// happen from a queued state (the cap cannot be hit on an empty queue),
// drains run only when nothing is visible, and a slot refresh never changes
// the arbiter state — the queued-behind-itself deadlock is unwritable. The
// immediate-open events being absent from both shown states is the table
// itself speaking: an undeclared (state, event) pair is a rejection, so an
// open can never apply while a modal is visible.
func overlayArbiterCheck() error {
	return overlayArbiterMachine.Check(fsm.Checker[overlayArbiterState, overlayArbiterEvent]{
		Alphabet: []overlayArbiterEvent{
			arbEvtOpenPresented, arbEvtOpenQuiet, arbEvtDefer, arbEvtOverflowDrop,
			arbEvtModalClosed, arbEvtDrainCompleted, arbEvtDrainBlocked, arbEvtSlotRefresh,
		},
		AllDeclaredReachable: true,
		Edge: func(from overlayArbiterState, event overlayArbiterEvent, to overlayArbiterState) error {
			switch event {
			case arbEvtDefer:
				if to != arbQueuedShown {
					return errors.New("a deferred open must stay queued behind the visible modal")
				}
			case arbEvtOverflowDrop:
				if from != arbQueuedShown || to != arbQueuedShown {
					return errors.New("a drop requires a full queue, which is always queued-shown")
				}
			case arbEvtDrainCompleted:
				if from != arbQueuedHidden || to != arbFreeHidden {
					return errors.New("a completed drain starts from a visible-free queue and empties it")
				}
			case arbEvtDrainBlocked:
				if from != arbQueuedHidden || to != arbQueuedShown {
					return errors.New("a blocked drain starts from a visible-free queue and leaves the remainder queued")
				}
			case arbEvtSlotRefresh:
				if from != to {
					return errors.New("a slot refresh must never change arbiter state (self-deadlock avoidance)")
				}
			}
			return nil
		},
	})
}
