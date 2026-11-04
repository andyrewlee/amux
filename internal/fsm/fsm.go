// Package fsm provides a declarative finite-state-machine core plus an
// exhaustive model checker, so a machine's transition table can be formally
// verified in ordinary Go tests (and therefore in make ci).
//
// A Machine is data: an initial state and a list of Steps. Compile turns it
// into a Compiled machine that production code steps through via Next — the
// table is the single source of truth for both runtime transitions and the
// checked model, so the two can never drift.
//
// Verification is reachability-based model checking: Check explores every
// state reachable from Initial under the declared event alphabet and asserts
// caller-supplied invariants, edge properties, reachability claims, and
// structural rules (no undeclared deadlocks, no unreachable declared states,
// no contradictory terminal marking). Every violation is reported with a
// witness: the shortest event path from Initial that exhibits it.
//
// Machines here model qualitative control state. Counters, timestamps, and
// other extended state stay at the call site; the call site reduces them to
// the machine's events. Undefined (state, event) pairs are rejections by
// design — stepping one returns ok=false, matching this codebase's
// fail-closed lifecycle guards.
package fsm

import (
	"fmt"
	"slices"
	"strings"
)

// Step is one declared transition: from --event--> to.
type Step[S, E comparable] struct {
	From  S
	Event E
	To    S
}

// Machine is the declarative form of a finite-state machine.
type Machine[S, E comparable] struct {
	// Name labels the machine in error messages.
	Name string
	// Initial is the state the machine starts in.
	Initial S
	// Steps lists every allowed transition. Duplicate (From, Event) pairs
	// are a Compile error — a machine must be deterministic.
	Steps []Step[S, E]
	// Terminals lists states the machine may legitimately rest in with no
	// outgoing edges. A reachable state with no outgoing edges that is not
	// terminal is a deadlock and fails Check. A terminal that does have
	// outgoing edges also fails Check (contradictory declaration).
	Terminals []S
}

// key identifies one table row.
type key[S, E comparable] struct {
	from  S
	event E
}

// Compiled is a validated, ready-to-step machine.
type Compiled[S, E comparable] struct {
	machine   Machine[S, E]
	steps     map[key[S, E]]S
	terminals map[S]bool
}

// Compile validates m (deterministic steps) and indexes it for stepping.
func Compile[S, E comparable](m Machine[S, E]) (*Compiled[S, E], error) {
	steps := make(map[key[S, E]]S, len(m.Steps))
	for _, s := range m.Steps {
		k := key[S, E]{from: s.From, event: s.Event}
		if prev, dup := steps[k]; dup {
			return nil, fmt.Errorf("fsm %q: duplicate transition for (%v, %v): %v and %v", m.Name, s.From, s.Event, prev, s.To)
		}
		steps[k] = s.To
	}
	terminals := make(map[S]bool, len(m.Terminals))
	for _, t := range m.Terminals {
		terminals[t] = true
	}
	return &Compiled[S, E]{machine: m, steps: steps, terminals: terminals}, nil
}

// MustCompile is Compile but panics on an invalid table. Package-level
// machine declarations use it so a broken table fails at first import
// (test time), never mid-run.
func MustCompile[S, E comparable](m Machine[S, E]) *Compiled[S, E] {
	c, err := Compile(m)
	if err != nil {
		panic(err)
	}
	return c
}

// Next applies event e in state from. ok is false when the table rejects the
// move — by convention an invalid or out-of-contract transition that the
// caller must treat as a no-op or a guarded rejection, never a crash.
func (c *Compiled[S, E]) Next(from S, e E) (to S, ok bool) {
	to, ok = c.steps[key[S, E]{from: from, event: e}]
	return to, ok
}

// MustNext applies event e and panics on a rejected transition. Use only on
// paths where a rejection is a programming error already excluded by Check.
func (c *Compiled[S, E]) MustNext(from S, e E) S {
	to, ok := c.Next(from, e)
	if !ok {
		panic(fmt.Sprintf("fsm %q: rejected transition %v --%v-->", c.machine.Name, from, e))
	}
	return to
}

// Events returns the deduplicated event alphabet implied by the table.
func (c *Compiled[S, E]) Events() []E {
	var out []E
	for k := range c.steps {
		if !slices.Contains(out, k.event) {
			out = append(out, k.event)
		}
	}
	return out
}

// path renders a witness route for error messages.
func path[S, E comparable](initial S, route []Step[S, E]) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%v", initial)
	for _, s := range route {
		fmt.Fprintf(&b, " --%v--> %v", s.Event, s.To)
	}
	return b.String()
}
