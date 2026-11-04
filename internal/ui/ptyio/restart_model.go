package ptyio

import (
	"errors"
	"fmt"

	"github.com/andyrewlee/amux/internal/fsm"
)

// restartState is the reader-restart policy's control state. The numeric
// implementation (State.RestartBackoff/RestartCount/RestartSince plus
// NextRestartBackoffLocked) is this machine's extended state; the table
// below is the verified spec the arithmetic must implement.
type restartState int

const (
	// restartSteady: the reader is healthy; no budget consumed.
	restartSteady restartState = iota
	// restartBackingOff: the reader stopped while the terminal lives;
	// restarts are scheduled with exponential delay while budget remains.
	restartBackingOff
	// restartExhausted: the per-window budget is spent — detach instead of
	// restarting. Aged-out windows and reattach reset back to steady.
	restartExhausted
)

func (s restartState) String() string {
	switch s {
	case restartBackingOff:
		return "backing-off"
	case restartExhausted:
		return "exhausted"
	default:
		return "steady"
	}
}

// restartEvent is the restart policy's alphabet.
type restartEvent int

const (
	// evtReaderStoppedAlive: the reader stopped but the terminal is open.
	evtReaderStoppedAlive restartEvent = iota
	// evtReaderStoppedDead: the reader stopped because the terminal is gone.
	evtReaderStoppedDead
	// evtBudgetExhausted: another stop arrived with no restarts left in the
	// rolling window.
	evtBudgetExhausted
	// evtWindowAgedOut: the rolling window passed since the last restart —
	// the budget resets.
	evtWindowAgedOut
	// evtRestartReset: detach/reattach clears restart state.
	evtRestartReset
)

func (e restartEvent) String() string {
	switch e {
	case evtReaderStoppedAlive:
		return "reader-stopped-alive"
	case evtReaderStoppedDead:
		return "reader-stopped-dead"
	case evtBudgetExhausted:
		return "budget-exhausted"
	case evtWindowAgedOut:
		return "window-aged-out"
	default:
		return "reset"
	}
}

// restartMachine is the reader-restart policy: only a living terminal
// consumes budget; a dead terminal resets (restarts do not apply); an
// aged-out window always returns to steady; exhaustion detaches.
var restartMachine = fsm.MustCompile(fsm.Machine[restartState, restartEvent]{
	Name:    "pty-reader-restart",
	Initial: restartSteady,
	Steps: []fsm.Step[restartState, restartEvent]{
		{From: restartSteady, Event: evtReaderStoppedAlive, To: restartBackingOff},
		{From: restartSteady, Event: evtReaderStoppedDead, To: restartSteady},
		{From: restartSteady, Event: evtWindowAgedOut, To: restartSteady},
		{From: restartSteady, Event: evtRestartReset, To: restartSteady},
		{From: restartBackingOff, Event: evtReaderStoppedAlive, To: restartBackingOff},
		{From: restartBackingOff, Event: evtBudgetExhausted, To: restartExhausted},
		// An aged-out window refreshes the budget but keeps the terminal in
		// restart mode: the delay keeps doubling across windows (only a
		// fresh reader or a detach clears it).
		{From: restartBackingOff, Event: evtWindowAgedOut, To: restartBackingOff},
		{From: restartBackingOff, Event: evtReaderStoppedDead, To: restartSteady},
		{From: restartBackingOff, Event: evtRestartReset, To: restartSteady},
		{From: restartExhausted, Event: evtWindowAgedOut, To: restartBackingOff},
		{From: restartExhausted, Event: evtRestartReset, To: restartSteady},
		{From: restartExhausted, Event: evtReaderStoppedDead, To: restartSteady},
	},
})

// RestartPolicyCheck formally verifies the restart policy: exhaustion is
// reachable only through a spent budget, an aged window always refreshes the
// budget (never detaches), and a dead terminal never consumes budget.
func RestartPolicyCheck() error {
	return restartMachine.Check(fsm.Checker[restartState, restartEvent]{
		Alphabet: []restartEvent{
			evtReaderStoppedAlive, evtReaderStoppedDead, evtBudgetExhausted,
			evtWindowAgedOut, evtRestartReset,
		},
		AllDeclaredReachable: true,
		Edge: func(from restartState, event restartEvent, to restartState) error {
			if to == restartExhausted && event != evtBudgetExhausted {
				return fmt.Errorf("exhaustion reachable via %v: only a spent budget may detach", event)
			}
			if event == evtWindowAgedOut && to == restartExhausted {
				return errors.New("an aged-out window must refresh the budget, never detach")
			}
			if event == evtReaderStoppedDead && to != restartSteady {
				return errors.New("a dead terminal must reset, not consume budget")
			}
			return nil
		},
	})
}
