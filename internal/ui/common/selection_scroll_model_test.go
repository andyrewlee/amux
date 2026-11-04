package common

import (
	"testing"
)

// TestSelectionScrollModelCheck runs the formal verification of the
// drag-selection auto-scroll machine (invariants documented on
// SelectionScrollCheck).
func TestSelectionScrollModelCheck(t *testing.T) {
	if err := SelectionScrollCheck(); err != nil {
		t.Fatal(err)
	}
}

// selScrollDriver steps the real SelectionScrollState (shipped struct, real
// methods) and the fsm table in lockstep. Every apply call drives the real
// method for the event regardless of whether the table declares an edge, so
// rejections are exercised too: a rejected event must leave the real
// control state untouched, and an accepted one must land exactly where the
// table predicts.
type selScrollDriver struct {
	t    *testing.T
	real SelectionScrollState
}

func (d *selScrollDriver) apply(model selScrollState, e selScrollEvent) selScrollState {
	d.t.Helper()
	before := d.real.ctlState()
	if before != model {
		d.t.Fatalf("lockstep desynced before %v: model=%v real=%v", e, model, before)
	}
	to, ok := selScrollMachine.Next(model, e)

	// Snapshot the epoch so we can pin Gen/TickSeq discipline, which the
	// table keeps as extended state.
	oldGen, oldSeq := d.real.Gen, d.real.TickSeq

	switch e {
	case evtArmUp:
		d.real.SetDirection(-3, 10) // termY < 0 → scroll up into history
	case evtArmDown:
		d.real.SetDirection(14, 10) // termY >= termHeight → scroll down
	case evtDirZero:
		d.real.SetDirection(5, 10) // inside the viewport
	case evtNeedsTick:
		want := selScrollDir(before) != 0
		got, gen, seq := d.real.NeedsTick()
		if got != want {
			d.t.Fatalf("NeedsTick from %v: got %v, want %v", before, got, want)
		}
		if !got {
			break
		}
		if selScrollArmed(before) {
			// The self-healing re-request: same generation, same expected
			// sequence. This is exactly the behavior SCROLLING.md warns not
			// to "optimize" away.
			if gen != oldGen || seq != oldSeq {
				d.t.Fatalf("NeedsTick re-request from %v: got (gen=%d seq=%d), want unchanged (gen=%d seq=%d)",
					before, gen, seq, oldGen, oldSeq)
			}
		} else {
			// Arming from pending restarts the epoch.
			if gen != oldGen+1 || seq != 1 {
				d.t.Fatalf("NeedsTick arm from %v: got (gen=%d seq=%d), want (gen=%d seq=1)",
					before, gen, seq, oldGen+1)
			}
		}
	case evtTickCurrent:
		d.checkTick(before, oldSeq, d.real.HandleTick(d.real.Gen, d.real.TickSeq), true)
	case evtTickStale:
		got := d.real.HandleTick(oldGen+7, d.real.TickSeq)
		d.checkTick(before, oldSeq, got, false)
		if !got && selScrollArmed(before) && selScrollDir(before) != 0 {
			// Dropped without stopping the loop: epoch fields untouched.
			if d.real.Gen != oldGen || d.real.TickSeq != oldSeq {
				d.t.Fatalf("HandleTick(stale gen) from %v mutated epoch: (gen=%d seq=%d), want (gen=%d seq=%d)",
					before, d.real.Gen, d.real.TickSeq, oldGen, oldSeq)
			}
		}
	case evtReset:
		d.real.Reset()
		if d.real.Gen != oldGen+1 {
			d.t.Fatalf("Reset from %v: Gen %d, want %d (must invalidate in-flight ticks)", before, d.real.Gen, oldGen+1)
		}
	default:
		d.t.Fatalf("unknown event %v", e)
	}

	after := d.real.ctlState()
	switch {
	case ok && after != to:
		d.t.Fatalf("event %v from %v: table predicts %v, real struct landed %v", e, before, to, after)
	case !ok && after != before:
		d.t.Fatalf("rejected event %v from %v: real struct mutated to %v", e, before, after)
	}
	if ok {
		return to
	}
	return before
}

