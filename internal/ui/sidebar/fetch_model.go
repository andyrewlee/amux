// Package sidebar: fetch_model.go holds the shared async-fetch machine used
// by the Changes tab's branch-vs-base list and its ahead/behind badge —
// two instances of one checked table with different payloads.
package sidebar

import (
	"errors"
	"fmt"

	"github.com/andyrewlee/amux/internal/fsm"
)

// fetchState is the phase of one async fetch. It collapses the former
// branchMode/branchLoading flag pair (and the ahead/behind fetch's implicit
// "no data yet" state) into a single phase whose illegal combinations —
// loading∧error, loaded∧loading — are unrepresentable.
type fetchState uint8

const (
	// fetchIdle is the zero value: not engaged (branch list off) or no
	// fetch has been issued since the last reset.
	fetchIdle fetchState = iota
	// fetchLoading: a fetch is in flight under the current load ID.
	fetchLoading
	// fetchLoaded: the current load's result has been applied.
	fetchLoaded
	// fetchError: the current load's result failed; the error payload is
	// kept beside the phase at the call site.
	fetchError
)

func (s fetchState) String() string {
	switch s {
	case fetchLoading:
		return "loading"
	case fetchLoaded:
		return "loaded"
	case fetchError:
		return "error"
	default:
		return "idle"
	}
}

// fetchEvent is the fetch alphabet. Load IDs, roots, and payloads stay at
// the call site as extended state; the drop-stale guards there reduce to
// these events.
type fetchEvent uint8

const (
	evFetchStart   fetchEvent = iota // a fetch was issued (toggle-on, refresh)
	evFetchSucceed                   // the current load's result applied
	evFetchFail                      // the current load's result failed
	evFetchReset                     // hard reset (toggle-off, workspace switch)
)

func (e fetchEvent) String() string {
	switch e {
	case evFetchStart:
		return "start"
	case evFetchSucceed:
		return "succeed"
	case evFetchFail:
		return "fail"
	default:
		return "reset"
	}
}

// fetchMachine is the shared fetch table: a start (re)enters loading from
// any phase — including a superseding start while one is already loading
// (the ahead/behind refresh path; the loser drops via its load ID) —
// results apply only while loading, and a reset returns to idle from
// anywhere. Both the branch-changes fetch and the ahead/behind fetch step
// this one table; their call sites decide when each event fires (the branch
// instance only starts from idle because its toggle passes through off,
// while the ahead/behind instance refreshes directly from loaded or error).
var fetchMachine = fsm.MustCompile(fsm.Machine[fetchState, fetchEvent]{
	Name:    "changes-async-fetch",
	Initial: fetchIdle,
	Steps: []fsm.Step[fetchState, fetchEvent]{
		{From: fetchIdle, Event: evFetchStart, To: fetchLoading},
		{From: fetchLoading, Event: evFetchStart, To: fetchLoading},
		{From: fetchLoading, Event: evFetchSucceed, To: fetchLoaded},
		{From: fetchLoading, Event: evFetchFail, To: fetchError},
		{From: fetchLoaded, Event: evFetchStart, To: fetchLoading},
		{From: fetchError, Event: evFetchStart, To: fetchLoading},
		{From: fetchIdle, Event: evFetchReset, To: fetchIdle},
		{From: fetchLoading, Event: evFetchReset, To: fetchIdle},
		{From: fetchLoaded, Event: evFetchReset, To: fetchIdle},
		{From: fetchError, Event: evFetchReset, To: fetchIdle},
	},
})

// FetchCheck formally verifies the shared fetch table: every phase
// reachable, results (succeed/fail) apply only while a fetch is loading —
// so a stale or foreign result cannot mutate the phase — and a reset
// always returns to idle from any phase.
func FetchCheck() error {
	return fetchMachine.Check(fsm.Checker[fetchState, fetchEvent]{
		Alphabet:             []fetchEvent{evFetchStart, evFetchSucceed, evFetchFail, evFetchReset},
		AllDeclaredReachable: true,
		Edge: func(from fetchState, event fetchEvent, to fetchState) error {
			switch event {
			case evFetchSucceed, evFetchFail:
				if from != fetchLoading {
					return fmt.Errorf("a result (%v) applies only while loading, not %v", event, from)
				}
			case evFetchReset:
				if to != fetchIdle {
					return errors.New("a reset must always return to idle")
				}
			case evFetchStart:
				if to != fetchLoading {
					return errors.New("a start must always enter loading")
				}
			}
			return nil
		},
	})
}

// step advances the fetch phase in place; a rejected move (a result with no
// fetch in flight, for instance) leaves the phase untouched — a fail-closed
// no-op, never a crash.
func (s *fetchState) step(ev fetchEvent) {
	if next, ok := fetchMachine.Next(*s, ev); ok {
		*s = next
	}
}
