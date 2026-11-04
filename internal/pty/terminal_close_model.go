package pty

import (
	"errors"
	"fmt"

	"github.com/andyrewlee/amux/internal/fsm"
)

// closeState is the control state of a Terminal's lifecycle and, once Close
// begins, its teardown escalation. The implementation (Terminal's closed,
// cmd, ptyFile, waitDone fields plus Close's SIGTERM→wait→SIGKILL→wait
// ladder) is this machine's extended state; the table below is the verified
// spec that Close must implement.
type closeState int

const (
	// openRunning: constructed and not yet exited. Covers both monitored
	// terminals (New always attaches a wait monitor) and monitor-less test
	// constructions whose ProcessState is still nil — Running() treats the
	// two identically.
	openRunning closeState = iota
	// openExitedNoMonitor: the process exited but no waitDone channel
	// exists; only cmd.ProcessState records it (test-constructed terminals
	// whose waiter ran outside the package).
	openExitedNoMonitor
	// openExited: the wait monitor completed; Close will fast-path.
	openExited
	// closedTerminating: Close began and sent SIGTERM to the process
	// group; waiting up to terminalCloseTimeout.
	closedTerminating
	// closedKilling: SIGTERM timed out; SIGKILL sent; waiting again.
	closedKilling
	// closedAbandoned: even SIGKILL did not produce a wait within the
	// timeout — Close returned leaving the waiter stuck. Terminal resting
	// state; observable as closed with an unclosed waitDone.
	closedAbandoned
	// closedReaped: teardown finished with a completed wait. Terminal
	// resting state.
	closedReaped
)

func (s closeState) String() string {
	switch s {
	case openRunning:
		return "open-running"
	case openExitedNoMonitor:
		return "open-exited-no-monitor"
	case openExited:
		return "open-exited"
	case closedTerminating:
		return "closed-terminating"
	case closedKilling:
		return "closed-killing"
	case closedAbandoned:
		return "closed-abandoned"
	default:
		return "closed-reaped"
	}
}

// closeEvent is the escalation protocol's alphabet.
type closeEvent int

const (
	// evtProcExit: the wait monitor completed while open.
	evtProcExit closeEvent = iota
	// evtExternalWait: a monitor-less terminal's process was reaped by a
	// waiter outside the package (cmd.ProcessState became non-nil).
	evtExternalWait
	// evtMonitorAttach: startWaitMonitor (or Close's fresh async wait for a
	// monitor-less terminal) installs a waitDone channel. Legal only where
	// waitDone was nil.
	evtMonitorAttach
	// evtCloseBegin: Close entered (first time; a second Close from a
	// closed state is the idempotent no-op self-loop).
	evtCloseBegin
	// evtSigtermWaitOk: the process exited within terminalCloseTimeout of
	// SIGTERM.
	evtSigtermWaitOk
	// evtSigtermTimeout: SIGTERM was not enough; escalate to SIGKILL.
	evtSigtermTimeout
	// evtSigkillOk: the process exited within the second timeout.
	evtSigkillOk
	// evtSigkillTimeout: even SIGKILL produced no wait; abandon.
	evtSigkillTimeout
)

func (e closeEvent) String() string {
	switch e {
	case evtProcExit:
		return "proc-exit"
	case evtExternalWait:
		return "external-wait"
	case evtMonitorAttach:
		return "monitor-attach"
	case evtCloseBegin:
		return "close-begin"
	case evtSigtermWaitOk:
		return "sigterm-wait-ok"
	case evtSigtermTimeout:
		return "sigterm-timeout"
	case evtSigkillOk:
		return "sigkill-ok"
	default:
		return "sigkill-timeout"
	}
}

