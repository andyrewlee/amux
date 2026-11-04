package sidebar

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/fsm"
)

// The pendingCreation map is a two-state machine per workspace: the state is
// key membership (absent = unmarked, present = create in flight) and the
// timestamp value is extended state carrying the TTL. A parallel runtime
// enum would be a second source of truth for membership, so the machine is
// verification-only: the table below is the protocol, and the differential
// test walks it against the real map helpers.
type pendingCreationState uint8

const (
	createUnmarked pendingCreationState = iota
	createInFlight
)

func (s pendingCreationState) String() string {
	if s == createInFlight {
		return "in-flight"
	}
	return "unmarked"
}

type pendingCreationEvent uint8

const (
	evMarkCreate      pendingCreationEvent = iota // a create was dispatched (markPendingCreation)
	evCreateResolved                              // SidebarTerminalCreated filed the tab
	evCreateFailed                                // SidebarTerminalCreateFailed landed
	evCreateExpired                               // the TTL decided the result was lost
	evWorkspaceClosed                             // CloseTerminal / workspace deletion
)

func (e pendingCreationEvent) String() string {
	switch e {
	case evMarkCreate:
		return "mark"
	case evCreateResolved:
		return "resolved"
	case evCreateFailed:
		return "failed"
	case evCreateExpired:
		return "expired"
	default:
		return "closed"
	}
}

var pendingCreationMachine = fsm.MustCompile(fsm.Machine[pendingCreationState, pendingCreationEvent]{
	Name:    "sidebar-terminal-pending-creation",
	Initial: createUnmarked,
	Steps: []fsm.Step[pendingCreationState, pendingCreationEvent]{
		{From: createUnmarked, Event: evMarkCreate, To: createInFlight},
		{From: createInFlight, Event: evCreateResolved, To: createUnmarked},
		{From: createInFlight, Event: evCreateFailed, To: createUnmarked},
		{From: createInFlight, Event: evCreateExpired, To: createUnmarked},
		{From: createInFlight, Event: evWorkspaceClosed, To: createUnmarked},
		{From: createUnmarked, Event: evWorkspaceClosed, To: createUnmarked},
	},
})

// TestPendingCreationMachineFormallyChecked verifies the pending-creation
// protocol: a mark is accepted only while unmarked (the duplicate-create
// rejection — markCreate has no edge from createInFlight), every in-flight
// path lands back unmarked (a lost result can never wedge creation forever),
// and the expiry edge exists (the TTL is load-bearing, not decorative).
func TestPendingCreationMachineFormallyChecked(t *testing.T) {
	if err := pendingCreationMachine.Check(fsm.Checker[pendingCreationState, pendingCreationEvent]{
		Alphabet: []pendingCreationEvent{
			evMarkCreate, evCreateResolved, evCreateFailed, evCreateExpired, evWorkspaceClosed,
		},
		AllDeclaredReachable: true,
		Edge: func(from pendingCreationState, event pendingCreationEvent, to pendingCreationState) error {
			if event == evMarkCreate && from != createUnmarked {
				return fmt.Errorf("a mark over an in-flight create (%v) is the duplicate the gate must reject", from)
			}
			if from == createInFlight && to != createUnmarked {
				return errors.New("every exit from in-flight must unmark — a lost result may not wedge creation")
			}
			return nil
		},
	}); err != nil {
		t.Fatalf("pending-creation machine failed model checking: %v", err)
	}
	if !pendingCreationInFlightReachable(evCreateExpired) {
		t.Fatal("the expiry edge must exist: a lost create result must be able to unmark")
	}
}

func pendingCreationInFlightReachable(ev pendingCreationEvent) bool {
	st := pendingCreationMachine.MustNext(createUnmarked, evMarkCreate)
	next, ok := pendingCreationMachine.Next(st, ev)
	return ok && next == createUnmarked
}

// TestPendingCreationDifferential drives the real map helpers through the
// machine's alphabet and asserts the map's membership tracks the machine's
// state at every step.
func TestPendingCreationDifferential(t *testing.T) {
	m := NewTerminalModel()
	wsID := "ws-diff"

	// Unmarked: no gate, and a mark is accepted.
	if m.pendingCreationActive(wsID) {
		t.Fatal("fresh workspace must be unmarked")
	}
	m.markPendingCreation(wsID)
	if !m.pendingCreationActive(wsID) {
		t.Fatal("after markCreate the create must gate duplicates")
	}

	// markCreate is rejected while in flight: the gate refuses the
	// duplicate (EnsureTerminalTab returns nil), and the table has no edge.
	if _, ok := pendingCreationMachine.Next(createInFlight, evMarkCreate); ok {
		t.Fatal("table must reject a duplicate mark while in flight")
	}
	stamp := m.pendingCreation[wsID]

	// createExpired: a stamp past the TTL unmarks in place.
	m.pendingCreation[wsID] = stamp.Add(-pendingCreationTimeout - time.Second)
	if m.pendingCreationActive(wsID) {
		t.Fatal("expired mark must not gate")
	}
	if _, ok := m.pendingCreation[wsID]; ok {
		t.Fatal("expired mark must be deleted (unmarked)")
	}

	// createResolved: a fresh mark unmarks on the result.
	m.markPendingCreation(wsID)
	m.handleCreateFailed(SidebarTerminalCreateFailed{WorkspaceID: wsID, Err: errors.New("boom")})
	if m.pendingCreationActive(wsID) {
		t.Fatal("a failed create must unmark so the user can retry")
	}

	// createResolved: mark, then the created handler clears it with the tab.
	m.markPendingCreation(wsID)
	m.handleTerminalCreated(SidebarTerminalCreated{WorkspaceID: wsID, TabID: "t", SessionName: "s"})
	if m.pendingCreationActive(wsID) {
		t.Fatal("a landed create must unmark")
	}

	// evWorkspaceClosed is idempotent from either state.
	m.markPendingCreation(wsID)
	m.CloseTerminal(wsID)
	if m.pendingCreationActive(wsID) {
		t.Fatal("workspace close must unmark")
	}
}
