package center

import (
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/andyrewlee/amux/internal/fsm"
)

// tabActorState is the tab-actor liveness protocol's control state, derived
// (tabActorStateAt) from the atomics the hot path already reads — the flags
// are the extended state, kept exactly as they are because the protocol is
// cross-goroutine (actor goroutine heartbeats vs Update-goroutine reads).
// The state name folds in the two latches: Stalled means the once-per-episode
// warn latch is set; the Armed suffix means the redraw coalescing latch is
// set. The derivation is for tests and verification only — the per-keystroke
// read path (isTabActorReady) keeps its raw atomic loads.
type tabActorState uint8

const (
	// tabActorDetached: never attached (no heartbeat stamp yet).
	tabActorDetached tabActorState = iota
	// tabActorDetachedArmed: detached with a redraw request in flight.
	tabActorDetachedArmed
	// tabActorLive: attached or recovered; heartbeat fresh.
	tabActorLive
	// tabActorLiveArmed: live with a redraw request in flight.
	tabActorLiveArmed
	// tabActorStalled: heartbeat stale — demoted to direct-send with the
	// warn latch set (the stall logged exactly once this episode).
	tabActorStalled
	// tabActorStalledArmed: stalled with a redraw request in flight.
	tabActorStalledArmed
	// tabActorDemotedFresh: the defensive anomaly — ready cleared but the
	// heartbeat stamp is fresh. The real methods never produce it
	// single-threaded; it exists for the race where a stale-read demote
	// interleaves a heartbeat (see tabActorStateAt). The next heartbeat
	// repairs it — never a demote, never a second warn.
	tabActorDemotedFresh
	// tabActorDemotedFreshArmed: the same anomaly with a redraw in flight.
	tabActorDemotedFreshArmed
)

func (s tabActorState) String() string {
	armed := s == tabActorDetachedArmed || s == tabActorLiveArmed ||
		s == tabActorStalledArmed || s == tabActorDemotedFreshArmed
	var phase string
	switch s {
	case tabActorDetached, tabActorDetachedArmed:
		phase = "detached"
	case tabActorLive, tabActorLiveArmed:
		phase = "live"
	case tabActorStalled, tabActorStalledArmed:
		phase = "stalled"
	default:
		phase = "demoted-fresh"
	}
	if armed {
		return phase + "+redraw-armed"
	}
	return phase
}

// tabActorEvent is the liveness protocol's alphabet. The timeout-demote
// event models the isTabActorReady read that observes a stale heartbeat —
// demotion is lazy, materialized on the read path.
type tabActorEvent uint8

const (
	// tabActorEvtHeartbeat: noteTabActorHeartbeat on the actor goroutine.
	// From stalled it is the recovery edge (clears the episode latch, logs
	// once).
	tabActorEvtHeartbeat tabActorEvent = iota
	// tabActorEvtAttach: setTabActorReady — a fresh (re)attach resets the
	// episode from anywhere, silently (no recovery log).
	tabActorEvtAttach
	// tabActorEvtTimeoutDemote: an isTabActorReady read observing a stale
	// heartbeat: demotes to direct-send and arms the once-per-episode warn
	// latch. From stalled it is an idempotent self-loop — the warn fired
	// already, and only heartbeat or attach may end the episode.
	tabActorEvtTimeoutDemote
	// tabActorEvtStaleReadClobber: the race that produces the demoted-fresh
	// anomaly — an isTabActorReady call that loaded a stale heartbeat
	// interleaves a heartbeat (beat refreshed, ready set) and then stores
	// ready=0 and arms the warn latch over the fresh beat. Single-threaded
	// code never produces it; the table models it so the anomaly state is
	// reachable and its repair (heartbeat, never demote) is verified.
	tabActorEvtStaleReadClobber
	// tabActorEvtRedrawRequest: requestTabActorRedraw with the sink
	// accepting: arms the coalescing latch and delivers one message.
	// Re-requesting while armed is a no-op self-loop (the CAS fails).
	tabActorEvtRedrawRequest
	// tabActorEvtRedrawRequestRejected: requestTabActorRedraw whose sink
	// try declined — the just-armed latch rolls back. Only legal from an
	// unarmed state: an armed request never reaches the sink.
	tabActorEvtRedrawRequestRejected
	// tabActorEvtRedrawConsume: the Update handler's
	// clearTabActorRedrawPending upon receiving tabActorRedraw. Legal only
	// while armed.
	tabActorEvtRedrawConsume
)