// closeMachine is the close/teardown escalation protocol. Production always
// enters at openRunning via New (monitor attached); openExitedNoMonitor is
// the monitor-less entry used by hand-built test terminals.
var closeMachine = fsm.MustCompile(fsm.Machine[closeState, closeEvent]{
	Name:    "terminal-close-escalation",
	Initial: openRunning,
	Steps: []fsm.Step[closeState, closeEvent]{
		{From: openRunning, Event: evtProcExit, To: openExited},
		{From: openRunning, Event: evtExternalWait, To: openExitedNoMonitor},
		// Self-loop: in production the monitor is attached exactly once at
		// construction, so this edge is the (harmless, no-op) re-attach;
		// the waitDone-was-nil precondition is a caller contract.
		{From: openRunning, Event: evtMonitorAttach, To: openRunning},
		{From: openExitedNoMonitor, Event: evtMonitorAttach, To: openExited},
		{From: openRunning, Event: evtCloseBegin, To: closedTerminating},
		// A monitor-less exited terminal still runs the full ladder: Close
		// installs a fresh async wait, then signals (harmlessly) and
		// reaps.
		{From: openExitedNoMonitor, Event: evtCloseBegin, To: closedTerminating},
		{From: openExited, Event: evtCloseBegin, To: closedReaped},
		{From: closedTerminating, Event: evtSigtermWaitOk, To: closedReaped},
		{From: closedTerminating, Event: evtSigtermTimeout, To: closedKilling},
		{From: closedKilling, Event: evtSigkillOk, To: closedReaped},
		{From: closedKilling, Event: evtSigkillTimeout, To: closedAbandoned},
		// Idempotence: a second Close from any closed state is a no-op
		// self-loop (the closed flag short-circuits before any signal).
		{From: closedTerminating, Event: evtCloseBegin, To: closedTerminating},
		{From: closedKilling, Event: evtCloseBegin, To: closedKilling},
		{From: closedAbandoned, Event: evtCloseBegin, To: closedAbandoned},
		{From: closedReaped, Event: evtCloseBegin, To: closedReaped},
	},
})

// closeRank orders states along the escalation ladder; no declared edge may
// retreat (decrease it). The two open-exited forms share a rank: monitor
// attachment moves between them without progression or regression.
func closeRank(s closeState) int {
	switch s {
	case openRunning:
		return 0
	case openExitedNoMonitor, openExited:
		return 1
	case closedTerminating:
		return 2
	case closedKilling:
		return 3
	default: // closedReaped, closedAbandoned
		return 4
	}
}

// CloseModelCheck formally verifies the escalation protocol: escalation is
// monotone (never retreats from killing back to terminating, nor from any
// closed state back to open), abandoned is reached only via a SIGKILL
// timeout and rests there, every open state can still reach a resting state
// (reaped or abandoned), a second Close from any closed state is a no-op
// self-loop, and monitor-attach is declared only from waitDone-nil states.
func CloseModelCheck() error {
	alphabet := []closeEvent{
		evtProcExit, evtExternalWait, evtMonitorAttach, evtCloseBegin,
		evtSigtermWaitOk, evtSigtermTimeout, evtSigkillOk, evtSigkillTimeout,
	}
	err := closeMachine.Check(fsm.Checker[closeState, closeEvent]{
		Alphabet: alphabet,
		Edge: func(from closeState, event closeEvent, to closeState) error {
			if closeRank(to) < closeRank(from) {
				return fmt.Errorf("escalation retreats: rank %d -> %d", closeRank(from), closeRank(to))
			}
			if to == closedAbandoned && event != evtSigkillTimeout &&
				!(event == evtCloseBegin && from == closedAbandoned) {
				return fmt.Errorf("abandon reachable via %v: only a SIGKILL timeout may abandon", event)
			}
			if event == evtSigkillTimeout && to != closedAbandoned {
				return errors.New("a SIGKILL timeout must abandon, not continue")
			}
			if event == evtMonitorAttach && from != openRunning && from != openExitedNoMonitor {
				return errors.New("monitor-attach is legal only where waitDone was nil")
			}
			if event == evtCloseBegin && from != openRunning && from != openExitedNoMonitor && from != openExited {
				if to != from {
					return fmt.Errorf("second Close from %v moved to %v: must be a no-op self-loop", from, to)
				}
			}
			return nil
		},
	})
	if err != nil {
		return err
	}

	// Abandoned is a resting state: its only declared outgoing edge is the
	// idempotent close-begin self-loop.
	for _, s := range closeMachine.Reachable(alphabet) {
		if s == closedAbandoned {
			for _, e := range alphabet {
				if to, ok := closeMachine.Next(s, e); ok && !(e == evtCloseBegin && to == s) {
					return fmt.Errorf("abandoned has a non-idempotent outgoing edge %v -> %v", e, to)
				}
			}
		}
	}

	// Liveness: every open state must still be able to reach a resting
	// state (reaped or abandoned) — teardown can never wedge the terminal
	// in a phase with no exit.
	for _, s := range []closeState{openRunning, openExitedNoMonitor, openExited} {
		reach := closeMachine.ReachableFrom(s, alphabet)
		if !containsState(reach, closedReaped) && !containsState(reach, closedAbandoned) {
			return fmt.Errorf("open state %v cannot reach reaped or abandoned", s)
		}
	}
	return nil
}