// checkTick asserts a HandleTick result: only an armed, directed state may
// continue the loop (and only on a current-epoch tick, which must also
// advance TickSeq).
func (d *selScrollDriver) checkTick(before selScrollState, oldSeq uint64, got, want bool) {
	d.t.Helper()
	if selScrollArmed(before) && selScrollDir(before) != 0 {
		if got != want {
			d.t.Fatalf("HandleTick from %v: got %v, want %v", before, got, want)
		}
		if want && d.real.TickSeq != oldSeq+1 {
			d.t.Fatalf("HandleTick(current epoch) from %v: TickSeq %d, want %d", before, d.real.TickSeq, oldSeq+1)
		}
		return
	}
	if got {
		d.t.Fatalf("HandleTick from %v: got true, want false", before)
	}
}

// TestSelectionScrollLockstepExhaustive drives the real struct and the table
// through every event sequence of length 0..4 over the full alphabet and
// asserts they agree at every step, including rejection behavior and the
// Gen/TickSeq epoch discipline the table keeps as extended state.
func TestSelectionScrollLockstepExhaustive(t *testing.T) {
	var walk func(seq []selScrollEvent)
	walk = func(seq []selScrollEvent) {
		d := &selScrollDriver{t: t}
		model := stIdle // d.real is the zero value = the machine's initial state
		for _, e := range seq {
			model = d.apply(model, e)
		}
		if len(seq) == 4 {
			return
		}
		for _, e := range selScrollAlphabet {
			walk(append(seq, e))
		}
	}
	walk(nil)
}

// TestSelectionScrollDroppedTickSelfHeal pins the SCROLLING.md warning: a
// drag motion while a loop is armed must re-request the SAME generation and
// sequence, so a tick message shed by a bounded queue pauses auto-scroll
// only until the next mouse motion. NeedsTick must stay re-entrant, not
// start-only-when-not-active.
func TestSelectionScrollDroppedTickSelfHeal(t *testing.T) {
	var s SelectionScrollState
	s.SetDirection(-3, 10)
	_, gen1, seq1 := s.NeedsTick()
	// One tick fires; the next motion re-requests the same (gen, seq).
	if ok := s.HandleTick(gen1, seq1); !ok {
		t.Fatal("first current tick must continue the loop")
	}
	_, gen2, seq2 := s.NeedsTick()
	if gen2 != gen1 || seq2 != seq1+1 {
		t.Fatalf("re-request after a tick: got (gen=%d seq=%d), want (gen=%d seq=%d)",
			gen2, seq2, gen1, seq1+1)
	}
	// The duplicate of an already-consumed sequence is rejected without
	// stopping the loop.
	if ok := s.HandleTick(gen2, seq2); !ok {
		t.Fatal("current tick must continue the loop")
	}
	if ok := s.HandleTick(gen2, seq2); ok {
		t.Fatal("duplicate sequence must be rejected")
	}
	if ok := s.HandleTick(gen2, seq2+1); !ok {
		t.Fatal("duplicate must not stop the loop; the next sequence still validates")
	}
}

// TestSelectionScrollStaleEpochAfterReset pins the epoch fencing across a
// drag boundary: Reset invalidates in-flight ticks, and the next drag arms a
// fresh generation, so a late tick from the previous drag is dropped.
func TestSelectionScrollStaleEpochAfterReset(t *testing.T) {
	var s SelectionScrollState
	s.SetDirection(-3, 10)
	_, oldGen, firstSeq := s.NeedsTick()
	s.Reset()
	s.SetDirection(-3, 10)
	_, newGen, _ := s.NeedsTick()
	if newGen <= oldGen {
		t.Fatalf("re-arm after Reset must bump the epoch: old gen=%d new gen=%d", oldGen, newGen)
	}
	if ok := s.HandleTick(oldGen, firstSeq); ok {
		t.Fatal("tick from the pre-Reset epoch must be dropped")
	}
	if ok := s.HandleTick(newGen, firstSeq); !ok {
		t.Fatal("tick from the current epoch must validate after the stale one was dropped")
	}
}

