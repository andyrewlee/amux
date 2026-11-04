package fsm

import (
	"strings"
	"testing"
)

// demoTurnstile is the classic two-state machine: locked until coin, open
// until push. Its full behavior is verified by hand below, so any checker
// regression shows up as a diff here.
type demoState int

const (
	demoLocked demoState = iota
	demoOpen
)

type demoEvent int

const (
	demoCoin demoEvent = iota
	demoPush
)

func (e demoEvent) String() string {
	switch e {
	case demoCoin:
		return "coin"
	case demoPush:
		return "push"
	}
	return "?"
}

func demoTurnstile() *Compiled[demoState, demoEvent] {
	return MustCompile(Machine[demoState, demoEvent]{
		Name:    "turnstile",
		Initial: demoLocked,
		Steps: []Step[demoState, demoEvent]{
			{From: demoLocked, Event: demoCoin, To: demoOpen},
			{From: demoOpen, Event: demoPush, To: demoLocked},
			{From: demoLocked, Event: demoPush, To: demoLocked}, // push on locked: no-op
		},
	})
}

func TestTurnstileChecksClean(t *testing.T) {
	m := demoTurnstile()
	if err := m.Check(Checker[demoState, demoEvent]{
		Alphabet: []demoEvent{demoCoin, demoPush},
	}); err != nil {
		t.Fatalf("clean machine failed Check: %v", err)
	}
}

func TestNextRejectsUndefined(t *testing.T) {
	m := demoTurnstile()
	// coin while open is undefined: the machine rejects it.
	if _, ok := m.Next(demoOpen, demoCoin); ok {
		t.Fatal("open+coin should be rejected")
	}
	if to, ok := m.Next(demoLocked, demoPush); !ok || to != demoLocked {
		t.Fatalf("locked+push should be a self-transition, got (%v, %v)", to, ok)
	}
}

func TestDuplicateStepFailsCompile(t *testing.T) {
	_, err := Compile(Machine[demoState, demoEvent]{
		Name:    "dup",
		Initial: demoLocked,
		Steps: []Step[demoState, demoEvent]{
			{From: demoLocked, Event: demoCoin, To: demoOpen},
			{From: demoLocked, Event: demoCoin, To: demoLocked},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate-transition error, got %v", err)
	}
}

func TestInvariantViolationHasWitness(t *testing.T) {
	m := demoTurnstile()
	err := m.Check(Checker[demoState, demoEvent]{
		Alphabet: []demoEvent{demoCoin, demoPush},
		Invariant: func(s demoState) error {
			if s == demoOpen {
				return errDemo("open is forbidden in this scenario")
			}
			return nil
		},
	})
	if err == nil {
		t.Fatal("forbidden-state invariant should fail")
	}
	if !strings.Contains(err.Error(), "coin") || !strings.Contains(err.Error(), "witness") {
		t.Fatalf("error should carry a witness path, got: %v", err)
	}
}

type errDemo string

func (e errDemo) Error() string { return string(e) }

func TestDeadlockDetection(t *testing.T) {
	// sink has no outgoing edges and is not terminal: a deadlock.
	m := MustCompile(Machine[demoState, demoEvent]{
		Name:    "deadlock",
		Initial: demoLocked,
		Steps: []Step[demoState, demoEvent]{
			{From: demoLocked, Event: demoCoin, To: demoOpen},
		},
	})
	err := m.Check(Checker[demoState, demoEvent]{
		Alphabet: []demoEvent{demoCoin, demoPush},
	})
	if err == nil || !strings.Contains(err.Error(), "deadlock") {
		t.Fatalf("expected deadlock error, got %v", err)
	}
	// Declaring it terminal resolves the deadlock...
	m2 := MustCompile(Machine[demoState, demoEvent]{
		Name:      "deadlock-terminal",
		Initial:   demoLocked,
		Steps:     []Step[demoState, demoEvent]{{From: demoLocked, Event: demoCoin, To: demoOpen}},
		Terminals: []demoState{demoOpen},
	})
	if err := m2.Check(Checker[demoState, demoEvent]{Alphabet: []demoEvent{demoCoin, demoPush}}); err != nil {
		t.Fatalf("terminal sink should pass: %v", err)
	}
	// ...but a terminal with edges is contradictory.
	m3 := MustCompile(Machine[demoState, demoEvent]{
		Name:      "terminal-with-edges",
		Initial:   demoLocked,
		Steps:     []Step[demoState, demoEvent]{{From: demoLocked, Event: demoCoin, To: demoOpen}},
		Terminals: []demoState{demoLocked},
	})
	err = m3.Check(Checker[demoState, demoEvent]{Alphabet: []demoEvent{demoCoin, demoPush}})
	if err == nil || !strings.Contains(err.Error(), "declared terminal but has") {
		t.Fatalf("expected terminal-with-edges error, got %v", err)
	}
}

func TestUnreachableDeclaredState(t *testing.T) {
	// locked self-loops (no deadlock), but open is never reached: orphaned.
	m := MustCompile(Machine[demoState, demoEvent]{
		Name:    "orphan",
		Initial: demoLocked,
		Steps: []Step[demoState, demoEvent]{
			{From: demoLocked, Event: demoPush, To: demoLocked},
			{From: demoOpen, Event: demoPush, To: demoLocked}, // open never reached
		},
	})
	err := m.Check(Checker[demoState, demoEvent]{
		Alphabet:             []demoEvent{demoCoin, demoPush},
		AllDeclaredReachable: true,
	})
	if err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("expected unreachable-state error, got %v", err)
	}
}

func TestEdgeProperty(t *testing.T) {
	m := demoTurnstile()
	err := m.Check(Checker[demoState, demoEvent]{
		Alphabet: []demoEvent{demoCoin, demoPush},
		Edge: func(from demoState, e demoEvent, to demoState) error {
			if from == to && e == demoPush {
				return errDemo("push must always change the state")
			}
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "edge property") {
		t.Fatalf("expected edge-property error, got %v", err)
	}
}

func TestForbidReachable(t *testing.T) {
	m := demoTurnstile()
	if err := m.Check(Checker[demoState, demoEvent]{
		Alphabet:        []demoEvent{demoCoin, demoPush},
		ForbidReachable: []demoState{demoLocked, demoOpen},
	}); err == nil {
		t.Fatal("demoLocked is the initial state, so it is reachable")
	}
	if err := m.Check(Checker[demoState, demoEvent]{
		Alphabet:        []demoEvent{demoCoin, demoPush},
		ForbidReachable: []demoState{demoOpen},
	}); err == nil {
		t.Fatal("demoOpen is reachable by a coin, so it must be flagged")
	}
}

func TestReachableEnumeration(t *testing.T) {
	m := demoTurnstile()
	got := m.Reachable([]demoEvent{demoCoin, demoPush})
	if len(got) != 2 {
		t.Fatalf("expected both states reachable, got %v", got)
	}
}

func TestMustNextPanicsOnRejected(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustNext should panic on a rejected transition")
		}
	}()
	demoTurnstile().MustNext(demoOpen, demoCoin)
}