func (e tabActorEvent) String() string {
	switch e {
	case tabActorEvtHeartbeat:
		return "heartbeat"
	case tabActorEvtAttach:
		return "attach"
	case tabActorEvtTimeoutDemote:
		return "timeout-demote"
	case tabActorEvtStaleReadClobber:
		return "stale-read-clobber"
	case tabActorEvtRedrawRequest:
		return "redraw-request"
	case tabActorEvtRedrawRequestRejected:
		return "redraw-request-rejected"
	default:
		return "redraw-consume"
	}
}

// tabActorSteps expands the per-phase protocol over the redraw latch: every
// liveness event preserves the latch, every redraw event preserves the
// phase. Writing the product out literally keeps the table auditable
// against model.go's methods one row at a time.
func tabActorSteps() []fsm.Step[tabActorState, tabActorEvent] {
	// Heartbeat and attach both restore liveness from anywhere, preserving
	// the redraw latch.
	live := []struct{ from, to tabActorState }{
		{tabActorDetached, tabActorLive},
		{tabActorDetachedArmed, tabActorLiveArmed},
		{tabActorLive, tabActorLive},
		{tabActorLiveArmed, tabActorLiveArmed},
		{tabActorStalled, tabActorLive},
		{tabActorStalledArmed, tabActorLiveArmed},
		{tabActorDemotedFresh, tabActorLive},
		{tabActorDemotedFreshArmed, tabActorLiveArmed},
	}
	// Only a live actor with a gone-stale heartbeat demotes (the warn
	// edge); an already-stalled one re-reads idempotently. Detached and
	// demoted-fresh never demote — the read returns early without touching
	// the latch — so those pairs are absent and the table rejects them.
	demote := []struct{ from, to tabActorState }{
		{tabActorLive, tabActorStalled},
		{tabActorLiveArmed, tabActorStalledArmed},
		{tabActorStalled, tabActorStalled},
		{tabActorStalledArmed, tabActorStalledArmed},
	}
	// The stale-read race knocks a live actor into the demoted-fresh
	// anomaly (the clobbering stores in isTabActorReady's window).
	clobber := []struct{ from, to tabActorState }{
		{tabActorLive, tabActorDemotedFresh},
		{tabActorLiveArmed, tabActorDemotedFreshArmed},
	}
	unarmed := []tabActorState{
		tabActorDetached, tabActorLive, tabActorStalled, tabActorDemotedFresh,
	}
	armed := []tabActorState{
		tabActorDetachedArmed, tabActorLiveArmed, tabActorStalledArmed, tabActorDemotedFreshArmed,
	}
	var steps []fsm.Step[tabActorState, tabActorEvent]
	for _, s := range live {
		steps = append(steps,
			fsm.Step[tabActorState, tabActorEvent]{From: s.from, Event: tabActorEvtHeartbeat, To: s.to},
			fsm.Step[tabActorState, tabActorEvent]{From: s.from, Event: tabActorEvtAttach, To: s.to},
		)
	}
	for _, s := range demote {
		steps = append(steps, fsm.Step[tabActorState, tabActorEvent]{From: s.from, Event: tabActorEvtTimeoutDemote, To: s.to})
	}
	for _, s := range clobber {
		steps = append(steps, fsm.Step[tabActorState, tabActorEvent]{From: s.from, Event: tabActorEvtStaleReadClobber, To: s.to})
	}
	for _, s := range unarmed {
		// Request arms the latch; a declined sink rolls it straight back.
		armedOf := s + 1
		steps = append(steps,
			fsm.Step[tabActorState, tabActorEvent]{From: s, Event: tabActorEvtRedrawRequest, To: armedOf},
			fsm.Step[tabActorState, tabActorEvent]{From: s, Event: tabActorEvtRedrawRequestRejected, To: s},
		)
	}
	for _, s := range armed {
		// Re-request while armed coalesces (self-loop, no second message);
		// consuming disarms. A rejected sink never happens from armed.
		unarmedOf := s - 1
		steps = append(steps,
			fsm.Step[tabActorState, tabActorEvent]{From: s, Event: tabActorEvtRedrawRequest, To: s},
			fsm.Step[tabActorState, tabActorEvent]{From: s, Event: tabActorEvtRedrawConsume, To: unarmedOf},
		)
	}
	return steps
}

// tabActorMachine is the tab-actor liveness / health protocol over the
// atomics in model.go (tabActorReady, tabActorHeartbeat,
// tabActorStalled, tabActorRedrawPending).
var tabActorMachine = fsm.MustCompile(fsm.Machine[tabActorState, tabActorEvent]{
	Name:    "tab-actor-liveness",
	Initial: tabActorDetached,
	Steps:   tabActorSteps(),
})

