// bulkops phase machine: the driver's control state, shared by runtime and
// the exhaustive model check in driver_phase_test.go.
package bulkops

import (
	"errors"
	"fmt"

	"github.com/andyrewlee/amux/internal/fsm"
)

// Phase is the bulk driver's control state. The queue/counters/headID are
// extended state; this type is the qualitative position.
type Phase uint8

const (
	// PhaseIdle: no batch; Start is the only legal event.
	PhaseIdle Phase = iota
	// PhaseDraining: the batch owns the driver; targets may be in flight
	// (headID set) or momentarily between ops (advancing).
	PhaseDraining
)

func (p Phase) String() string {
	if p == PhaseDraining {
		return "draining"
	}
	return "idle"
}

// phaseEvent is the driver phase machine's alphabet.
type phaseEvent uint8

const (
	// evtBatchStart: a new batch begins (rejected while draining).
	evtBatchStart phaseEvent = iota
	// evtOpAccepted: the head target's per-row op launched.
	evtOpAccepted
	// evtTargetSkipped: a guard rejected the target; the drain advances.
	evtTargetSkipped
	// evtHeadCompleted: the in-flight op's completion matched the head.
	evtHeadCompleted
	// evtDrainFinished: the queue emptied; summary emitted, marks cleared.
	evtDrainFinished
	// evtBatchAbandoned: the panic-recovery path drops the batch mid-drain.
	evtBatchAbandoned
)

func (e phaseEvent) String() string {
	switch e {
	case evtBatchStart:
		return "batch-start"
	case evtOpAccepted:
		return "op-accepted"
	case evtTargetSkipped:
		return "target-skipped"
	case evtHeadCompleted:
		return "head-completed"
	case evtDrainFinished:
		return "drain-finished"
	default:
		return "batch-abandoned"
	}
}

// phaseMachine is the driver's control protocol: only Start moves idle →
// draining; every draining event is either advancement (staying draining)
// or termination (finished/abandoned back to idle). Start while draining is
// REJECTED — a second batch while one drains must never overwrite the
// in-flight op's identity.
var phaseMachine = fsm.MustCompile(fsm.Machine[Phase, phaseEvent]{
	Name:    "bulk-driver",
	Initial: PhaseIdle,
	Steps: []fsm.Step[Phase, phaseEvent]{
		{From: PhaseIdle, Event: evtBatchStart, To: PhaseDraining},
		{From: PhaseDraining, Event: evtOpAccepted, To: PhaseDraining},
		{From: PhaseDraining, Event: evtTargetSkipped, To: PhaseDraining},
		{From: PhaseDraining, Event: evtHeadCompleted, To: PhaseDraining},
		{From: PhaseDraining, Event: evtDrainFinished, To: PhaseIdle},
		{From: PhaseDraining, Event: evtBatchAbandoned, To: PhaseIdle},
	},
})

// PhaseCheck formally verifies the driver protocol: draining is reachable,
// Start is the only way in, only finishing or abandoning end a drain, and
// the idle state handles no in-flight events.
func PhaseCheck() error {
	return phaseMachine.Check(fsm.Checker[Phase, phaseEvent]{
		Alphabet: []phaseEvent{
			evtBatchStart, evtOpAccepted, evtTargetSkipped,
			evtHeadCompleted, evtDrainFinished, evtBatchAbandoned,
		},
		AllDeclaredReachable: true,
		Edge: func(from Phase, event phaseEvent, to Phase) error {
			if from == PhaseIdle && to == PhaseDraining && event != evtBatchStart {
				return errors.New("a batch may begin only via Start")
			}
			if from == PhaseDraining && to == PhaseIdle && event != evtDrainFinished && event != evtBatchAbandoned {
				return fmt.Errorf("a drain ends only by finishing or abandonment (got %v)", event)
			}
			return nil
		},
	})
}
