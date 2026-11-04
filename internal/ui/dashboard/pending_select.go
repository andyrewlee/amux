package dashboard

import (
	"fmt"

	"github.com/andyrewlee/amux/internal/fsm"
)

// pendingSelectMaxLoads bounds how many project rebuilds a pending selection
// may wait for. Two covers the reload the creation itself queued plus one
// concurrent reload landing first.
const pendingSelectMaxLoads = 2

// pendingSelectState is the pending-selection debounce machine. The ID it
// waits on and the loads countdown stay at the call site as extended state;
// the machine owns only whether a selection is armed.
type pendingSelectState int

const (
	pendingSelectIdle pendingSelectState = iota
	pendingSelectArmed
)

type pendingSelectEvent int

const (
	pendingSelectArm    pendingSelectEvent = iota // SelectWorkspace / resolveCursorAfterRebuild
	pendingSelectApply                            // row found, cursor landed
	pendingSelectExpire                           // loads countdown reached zero
	pendingSelectClear                            // user input outranked the wait
)

// pendingSelectMachine is the checked debounce table. Undefined (Idle,
// apply/expire/clear) pairs are rejections the call sites never step —
// every exit from Armed lands Idle, so no path leaves a selection latched
// except an explicit re-arm.
var pendingSelectMachine = fsm.MustCompile(fsm.Machine[pendingSelectState, pendingSelectEvent]{
	Name:    "dashboard-pending-select",
	Initial: pendingSelectIdle,
	Steps: []fsm.Step[pendingSelectState, pendingSelectEvent]{
		{From: pendingSelectIdle, Event: pendingSelectArm, To: pendingSelectArmed},
		{From: pendingSelectArmed, Event: pendingSelectArm, To: pendingSelectArmed},
		{From: pendingSelectArmed, Event: pendingSelectApply, To: pendingSelectIdle},
		{From: pendingSelectArmed, Event: pendingSelectExpire, To: pendingSelectIdle},
		{From: pendingSelectArmed, Event: pendingSelectClear, To: pendingSelectIdle},
	},
})

// nextPendingSelect applies an event through the checked table; a rejected
// event (an exit attempted from Idle) is a no-op.
func nextPendingSelect(prev pendingSelectState, ev pendingSelectEvent) pendingSelectState {
	if next, ok := pendingSelectMachine.Next(prev, ev); ok {
		return next
	}
	return prev
}

// pendingSelectMachineCheck formally verifies the debounce table: both
// states reachable, every Armed exit lands Idle, and arming is the only way
// in — the machine cannot strand a selection.
func pendingSelectMachineCheck() error {
	return pendingSelectMachine.Check(fsm.Checker[pendingSelectState, pendingSelectEvent]{
		Alphabet: []pendingSelectEvent{
			pendingSelectArm, pendingSelectApply, pendingSelectExpire, pendingSelectClear,
		},
		AllDeclaredReachable: true,
		Edge: func(from pendingSelectState, ev pendingSelectEvent, to pendingSelectState) error {
			if from == pendingSelectArmed && ev != pendingSelectArm && to != pendingSelectIdle {
				return fmt.Errorf("event %v must retire an armed selection, got %v", ev, to)
			}
			if to == pendingSelectArmed && ev != pendingSelectArm {
				return fmt.Errorf("event %v cannot arm a selection", ev)
			}
			return nil
		},
	})
}
