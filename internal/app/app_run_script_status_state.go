package app

import (
	"errors"

	"github.com/andyrewlee/amux/internal/fsm"
)

// runTrackState models the crashed-run toast tracker over
// lastRunScriptRoot/lastRunScriptAlive (handleRunScriptStatusResult): a
// running→stopped flip with a non-zero exit surfaces exactly one toast, and
// cannot refire while the run stays dead. The root strings and exit codes
// are extended state; the root-switch reseed ("switching workspaces
// reseeds rather than misfiring") is the retrack event family.
type runTrackState uint8

const (
	// runTrackNone: no root tracked yet (lastRunScriptRoot == "").
	runTrackNone runTrackState = iota
	// runTrackAlive: the tracked root's run script is alive.
	runTrackAlive
	// runTrackDead: the tracked root's run script is stopped (a crash toast
	// may already have fired on the entering edge).
	runTrackDead
)

func (s runTrackState) String() string {
	switch s {
	case runTrackAlive:
		return "alive"
	case runTrackDead:
		return "dead"
	default:
		return "none"
	}
}

// runTrackEvent is the tracker machine's alphabet.
type runTrackEvent uint8

const (
	// runEvtRetrackAlive: a new/seeding observation reports alive.
	runEvtRetrackAlive runTrackEvent = iota
	// runEvtRetrackDead: a new/seeding observation reports stopped.
	runEvtRetrackDead
	// runEvtObserveAlive: the tracked root's run restarted.
	runEvtObserveAlive
	// runEvtObserveDeadExit: the tracked root's run stopped with a
	// non-zero exit — the crash-toast edge, fired only alive→dead.
	runEvtObserveDeadExit
	// runEvtObserveDeadSilent: the tracked root's run stopped with exit 0
	// or unknown — badge-off only, no toast.
	runEvtObserveDeadSilent
)

func (e runTrackEvent) String() string {
	switch e {
	case runEvtRetrackAlive:
		return "retrack-alive"
	case runEvtRetrackDead:
		return "retrack-dead"
	case runEvtObserveAlive:
		return "observe-alive"
	case runEvtObserveDeadExit:
		return "observe-dead-exit"
	default:
		return "observe-dead-silent"
	}
}

// runTrackMachine is the toast-edge table.
var runTrackMachine = fsm.MustCompile(fsm.Machine[runTrackState, runTrackEvent]{
	Name:    "run-script-toast",
	Initial: runTrackNone,
	Steps: []fsm.Step[runTrackState, runTrackEvent]{
		{From: runTrackNone, Event: runEvtRetrackAlive, To: runTrackAlive},
		{From: runTrackNone, Event: runEvtRetrackDead, To: runTrackDead},
		{From: runTrackAlive, Event: runEvtRetrackAlive, To: runTrackAlive},
		{From: runTrackAlive, Event: runEvtRetrackDead, To: runTrackDead},
		{From: runTrackDead, Event: runEvtRetrackAlive, To: runTrackAlive},
		{From: runTrackDead, Event: runEvtRetrackDead, To: runTrackDead},
		{From: runTrackAlive, Event: runEvtObserveAlive, To: runTrackAlive},
		{From: runTrackAlive, Event: runEvtObserveDeadExit, To: runTrackDead},
		{From: runTrackAlive, Event: runEvtObserveDeadSilent, To: runTrackDead},
		{From: runTrackDead, Event: runEvtObserveAlive, To: runTrackAlive},
		{From: runTrackDead, Event: runEvtObserveDeadExit, To: runTrackDead},
		{From: runTrackDead, Event: runEvtObserveDeadSilent, To: runTrackDead},
	},
})