func containsState(states []closeState, want closeState) bool {
	for _, s := range states {
		if s == want {
			return true
		}
	}
	return false
}

// closePhase is Close's position in the escalation ladder. The Terminal
// struct itself cannot distinguish terminating from killing (Close nils cmd
// and ptyFile up front); the phase is observable only via the kill hooks
// during the single Close call, so snapshotClose takes it as input.
type closePhase int

const (
	phaseOpen closePhase = iota
	phaseTerminating
	phaseKilling
	phaseSettled
)

// closeSnapshot is everything the readers (Write/Read/Running/IsClosed/File)
// and Close's ladder can observe of a Terminal at one instant.
type closeSnapshot struct {
	closed  bool
	hasCmd  bool
	waitNil bool // waitDone == nil
	waitOff bool // waitDone != nil and already closed
	exited  bool // cmd != nil && cmd.ProcessState != nil (monitor-less reaped)
	phase   closePhase
}

// snapshotClose reads the observable fields under the mutex and records the
// caller's view of the escalation phase. Test/model aid only — production
// Close paths never consult it.
func (t *Terminal) snapshotClose(phase closePhase) closeSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := closeSnapshot{
		closed:  t.closed,
		hasCmd:  t.cmd != nil,
		waitNil: t.waitDone == nil,
		phase:   phase,
	}
	if !s.waitNil && waitComplete(t.waitDone) {
		s.waitOff = true
	}
	if t.cmd != nil && t.cmd.ProcessState != nil {
		s.exited = true
	}
	return s
}

// state derives the machine state from the snapshot — the formal twin of
// what the reader methods distinguish. After Close returns, closedReaped
// and closedAbandoned remain separable — but only for monitored terminals:
// an abandoned waiter never closes its channel, so waitOff stays false.
// A monitor-less terminal's fresh async wait is assigned to a Close local,
// never to t.waitDone, so abandonment there is field-invisible and reads
// as reaped (waitNil).
func (s closeSnapshot) state() closeState {
	if !s.closed {
		switch {
		case !s.hasCmd:
			// Reader contract: cmd == nil while open reads as not running.
			return openExited
		case s.waitNil:
			if s.exited {
				return openExitedNoMonitor
			}
			return openRunning
		case s.waitOff:
			return openExited
		default:
			return openRunning
		}
	}
	switch s.phase {
	case phaseTerminating:
		return closedTerminating
	case phaseKilling:
		return closedKilling
	default: // phaseSettled (phaseOpen cannot co-occur with closed)
		if s.waitNil || s.waitOff {
			return closedReaped
		}
		return closedAbandoned
	}
}