// TestSelectionScrollDirectionSwitchKeepsEpoch pins the drift between the
// older prose claim ("direction changes restart the epoch") and the code: a
// direction switch while armed does NOT bump Gen. It is safe by design —
// SelectionScrollTickStep reads ScrollDir live, so current-epoch ticks
// simply scroll the new direction. The epoch fences drags, not directions.
func TestSelectionScrollDirectionSwitchKeepsEpoch(t *testing.T) {
	var s SelectionScrollState
	s.SetDirection(-3, 10) // dragging past the top edge
	ok, gen, seq := s.NeedsTick()
	if !ok {
		t.Fatal("arming past the top edge must request a tick")
	}
	if ok := s.HandleTick(gen, seq); !ok {
		t.Fatal("current tick must validate")
	}
	seq = s.TickSeq
	s.SetDirection(14, 10) // still dragging, now past the bottom edge
	if s.Active != true || s.Gen != gen {
		t.Fatalf("direction switch while armed must keep the epoch: active=%v gen=%d (want gen=%d)", s.Active, s.Gen, gen)
	}
	if s.ScrollDir != -1 {
		t.Fatalf("direction switch must update ScrollDir, got %d", s.ScrollDir)
	}
	// The loop continues seamlessly in the new direction.
	ok2, gen2, seq2 := s.NeedsTick()
	if !ok2 || gen2 != gen || seq2 != seq {
		t.Fatalf("re-request after direction switch: got (gen=%d seq=%d), want (gen=%d seq=%d)", gen2, seq2, gen, seq)
	}
	if ok := s.HandleTick(gen2, seq2); !ok {
		t.Fatal("current-epoch tick must still validate after a direction switch")
	}
	if s.ScrollDir != -1 {
		t.Fatalf("tick step must scroll the live direction, got %d", s.ScrollDir)
	}
}

// TestSelectionScrollClearedDirectionEndsLoop pins the stArmedNone
// self-heal: once the pointer returns inside the viewport, any tick — even a
// stale one — ends the loop, and NeedsTick refuses to re-request while the
// direction is clear.
func TestSelectionScrollClearedDirectionEndsLoop(t *testing.T) {
	var s SelectionScrollState
	s.SetDirection(-3, 10)
	s.NeedsTick()
	s.SetDirection(5, 10) // pointer back inside the viewport
	if !s.Active {
		t.Fatal("loop must still be armed (waiting for the next tick)")
	}
	if ok, _, _ := s.NeedsTick(); ok {
		t.Fatal("NeedsTick must not fire with the direction cleared")
	}
	// Even a stale-generation tick ends the loop: HandleTick checks the
	// direction before the epoch.
	if ok := s.HandleTick(s.Gen+99, s.TickSeq); ok {
		t.Fatal("tick with cleared direction must not continue the loop")
	}
	if s.Active {
		t.Fatal("tick with cleared direction must deactivate the loop")
	}
	if s.ctlState() != stIdle {
		t.Fatalf("post-deactivation control state: got %v, want idle", s.ctlState())
	}
	// And the next drag past an edge re-arms with a fresh epoch.
	s.SetDirection(-3, 10)
	if ok, gen, _ := s.NeedsTick(); !ok || gen != s.Gen {
		t.Fatalf("re-arm after deactivation must bump the epoch: ok=%v gen=%d", ok, s.Gen)
	}
}

// TestSelectionScrollScenarioNoWedge drives each reachable state through
// Reset and asserts the real struct lands idle — the runtime half of the
// checker's ReachableFrom no-wedge proof.
func TestSelectionScrollScenarioNoWedge(t *testing.T) {
	// Enumerate states by constructing representatives.
	reps := map[selScrollState]func() *SelectionScrollState{
		stIdle: func() *SelectionScrollState { return &SelectionScrollState{} },
		stPendingUp: func() *SelectionScrollState {
			var s SelectionScrollState
			s.SetDirection(-1, 10)
			return &s
		},
		stPendingDown: func() *SelectionScrollState {
			var s SelectionScrollState
			s.SetDirection(11, 10)
			return &s
		},
		stArmedUp: func() *SelectionScrollState {
			var s SelectionScrollState
			s.SetDirection(-1, 10)
			s.NeedsTick()
			return &s
		},
		stArmedDown: func() *SelectionScrollState {
			var s SelectionScrollState
			s.SetDirection(11, 10)
			s.NeedsTick()
			return &s
		},
		stArmedNone: func() *SelectionScrollState {
			var s SelectionScrollState
			s.SetDirection(-1, 10)
			s.NeedsTick()
			s.SetDirection(4, 10)
			return &s
		},
	}
	for st, make := range reps {
		s := make()
		if got := s.ctlState(); got != st {
			t.Fatalf("representative for %v derived %v", st, got)
		}
		s.Reset()
		if got := s.ctlState(); got != stIdle {
			t.Fatalf("reset from %v landed %v, want idle", st, got)
		}
	}
}
