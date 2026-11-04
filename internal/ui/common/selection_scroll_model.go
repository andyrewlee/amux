package common

import (
	"errors"
	"fmt"

	"github.com/andyrewlee/amux/internal/fsm"
)

// selScrollState is the control state of the drag-selection auto-scroll
// machine, derived from SelectionScrollState's exported fields (see
// ctlState below — the fields are the extended state; the derivation is the
// single mapping between them and this machine).
//
// The six states refine the three the prose names (inactive / scrolling up /
// scrolling down) exactly as the code behaves:
//
//   - stPendingUp / stPendingDown: a direction is set (pointer past an edge)
//     but no tick loop is armed yet — SetDirection ran, NeedsTick has not.
//   - stArmedUp / stArmedDown: a tick loop is running in that direction.
//   - stArmedNone: a loop is armed but the direction has been cleared
//     (pointer back inside the viewport). Self-healing: the next tick of any
//     generation deactivates the loop, and NeedsTick refuses to re-request.
//   - stIdle: no direction, no loop.
type selScrollState int

const (
	stIdle selScrollState = iota
	stPendingUp
	stPendingDown
	stArmedUp
	stArmedDown
	stArmedNone
)

func (s selScrollState) String() string {
	switch s {
	case stPendingUp:
		return "pending-up"
	case stPendingDown:
		return "pending-down"
	case stArmedUp:
		return "armed-up"
	case stArmedDown:
		return "armed-down"
	case stArmedNone:
		return "armed-none"
	default:
		return "idle"
	}
}

// selScrollDir reports the scroll direction a state carries (+1, -1, 0).
// Used by the checker's edge invariants, which are direction-aware.
func selScrollDir(s selScrollState) int {
	switch s {
	case stPendingUp, stArmedUp:
		return 1
	case stPendingDown, stArmedDown:
		return -1
	default:
		return 0
	}
}

// selScrollArmed reports whether a state has an armed tick loop.
func selScrollArmed(s selScrollState) bool {
	return s == stArmedUp || s == stArmedDown || s == stArmedNone
}

// selScrollEvent is the auto-scroll machine's alphabet. Each event maps to
// one observable call on SelectionScrollState:
//
//	armUp / armDown / dirZero  SetDirection's three outcomes (pointer above
//	                           the top edge, below the bottom edge, inside).
//	needsTick                  a NeedsTick call (drag motion re-requesting
//	                           the tick loop).
//	tickCurrent                HandleTick with the current generation and
//	                           the next expected sequence number.
//	tickStale                  HandleTick with a superseded generation
//	                           (after Reset or a re-arm bumped the epoch).
//	reset                      Reset (mouse release / selection clear).
type selScrollEvent int

const (
	evtArmUp selScrollEvent = iota
	evtArmDown
	evtDirZero
	evtNeedsTick
	evtTickCurrent
	evtTickStale
	evtReset
)

func (e selScrollEvent) String() string {
	switch e {
	case evtArmUp:
		return "arm-up"
	case evtArmDown:
		return "arm-down"
	case evtDirZero:
		return "dir-zero"
	case evtNeedsTick:
		return "needs-tick"
	case evtTickCurrent:
		return "tick-current"
	case evtTickStale:
		return "tick-stale"
	default:
		return "reset"
	}
}

// selScrollAlphabet enumerates every event the environment may deliver.
var selScrollAlphabet = []selScrollEvent{
	evtArmUp, evtArmDown, evtDirZero, evtNeedsTick,
	evtTickCurrent, evtTickStale, evtReset,
}

