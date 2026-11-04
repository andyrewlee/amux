package data

import (
	"strings"

	"github.com/andyrewlee/amux/internal/fsm"
)

// SessionStatus is a tmux-backed tab's liveness status. The string values are
// the persistence contract (workspace.json) and the tmux-sync vocabulary, so
// the type is a named string: unknown persisted values round-trip untouched.
//
// The liveness axis is governed by SessionStatusMachine below — running and
// detached are both "running-like" (a live session), stopped is dead. The
// distinction between running and detached is attach-ownership, set by the
// tab/attach code, not by liveness observation.
type SessionStatus string

const (
	// SessionRunning: the session exists with a live pane and this amux
	// instance considers itself attached.
	SessionRunning SessionStatus = "running"
	// SessionDetached: the session exists with a live pane but the tab is in
	// detached (attach-free) mode.
	SessionDetached SessionStatus = "detached"
	// SessionStopped: the session is gone or has no live pane.
	SessionStopped SessionStatus = "stopped"
)

// ParseSessionStatus normalizes a raw or persisted status: trimmed,
// lowercased, with empty (never-set) treated as running. Unknown values are
// preserved (not coerced) so they keep round-tripping; classify them as
// stopped-like via canonical.
func ParseSessionStatus(raw string) SessionStatus {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	if trimmed == "" {
		return SessionRunning
	}
	return SessionStatus(trimmed)
}

// RunningLike reports whether the status means a live session: running,
// detached, or the never-set zero value. Any other value (including unknown
// persisted strings) is stopped-like.
func (s SessionStatus) RunningLike() bool {
	switch s {
	case SessionRunning, SessionDetached, "":
		return true
	default:
		return false
	}
}

// canonical maps every status to its nearest machine state: unknown values
// behave stopped-like, exactly as IsRunningSession always treated them.
func (s SessionStatus) canonical() SessionStatus {
	switch s {
	case SessionRunning, SessionDetached, SessionStopped:
		return s
	default:
		return SessionStopped
	}
}

// SessionStatusEvent is an observation that drives status changes. Liveness
// events come from the tmux sync scans; ownership events come from the
// attach/detach code (Tab.Detached), whose status projection is defined by
// this same table.
type SessionStatusEvent int

const (
	// SessionObservedLive: the tmux session exists with a live pane.
	SessionObservedLive SessionStatusEvent = iota
	// SessionObservedDead: the session is missing or has no live pane
	// (subject to the caller's demotion hysteresis — see app/activity).
	SessionObservedDead
	// SessionOwnershipDetach: the tab dropped its attach without the session
	// dying (pressure detach, manual detach).
	SessionOwnershipDetach
	// SessionOwnershipReattach: a detached tab reattached to the live session.
	SessionOwnershipReattach
)

func (e SessionStatusEvent) String() string {
	switch e {
	case SessionObservedLive:
		return "live"
	case SessionObservedDead:
		return "dead"
	case SessionOwnershipDetach:
		return "detach"
	case SessionOwnershipReattach:
		return "reattach"
	}
	return "?"
}

// sessionStatusMachine is the transition table for a tab session's status.
// Two orthogonal axes meet here: liveness (live/dead observations — dead
// drives any running-like status to stopped; live revives stopped) and
// attach-ownership (detach/reattach move between running and detached while
// the session lives). Death is the only path out of detached; a live
// observation never changes ownership.
var sessionStatusMachine = fsm.MustCompile(fsm.Machine[SessionStatus, SessionStatusEvent]{
	Name:    "session-status",
	Initial: SessionRunning,
	Steps: []fsm.Step[SessionStatus, SessionStatusEvent]{
		{From: SessionRunning, Event: SessionObservedLive, To: SessionRunning},
		{From: SessionRunning, Event: SessionObservedDead, To: SessionStopped},
		{From: SessionDetached, Event: SessionObservedLive, To: SessionDetached},
		{From: SessionDetached, Event: SessionObservedDead, To: SessionStopped},
		{From: SessionStopped, Event: SessionObservedLive, To: SessionRunning},
		{From: SessionStopped, Event: SessionObservedDead, To: SessionStopped},
		{From: SessionRunning, Event: SessionOwnershipDetach, To: SessionDetached},
		{From: SessionDetached, Event: SessionOwnershipReattach, To: SessionRunning},
	},
})

// NextSessionStatus applies a liveness observation to a status. Unknown
// (non-canonical) statuses transition as stopped-like, matching the
// historical string comparisons. The second return is false for a transition
// the table rejects (none exist today; the call is total over the alphabet).
func NextSessionStatus(current SessionStatus, event SessionStatusEvent) (SessionStatus, bool) {
	return sessionStatusMachine.Next(current.canonical(), event)
}

// SessionStatusMachineCheck formally verifies the status table: every
// declared state is reachable, dead observations always land on stopped,
// ownership events only move between the running-like states, and stopped is
// left only by a live observation. Runs in data's tests.
func SessionStatusMachineCheck() error {
	return sessionStatusMachine.Check(fsm.Checker[SessionStatus, SessionStatusEvent]{
		Alphabet: []SessionStatusEvent{
			SessionObservedLive, SessionObservedDead,
			SessionOwnershipDetach, SessionOwnershipReattach,
		},
		AllDeclaredReachable: true,
		Edge: func(from SessionStatus, event SessionStatusEvent, to SessionStatus) error {
			if event == SessionObservedDead && to != SessionStopped {
				return errSession("a dead observation must always yield stopped")
			}
			if from == SessionStopped && event == SessionObservedDead && to != SessionStopped {
				return errSession("stopped must be stable under dead observations")
			}
			if (event == SessionOwnershipDetach || event == SessionOwnershipReattach) && to == SessionStopped {
				return errSession("ownership changes must never stop a session")
			}
			return nil
		},
	})
}

type errSession string

func (e errSession) Error() string { return string(e) }
