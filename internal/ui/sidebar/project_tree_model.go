// Package sidebar: project_tree_model.go holds the project tree's load
// phase machine — the explicit form of the model-level async-loader state
// (generation, scheduler sets, and the parked refresh root stay alongside as
// extended state; see project_tree_load.go for the scheduler).
package sidebar

import (
	"errors"
	"fmt"
	"slices"

	"github.com/andyrewlee/amux/internal/fsm"
)

// treePhase is the project tree's model-level load phase. It makes the
// refresh lifecycle explicit: a refresh parks a shadow root (pendingRoot)
// while the old tree stays visible, and only that root's result — matched by
// identity — may apply it. The per-node loading markers (pendingRequest,
// loadErr) are node state, checked separately in
// project_tree_node_model_test.go.
type treePhase uint8

const (
	// treeIdle is the zero value: nothing in flight, no refresh parked, no
	// root error.
	treeIdle treePhase = iota
	// treeLoadingDirs: directory reads are queued or executing under the
	// current generation (the initial root load, expansions, or a refresh's
	// restored children).
	treeLoadingDirs
	// treeRefreshing: a full refresh is in flight — pendingRoot is parked
	// and the old tree remains visible and interactive.
	treeRefreshing
	// treeRootErr: the root load failed; the error payload (rootErr) is
	// carried beside the phase and the old tree (if any) is retained.
	treeRootErr
)

func (s treePhase) String() string {
	switch s {
	case treeLoadingDirs:
		return "loading-dirs"
	case treeRefreshing:
		return "refreshing"
	case treeRootErr:
		return "root-err"
	default:
		return "idle"
	}
}

// treeEvent is the tree loader alphabet.
type treeEvent uint8

const (
	evLoadScheduled      treeEvent = iota // a directory read was enqueued
	evLoadsDrained                        // the last in-flight read under this generation applied
	evRefreshStarted                      // reloadTree parked a shadow root
	evRefreshRootApplied                  // the parked root's listing applied by identity
	evRefreshRootFailed                   // the parked root's read failed
	evRefreshRebased                      // a root-path rebase dropped the parked root
	evTreeCleared                         // the workspace went away; everything invalidated
)

func (e treeEvent) String() string {
	switch e {
	case evLoadScheduled:
		return "load-scheduled"
	case evLoadsDrained:
		return "loads-drained"
	case evRefreshStarted:
		return "refresh-started"
	case evRefreshRootApplied:
		return "refresh-root-applied"
	case evRefreshRootFailed:
		return "refresh-root-failed"
	case evRefreshRebased:
		return "refresh-rebased"
	default:
		return "tree-cleared"
	}
}

// treeMachine is the tree's load-phase table. The load events keep the
// loading phase honest from any non-refreshing phase (a scheduled read while
// a refresh is parked must not mask the refresh); only the two root-result
// events leave treeRefreshing via a result, and they are stepped solely from
// applyDirectoryLoaded's pendingRoot identity match — the invariant that a
// refresh result applies only through the parked root.
var treeMachine = fsm.MustCompile(fsm.Machine[treePhase, treeEvent]{
	Name:    "project-tree-load",
	Initial: treeIdle,
	Steps: []fsm.Step[treePhase, treeEvent]{
		{From: treeIdle, Event: evLoadScheduled, To: treeLoadingDirs},
		{From: treeLoadingDirs, Event: evLoadScheduled, To: treeLoadingDirs},
		{From: treeRootErr, Event: evLoadScheduled, To: treeLoadingDirs},
		// Expanding old-tree nodes while a refresh is parked stays within
		// the refresh — the parked root still owns the phase.
		{From: treeRefreshing, Event: evLoadScheduled, To: treeRefreshing},

		{From: treeLoadingDirs, Event: evLoadsDrained, To: treeIdle},

		{From: treeIdle, Event: evRefreshStarted, To: treeRefreshing},
		{From: treeLoadingDirs, Event: evRefreshStarted, To: treeRefreshing},
		{From: treeRefreshing, Event: evRefreshStarted, To: treeRefreshing},
		{From: treeRootErr, Event: evRefreshStarted, To: treeRefreshing},

		{From: treeRefreshing, Event: evRefreshRootApplied, To: treeIdle},
		{From: treeRefreshing, Event: evRefreshRootFailed, To: treeRootErr},
		// A root-path rebase unparks the shadow root and re-requests it as
		// an ordinary load (reRequestRebasedTree).
		{From: treeRefreshing, Event: evRefreshRebased, To: treeLoadingDirs},

		{From: treeIdle, Event: evTreeCleared, To: treeIdle},
		{From: treeLoadingDirs, Event: evTreeCleared, To: treeIdle},
		{From: treeRefreshing, Event: evTreeCleared, To: treeIdle},
		{From: treeRootErr, Event: evTreeCleared, To: treeIdle},
	},
})

// TreeLoadCheck formally verifies the tree load-phase table: every phase
// reachable, refresh results apply only from treeRefreshing (never from an
// idle or ordinary-loading tree — the pendingRoot identity invariant), a
// drain returns to idle only from treeLoadingDirs, and every phase can both
// clear (workspace switch) and start a refresh (the retry affordance).
func TreeLoadCheck() error {
	if err := treeMachine.Check(fsm.Checker[treePhase, treeEvent]{
		Alphabet: []treeEvent{
			evLoadScheduled, evLoadsDrained, evRefreshStarted,
			evRefreshRootApplied, evRefreshRootFailed, evRefreshRebased,
			evTreeCleared,
		},
		AllDeclaredReachable: true,
		Edge: func(from treePhase, event treeEvent, to treePhase) error {
			switch event {
			case evRefreshRootApplied, evRefreshRootFailed:
				if from != treeRefreshing {
					return fmt.Errorf("a refresh root result (%v) applies only while a refresh is parked, not from %v", event, from)
				}
			case evRefreshRebased:
				if from != treeRefreshing {
					return errors.New("only a parked refresh can be dropped by a rebase")
				}
			case evLoadsDrained:
				if from != treeLoadingDirs || to != treeIdle {
					return errors.New("only an ordinary loading phase drains to idle")
				}
			case evLoadScheduled:
				if to != treeLoadingDirs && to != treeRefreshing {
					return errors.New("a scheduled read must enter or preserve loading")
				}
			case evTreeCleared:
				if to != treeIdle {
					return errors.New("clearing must always return to idle")
				}
			}
			return nil
		},
	}); err != nil {
		return err
	}
	for _, phase := range []treePhase{treeIdle, treeLoadingDirs, treeRefreshing, treeRootErr} {
		if !slices.Contains(treeMachine.ReachableFrom(phase, nil), treeIdle) {
			return fmt.Errorf("phase %v cannot reach idle: every load/refresh must be able to settle", phase)
		}
	}
	return nil
}

// step advances the tree's load phase in place; a rejected move (a drain
// with nothing draining, a root result with no refresh parked) leaves the
// phase untouched — a no-op the loader treats as fail-closed.
func (s *treePhase) step(ev treeEvent) {
	if next, ok := treeMachine.Next(*s, ev); ok {
		*s = next
	}
}