// selScrollMachine is the drag-selection auto-scroll protocol behind
// SelectionScrollState, as documented in internal/ui/center/SCROLLING.md
// ("Drag-selection auto-scroll"). Invariants it exists to uphold:
//
//   - Self-healing re-request: NeedsTick from an armed, directed state
//     re-requests the SAME generation/sequence instead of invalidating the
//     live timer. Do not "optimize" it to start-only-when-not-active; a
//     dropped tick message must pause auto-scroll only until the next mouse
//     motion, and the re-request edge (armed -> armed on needs-tick) is what
//     makes that true.
//   - Epoch fencing: Gen/TickSeq are extended state at the call site, not
//     table columns, but the table pins the only two places the epoch can
//     restart: arming from a pending state (NeedsTick bumps Gen, resets
//     TickSeq to 1) and Reset (bumps Gen). Every edge into an armed state
//     from a non-armed state is a needs-tick edge, so a stale tick from any
//     superseded epoch always arrives in a state where the table rejects it
//     or the Gen mismatch drops it.
//   - Direction switches while armed (arm-up from armed-down and vice
//     versa) deliberately do NOT restart the epoch: SetDirection only
//     rewrites ScrollDir, and SelectionScrollTickStep reads ScrollDir live,
//     so in-flight ticks of the current epoch simply scroll the new
//     direction. The epoch fences drags (across Reset/deactivate), not
//     per-direction tick generations. (This refines the older prose claim
//     that "direction changes restart the epoch" — the code and this table
//     agree that they do not, and do not need to.)
//   - A tick arriving with the direction cleared (stArmedNone) ends the
//     loop regardless of generation — HandleTick checks Active and
//     ScrollDir==0 before the Gen/Seq comparison.
//
// The anchor/head side of drag selection (selection.go) stays extended
// state, outside this machine, by design: DragSelect clamps the pointer and
// pins termY to the viewport edge before mapping it to an absolute line;
// ExtendSelection keeps the vterm anchor and SelectionState's start in sync
// while moving only the far endpoint; SelectionScrollTickStep re-uses
// lastTermX to extend the endpoint to the newly exposed edge row each tick.
// Those are coordinates and geometry (data the machine's ticks consume),
// not control flow — the machine governs only whether a tick scrolls and in
// which direction. DragSelect's scroll(±1) call on edge crossing is
// likewise an immediate effect, not a transition.
var selScrollMachine = fsm.MustCompile(fsm.Machine[selScrollState, selScrollEvent]{
	Name:    "selection-scroll",
	Initial: stIdle,
	Steps: []fsm.Step[selScrollState, selScrollEvent]{
		// SetDirection sets the direction; it never touches Active or the
		// epoch. From any state, the three outcomes are total.
		{From: stIdle, Event: evtArmUp, To: stPendingUp},
		{From: stIdle, Event: evtArmDown, To: stPendingDown},
		{From: stIdle, Event: evtDirZero, To: stIdle},
		{From: stPendingUp, Event: evtArmUp, To: stPendingUp},
		{From: stPendingUp, Event: evtArmDown, To: stPendingDown},
		{From: stPendingUp, Event: evtDirZero, To: stIdle},
		{From: stPendingDown, Event: evtArmUp, To: stPendingUp},
		{From: stPendingDown, Event: evtArmDown, To: stPendingDown},
		{From: stPendingDown, Event: evtDirZero, To: stIdle},
		{From: stArmedUp, Event: evtArmUp, To: stArmedUp},
		// Direction switch while armed: same loop, new direction, same epoch.
		{From: stArmedUp, Event: evtArmDown, To: stArmedDown},
		{From: stArmedUp, Event: evtDirZero, To: stArmedNone},
		{From: stArmedDown, Event: evtArmUp, To: stArmedUp},
		{From: stArmedDown, Event: evtArmDown, To: stArmedDown},
		{From: stArmedDown, Event: evtDirZero, To: stArmedNone},
		{From: stArmedNone, Event: evtArmUp, To: stArmedUp},
		{From: stArmedNone, Event: evtArmDown, To: stArmedDown},
		{From: stArmedNone, Event: evtDirZero, To: stArmedNone},

		// NeedsTick arms from pending (epoch restart: Gen++, TickSeq=1) and
		// re-requests from armed (same Gen/TickSeq — the self-heal). From a
		// directionless state it returns false: no edge, no change.
		{From: stPendingUp, Event: evtNeedsTick, To: stArmedUp},
		{From: stPendingDown, Event: evtNeedsTick, To: stArmedDown},
		{From: stArmedUp, Event: evtNeedsTick, To: stArmedUp},
		{From: stArmedDown, Event: evtNeedsTick, To: stArmedDown},

		// Ticks are legal only while armed. Current-epoch ticks continue the
		// loop (TickSeq++ at the call site); stale generations are dropped
		// without stopping it. From stArmedNone any tick — current or stale
		// — ends the loop (HandleTick checks direction before the epoch).
		{From: stArmedUp, Event: evtTickCurrent, To: stArmedUp},
		{From: stArmedUp, Event: evtTickStale, To: stArmedUp},
		{From: stArmedDown, Event: evtTickCurrent, To: stArmedDown},
		{From: stArmedDown, Event: evtTickStale, To: stArmedDown},
		{From: stArmedNone, Event: evtTickCurrent, To: stIdle},
		{From: stArmedNone, Event: evtTickStale, To: stIdle},

		// Reset lands idle from everywhere and bumps Gen at the call site,
		// invalidating any in-flight tick of the superseded epoch.
		{From: stIdle, Event: evtReset, To: stIdle},
		{From: stPendingUp, Event: evtReset, To: stIdle},
		{From: stPendingDown, Event: evtReset, To: stIdle},
		{From: stArmedUp, Event: evtReset, To: stIdle},
		{From: stArmedDown, Event: evtReset, To: stIdle},
		{From: stArmedNone, Event: evtReset, To: stIdle},
	},
})