// runTrackCheck formally verifies the toast edge: the crash toast fires on
// exactly the alive --observe-dead-exit--> dead edge (the Edge property
// below), a dead run is stable under further dead observations (the toast
// cannot refire), and every reseed lands on the observed liveness (a root
// switch never fabricates a flip).
func runTrackCheck() error {
	return runTrackMachine.Check(fsm.Checker[runTrackState, runTrackEvent]{
		Alphabet: []runTrackEvent{
			runEvtRetrackAlive, runEvtRetrackDead, runEvtObserveAlive,
			runEvtObserveDeadExit, runEvtObserveDeadSilent,
		},
		AllDeclaredReachable: true,
		Edge: func(from runTrackState, event runTrackEvent, to runTrackState) error {
			// The crash toast fires on exactly the alive --observe-dead-exit-->
			// dead edge; the dead → dead self-loop is the "no refire" guarantee
			// (a stable dead run never re-toasts).
			if event == runEvtObserveDeadExit && to != runTrackDead {
				return errors.New("a crash-exit observation must land on dead")
			}
			if from == runTrackAlive && event == runEvtObserveDeadExit && to != runTrackDead {
				return errors.New("the crash-toast edge is strictly alive→dead")
			}
			if (event == runEvtRetrackAlive || event == runEvtObserveAlive) && to != runTrackAlive {
				return errors.New("an alive observation must land on alive")
			}
			if (event == runEvtRetrackDead || event == runEvtObserveDeadExit || event == runEvtObserveDeadSilent) &&
				from != runTrackNone && to != runTrackDead {
				return errors.New("a dead observation must land on dead")
			}
			return nil
		},
	})
}

// runCheckState models the run-script status single-flight latch
// (runScriptStatusInFlight): the status check shells tmux, so a wedged
// server must degrade to stale indicators, not a goroutine per tick.
type runCheckState uint8

const (
	// runCheckIdle: no status check in flight.
	runCheckIdle runCheckState = iota
	// runCheckInFlight: a request was admitted and its result has not
	// landed.
	runCheckInFlight
)

func (s runCheckState) String() string {
	if s == runCheckInFlight {
		return "in-flight"
	}
	return "idle"
}

// runCheckEvent is the single-flight machine's alphabet.
type runCheckEvent uint8

const (
	// runEvtRequestAdmitted: a request started (latch set).
	runEvtRequestAdmitted runCheckEvent = iota
	// runEvtRequestSuppressed: a request arrived while one was in flight —
	// dropped (the running check's result covers the interval).
	runEvtRequestSuppressed
	// runEvtResultArrived: the typed result landed — the latch clears
	// BEFORE the staleness check, so even an irrelevant result re-arms the
	// next tick.
	runEvtResultArrived
	// runEvtPanicReset: resetAdvisoryLatchesAfterPanic cleared the latch
	// (the producer was discarded by the panic; a duplicate read is cheap,
	// a frozen indicator is not).
	runEvtPanicReset
)

func (e runCheckEvent) String() string {
	switch e {
	case runEvtRequestAdmitted:
		return "request-admitted"
	case runEvtRequestSuppressed:
		return "request-suppressed"
	case runEvtResultArrived:
		return "result-arrived"
	default:
		return "panic-reset"
	}
}

// runCheckMachine is the single-flight table.
var runCheckMachine = fsm.MustCompile(fsm.Machine[runCheckState, runCheckEvent]{
	Name:    "run-script-status-singleflight",
	Initial: runCheckIdle,
	Steps: []fsm.Step[runCheckState, runCheckEvent]{
		{From: runCheckIdle, Event: runEvtRequestAdmitted, To: runCheckInFlight},
		{From: runCheckInFlight, Event: runEvtRequestSuppressed, To: runCheckInFlight},
		{From: runCheckInFlight, Event: runEvtResultArrived, To: runCheckIdle},
		{From: runCheckInFlight, Event: runEvtPanicReset, To: runCheckIdle},
	},
})

// runCheckCheck formally verifies the single-flight: in-flight is entered
// only by admission and exited only by a result (or the panic reset) — a
// suppressed request never admits a second concurrent check.
func runCheckCheck() error {
	return runCheckMachine.Check(fsm.Checker[runCheckState, runCheckEvent]{
		Alphabet: []runCheckEvent{
			runEvtRequestAdmitted, runEvtRequestSuppressed,
			runEvtResultArrived, runEvtPanicReset,
		},
		AllDeclaredReachable: true,
		Edge: func(from runCheckState, event runCheckEvent, to runCheckState) error {
			if event == runEvtRequestAdmitted && from != runCheckIdle {
				return errors.New("a request may only be admitted while idle")
			}
			if event == runEvtRequestSuppressed && to != runCheckInFlight {
				return errors.New("a suppressed request must not disturb the in-flight check")
			}
			return nil
		},
	})
}
