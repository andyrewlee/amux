package fsm

import (
	"fmt"
	"slices"
	"strings"
)

// Checker configures an exhaustive verification run. The zero value checks
// only structural properties (determinism is already enforced by Compile).
type Checker[S, E comparable] struct {
	// Alphabet enumerates every event the environment may deliver. Check
	// applies each event to each reachable state; leave nil to explore only
	// the events that appear in the table (weaker: it cannot find states
	// that deadlock on an unexplored event, but it still covers every
	// declared edge).
	Alphabet []E
	// Invariant, when set, must return nil on every reachable state
	// (an AG — "globally, always" — property).
	Invariant func(S) error
	// Edge, when set, is evaluated on every table edge reachable from the
	// initial state (an AG property over transitions).
	Edge func(from S, event E, to S) error
	// ForbidReachable lists states that must never be reachable.
	ForbidReachable []S
	// AllDeclaredReachable requires every state mentioned by the machine
	// (initial, step endpoints, terminals) to be reachable from Initial —
	// catches states orphaned by later table edits.
	AllDeclaredReachable bool
}

// Check performs exhaustive reachability analysis over the machine and
// returns nil when every configured property holds. The state space of the
// machines this package serves is tiny (states × alphabet events), so the
// exploration is complete, not sampled: if a property holds over all
// reachable states, Check proves it for the modeled machine.
func (c *Compiled[S, E]) Check(chk Checker[S, E]) error {
	// Clone before sorting: the alphabet belongs to the caller, and Check
	// must stay free of side effects on its configuration.
	alphabet := slices.Clone(chk.Alphabet)
	if alphabet == nil {
		alphabet = c.Events()
	}
	slices.SortFunc(alphabet, func(a, b E) int {
		switch {
		case fmt.Sprint(a) < fmt.Sprint(b):
			return -1
		case fmt.Sprint(a) > fmt.Sprint(b):
			return 1
		default:
			return 0
		}
	})

	// Breadth-first reachability with parent records for witness paths.
	parents := map[S]edgeParent[S, E]{}
	seen := map[S]bool{c.machine.Initial: true}
	queue := []S{c.machine.Initial}
	outgoing := map[S]int{}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if chk.Invariant != nil {
			if err := chk.Invariant(cur); err != nil {
				return fmt.Errorf("fsm %q: invariant violated in state %v (witness: %s): %w",
					c.machine.Name, cur, c.witness(parents, cur), err)
			}
		}

		handled := 0
		for _, e := range alphabet {
			to, ok := c.Next(cur, e)
			if !ok {
				continue // rejected event: legal by default (fail-closed)
			}
			handled++
			if chk.Edge != nil {
				if err := chk.Edge(cur, e, to); err != nil {
					return fmt.Errorf("fsm %q: edge property violated on %v --%v--> %v (witness: %s): %w",
						c.machine.Name, cur, e, to, c.witness(parents, cur), err)
				}
			}
			if !seen[to] {
				seen[to] = true
				parents[to] = edgeParent[S, E]{from: cur, event: e}
				queue = append(queue, to)
			}
		}
		outgoing[cur] = handled
	}

	// Structural rules over everything reachable, in a deterministic order
	// (sorted by string form) so a failing machine always reports the same
	// violation first.
	reachable := make([]S, 0, len(seen))
	for st := range seen {
		reachable = append(reachable, st)
	}
	slices.SortFunc(reachable, func(a, b S) int {
		switch {
		case fmt.Sprint(a) < fmt.Sprint(b):
			return -1
		case fmt.Sprint(a) > fmt.Sprint(b):
			return 1
		default:
			return 0
		}
	})
	for _, st := range reachable {
		if outgoing[st] == 0 && !c.terminals[st] {
			return fmt.Errorf("fsm %q: deadlock: reachable state %v has no outgoing transition for any alphabet event and is not declared terminal (witness: %s)",
				c.machine.Name, st, c.witness(parents, st))
		}
		if c.terminals[st] && outgoing[st] > 0 {
			return fmt.Errorf("fsm %q: state %v is declared terminal but has %d outgoing transitions (witness: %s)",
				c.machine.Name, st, outgoing[st], c.witness(parents, st))
		}
	}

	for _, st := range chk.ForbidReachable {
		if seen[st] {
			return fmt.Errorf("fsm %q: forbidden state %v is reachable (witness: %s)",
				c.machine.Name, st, c.witness(parents, st))
		}
	}
	if chk.AllDeclaredReachable {
		declared := map[S]bool{c.machine.Initial: true}
		for _, s := range c.machine.Steps {
			declared[s.From] = true
			declared[s.To] = true
		}
		for st := range c.terminals {
			declared[st] = true
		}
		var unreachable []string
		for st := range declared {
			if !seen[st] {
				unreachable = append(unreachable, fmt.Sprint(st))
			}
		}
		if len(unreachable) > 0 {
			slices.Sort(unreachable)
			return fmt.Errorf("fsm %q: declared states unreachable from %v: %s",
				c.machine.Name, c.machine.Initial, strings.Join(unreachable, ", "))
		}
	}
	return nil
}

// Reachable returns every state reachable from Initial under the given
// alphabet (nil means the table's own events).
func (c *Compiled[S, E]) Reachable(alphabet []E) []S {
	return c.ReachableFrom(c.machine.Initial, alphabet)
}

// ReachableFrom returns every state reachable from an arbitrary start state
// under the given alphabet — the tool for "can the machine still terminate
// from here" checks.
func (c *Compiled[S, E]) ReachableFrom(start S, alphabet []E) []S {
	if alphabet == nil {
		alphabet = c.Events()
	}
	seen := map[S]bool{start: true}
	queue := []S{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range alphabet {
			if to, ok := c.Next(cur, e); ok && !seen[to] {
				seen[to] = true
				queue = append(queue, to)
			}
		}
	}
	out := make([]S, 0, len(seen))
	for st := range seen {
		out = append(out, st)
	}
	return out
}

// edgeParent records how a state was first reached, for witness routes.
type edgeParent[S, E comparable] struct {
	from  S
	event E
}

// witness reconstructs the shortest event route from Initial to st.
func (c *Compiled[S, E]) witness(parents map[S]edgeParent[S, E], st S) string {
	var rev []Step[S, E]
	cur := st
	for {
		p, ok := parents[cur]
		if !ok {
			break // cur is Initial
		}
		rev = append(rev, Step[S, E]{From: p.from, Event: p.event, To: cur})
		cur = p.from
	}
	slices.Reverse(rev)
	return path(c.machine.Initial, rev)
}