// ctlState derives the machine's control state from the shipped struct's
// fields. (Active, ScrollDir) is a bijection with the six states; Gen and
// TickSeq are the epoch fencing extended state and do not participate.
//
// The Active && TickSeq==0 guard inside NeedsTick is defensive: no sequence
// of public-method calls can produce Active with TickSeq==0 (arming sets
// TickSeq=1, ticks only increment it, Reset clears Active), so the
// derivation never has to name a seventh state for it.
func (s *SelectionScrollState) ctlState() selScrollState {
	switch {
	case !s.Active:
		switch {
		case s.ScrollDir > 0:
			return stPendingUp
		case s.ScrollDir < 0:
			return stPendingDown
		default:
			return stIdle
		}
	default:
		switch {
		case s.ScrollDir > 0:
			return stArmedUp
		case s.ScrollDir < 0:
			return stArmedDown
		default:
			return stArmedNone
		}
	}
}

// SelectionScrollCheck formally verifies the drag-selection auto-scroll
// machine: NeedsTick never fires from a directionless state (the SCROLLING.md
// anti-optimization is structurally impossible to regress past the checker),
// ticks are legal only while armed, entering an armed state from a non-armed
// state happens only through NeedsTick's epoch-restarting arm, and Reset
// lands idle from everywhere.
func SelectionScrollCheck() error {
	err := selScrollMachine.Check(fsm.Checker[selScrollState, selScrollEvent]{
		Alphabet:             selScrollAlphabet,
		AllDeclaredReachable: true,
		Edge: func(from selScrollState, event selScrollEvent, to selScrollState) error {
			if event == evtNeedsTick && selScrollDir(from) == 0 {
				return errors.New("needs-tick must not fire from a directionless state")
			}
			if (event == evtTickCurrent || event == evtTickStale) && !selScrollArmed(from) {
				return fmt.Errorf("tick %v is legal only while armed (from %v)", event, from)
			}
			if event == evtReset && to != stIdle {
				return fmt.Errorf("reset must land idle, got %v", to)
			}
			// The only door into an armed state from a non-armed one is the
			// arm edge — the one place (with Reset) that bumps Gen. A stale
			// tick from a superseded epoch therefore always arrives where
			// the table rejects it or the Gen mismatch drops it.
			if selScrollArmed(to) && !selScrollArmed(from) && event != evtNeedsTick {
				return fmt.Errorf("entering an armed state from %v requires needs-tick's epoch-restarting arm, got %v", from, event)
			}
			return nil
		},
	})
	if err != nil {
		return err
	}
	// No wedge: from every reachable state the machine can still reach idle
	// (Reset is total), so no drag can strand an armed loop forever.
	for _, st := range selScrollMachine.Reachable(selScrollAlphabet) {
		reachable := selScrollMachine.ReachableFrom(st, selScrollAlphabet)
		found := false
		for _, r := range reachable {
			if r == stIdle {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("selection-scroll: state %v cannot reach idle (wedge)", st)
		}
	}
	return nil
}