// tabActorStateAt derives the protocol state from the real fields at a
// caller-supplied instant (the stale/fresh cut needs a clock; tests pass
// time.Now()). A stale heartbeat maps to Stalled even before the lazy
// demote store lands, because isTabActorReady already answers false for a
// stale beat — the derivation matches observable behavior, and the
// timeout-demote event is the read that materializes ready=0 plus the warn
// latch.
func (m *Model) tabActorStateAt(now time.Time) tabActorState {
	armed := atomic.LoadUint32(&m.tabActorRedrawPending) == 1
	ready := atomic.LoadUint32(&m.tabActorReady) == 1
	beat := atomic.LoadInt64(&m.tabActorHeartbeat)
	withLatch := func(phase tabActorState) tabActorState {
		if armed {
			return phase + 1
		}
		return phase
	}
	switch {
	case beat == 0:
		if ready {
			// Unreachable via the real methods (setTabActorReady always
			// stamps first); the runtime treats it as not-ready, so it
			// derives to the repairable anomaly state.
			return withLatch(tabActorDemotedFresh)
		}
		return withLatch(tabActorDetached)
	case now.Sub(time.Unix(0, beat)) > tabActorStallTimeout:
		return withLatch(tabActorStalled)
	case ready:
		return withLatch(tabActorLive)
	default:
		return withLatch(tabActorDemotedFresh)
	}
}

// tabActorCheck formally verifies the liveness protocol table: demotion is
// one-way per episode (only heartbeat or attach may leave the stalled
// family), the demote read is idempotent once stalled (the warn fires on
// the Live→Stalled edge only — the once-per-episode guarantee), and the
// redraw latch is a coalescing latch (a re-request while armed changes
// nothing, consume is legal only while armed).
// The cousin latch — pendingActivityTags + activityTagFlushPending
// (activity_tag_flush.go) — deliberately gets no table of its own. Unlike
// the tab-actor flags, whose combinations carry protocol meaning (episode
// latch vs redraw latch vs readiness), the flush pair is a pure bijection
// latch: under activityTagsMu the flag and the set are only ever mutated
// together (markActivityTagForFlush sets flag=true while inserting;
// drainActivityTags clears flag while emptying), so
// activityTagFlushPending ⇔ len(pendingActivityTags) > 0 at every mutex
// boundary and there is exactly one arming event (mark) and one disarming
// event (drain) with no interleaving that could desynchronize them. A
// one-state-machine-shaped table would restate that bijection without
// excluding anything; TestTabActorModelActivityFlushLatchBijection below
// pins it differentially instead.

func tabActorCheck() error {
	stalled := func(s tabActorState) bool {
		return s == tabActorStalled || s == tabActorStalledArmed ||
			s == tabActorDemotedFresh || s == tabActorDemotedFreshArmed
	}
	return tabActorMachine.Check(fsm.Checker[tabActorState, tabActorEvent]{
		Alphabet: []tabActorEvent{
			tabActorEvtHeartbeat, tabActorEvtAttach, tabActorEvtTimeoutDemote,
			tabActorEvtStaleReadClobber, tabActorEvtRedrawRequest,
			tabActorEvtRedrawRequestRejected, tabActorEvtRedrawConsume,
		},
		AllDeclaredReachable: true,
		Edge: func(from tabActorState, event tabActorEvent, to tabActorState) error {
			switch event {
			case tabActorEvtHeartbeat, tabActorEvtAttach:
				if to != tabActorLive && to != tabActorLiveArmed {
					return fmt.Errorf("a %s must always restore liveness, got %v", event, to)
				}
			case tabActorEvtTimeoutDemote:
				if !stalled(to) {
					return fmt.Errorf("a demote read may never restore liveness (got %v)", to)
				}
				if stalled(from) && to != from {
					return errors.New("a demote read while stalled must be an idempotent self-loop")
				}
			case tabActorEvtStaleReadClobber:
				if to != tabActorDemotedFresh && to != tabActorDemotedFreshArmed {
					return fmt.Errorf("the stale-read race may only produce the demoted-fresh anomaly (got %v)", to)
				}
			case tabActorEvtRedrawRequest:
				if from == to {
					return nil // the armed coalescing self-loop
				}
				if to != from+1 {
					return fmt.Errorf("an unarmed request must arm exactly once (got %v)", to)
				}
			case tabActorEvtRedrawRequestRejected:
				if to != from || from%2 != 0 {
					return fmt.Errorf("a declined request must leave an unarmed state untouched (got %v)", to)
				}
			case tabActorEvtRedrawConsume:
				if from%2 != 1 || to != from-1 {
					return fmt.Errorf("consume is legal only while armed and disarms exactly (got %v)", to)
				}
			}
			return nil
		},
	})
}
