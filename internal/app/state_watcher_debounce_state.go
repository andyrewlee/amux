package app

import (
	"errors"

	"github.com/andyrewlee/amux/internal/fsm"
)

// watcherDebounceState models stateWatcher's debounce lifecycle
// (state_watcher.go): constructed O(1), registered at Run start, events
// coalesced behind one timer, flushed sorted-by-reason on fire. The
// pending path map, the timer, and the fsnotify plumbing are extended
// state. The load-bearing property is the Close contract: Close during a
// pending debounce DROPS the queued paths (the callback never fires for
// them), and every event after Close is a no-op.
type watcherDebounceState uint8

const (
	// watcherConstructed: newStateWatcher returned; nothing registered yet
	// (the fsnotify Add loop is deferred off the first-paint path).
	watcherConstructed watcherDebounceState = iota
	// watcherRegistered: watches live, no debounce pending.
	watcherRegistered
	// watcherDebouncing: a timer is armed and paths are queued (each new
	// event Resets it).
	watcherDebouncing
	// watcherClosed: Close ran — terminal. Queued paths were dropped.
	watcherClosed
)

func (s watcherDebounceState) String() string {
	switch s {
	case watcherRegistered:
		return "registered"
	case watcherDebouncing:
		return "debouncing"
	case watcherClosed:
		return "closed"
	default:
		return "constructed"
	}
}

// watcherDebounceEvent is the watcher machine's alphabet.
type watcherDebounceEvent uint8

const (
	// watcherEvtRegister: Run registered the watches (plus the one-shot
	// reconcile for the construct→register window).
	watcherEvtRegister watcherDebounceEvent = iota
	// watcherEvtFsEvent: an fsnotify event queued a path (arming or
	// resetting the timer).
	watcherEvtFsEvent
	// watcherEvtTimerFired: the debounce elapsed — flush the queued paths
	// to onChanged (sorted, per-reason) and re-arm nothing.
	watcherEvtTimerFired
	// watcherEvtClose: Close — from debouncing this DROPS the queued paths.
	watcherEvtClose
)

func (e watcherDebounceEvent) String() string {
	switch e {
	case watcherEvtRegister:
		return "register"
	case watcherEvtFsEvent:
		return "fs-event"
	case watcherEvtTimerFired:
		return "timer-fired"
	default:
		return "close"
	}
}

// watcherDebounceMachine is the watcher debounce table. No event is
// declared from closed — scheduleNotify, fire, and the metadata-dir
// watchers all guard on sw.closed, which the checker's fail-closed rule
// (an undeclared pair is a rejection) makes structural.
var watcherDebounceMachine = fsm.MustCompile(fsm.Machine[watcherDebounceState, watcherDebounceEvent]{
	Name:      "state-watcher-debounce",
	Initial:   watcherConstructed,
	Terminals: []watcherDebounceState{watcherClosed},
	Steps: []fsm.Step[watcherDebounceState, watcherDebounceEvent]{
		{From: watcherConstructed, Event: watcherEvtRegister, To: watcherRegistered},
		{From: watcherRegistered, Event: watcherEvtFsEvent, To: watcherDebouncing},
		{From: watcherDebouncing, Event: watcherEvtFsEvent, To: watcherDebouncing},
		{From: watcherDebouncing, Event: watcherEvtTimerFired, To: watcherRegistered},
		{From: watcherConstructed, Event: watcherEvtClose, To: watcherClosed},
		{From: watcherRegistered, Event: watcherEvtClose, To: watcherClosed},
		{From: watcherDebouncing, Event: watcherEvtClose, To: watcherClosed},
	},
})

// watcherDebounceCheck formally verifies the debounce lifecycle: the
// flush-to-callback edge (timer-fired) exists only while debouncing, close
// from debouncing reaches closed (dropping the queued paths — the terminal
// proves no path back to observing), and every state is reachable.
func watcherDebounceCheck() error {
	return watcherDebounceMachine.Check(fsm.Checker[watcherDebounceState, watcherDebounceEvent]{
		Alphabet: []watcherDebounceEvent{
			watcherEvtRegister, watcherEvtFsEvent, watcherEvtTimerFired, watcherEvtClose,
		},
		AllDeclaredReachable: true,
		Edge: func(_ watcherDebounceState, event watcherDebounceEvent, to watcherDebounceState) error {
			if event == watcherEvtTimerFired && to != watcherRegistered {
				return errors.New("a fired debounce flushes back to registered")
			}
			if event == watcherEvtClose && to != watcherClosed {
				return errors.New("close always closes")
			}
			return nil
		},
	})
}
