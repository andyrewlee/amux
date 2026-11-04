// liveness.go holds the demotion-hysteresis machine that decides when a
// session's status observes as stopped.

package activity

import (
	"errors"
	"fmt"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/fsm"
)

// DemotionThreshold is the number of consecutive non-live observations
// required before a running session is demoted to stopped. A single
// best-effort AllSessionStates call can miss a live session under load, so
// one miss must not tear down a working background agent.
const DemotionThreshold = 2

// livenessState is the per-session demotion-hysteresis state: how many
// consecutive non-live observations have accumulated, capped at the
// demotion threshold (the state stops counting once demoted).
type livenessState uint8

const (
	livenessLive    livenessState = iota // zero misses
	livenessMiss1                        // one consecutive miss
	livenessStopped                      // demoted (threshold reached)
)

func (s livenessState) String() string {
	switch s {
	case livenessMiss1:
		return "miss1"
	case livenessStopped:
		return "stopped"
	default:
		return "live"
	}
}

// livenessEvent mirrors the two observations the scanner can make.
type livenessEvent uint8

const (
	livenessObserveLive livenessEvent = iota
	livenessObserveMiss
)

func (e livenessEvent) String() string {
	if e == livenessObserveLive {
		return "live"
	}
	return "miss"
}

// livenessMachine is the demotion table: a miss advances toward stopped, a
// live observation resets to live (and revives a demoted session). With
// DemotionThreshold = 2 the state IS the machine state; if the threshold
// ever changes, intermediate states must be added — LivenessCheck asserts
// the threshold still matches the table's shape.
var livenessMachine = fsm.MustCompile(fsm.Machine[livenessState, livenessEvent]{
	Name:    "session-liveness-demotion",
	Initial: livenessLive,
	Steps: []fsm.Step[livenessState, livenessEvent]{
		{From: livenessLive, Event: livenessObserveLive, To: livenessLive},
		{From: livenessLive, Event: livenessObserveMiss, To: livenessMiss1},
		{From: livenessMiss1, Event: livenessObserveLive, To: livenessLive},
		{From: livenessMiss1, Event: livenessObserveMiss, To: livenessStopped},
		{From: livenessStopped, Event: livenessObserveLive, To: livenessLive},
		{From: livenessStopped, Event: livenessObserveMiss, To: livenessStopped},
	},
})

// LivenessCheck formally verifies the demotion machine: stopped is reachable
// only through miss1 (never from a single miss on the initial state), a live
// observation always leaves the machine at live, and stopped is stable under
// further misses.
func LivenessCheck() error {
	if DemotionThreshold != 2 {
		return fmt.Errorf("DemotionThreshold = %d: the liveness table models exactly two misses; add intermediate states", DemotionThreshold)
	}
	return livenessMachine.Check(fsm.Checker[livenessState, livenessEvent]{
		Alphabet:             []livenessEvent{livenessObserveLive, livenessObserveMiss},
		AllDeclaredReachable: true,
		Edge: func(from livenessState, event livenessEvent, to livenessState) error {
			if event == livenessObserveLive && to != livenessLive {
				return errors.New("a live observation must always reset to live")
			}
			if from == livenessLive && event == livenessObserveMiss && to == livenessStopped {
				return fmt.Errorf("a single miss must not demote (threshold is %d)", DemotionThreshold)
			}
			return nil
		},
	})
}

// Liveness is the per-session demotion-hysteresis tracker. It is the
// runtime embodiment of livenessMachine: every observation steps the table,
// so the checked model and the shipped behavior are the same artifact.
type Liveness struct {
	state livenessState
}

// NewLiveness returns a tracker in the live (zero-miss) state.
func NewLiveness() *Liveness { return &Liveness{state: livenessLive} }

// observeLive records a live observation: resets the miss count, reviving a
// demoted session. Production's live reset deletes the tracker wholesale
// (a fresh tracker is live), so this edge is exercised by the machine's own
// tests — it exists to make the table's live axis total and checked.
func (l *Liveness) observeLive() {
	if l == nil {
		return
	}
	if next, ok := livenessMachine.Next(l.state, livenessObserveLive); ok {
		l.state = next
	}
}

// ObserveMiss records a non-live observation and reports whether this miss
// demoted the session (the running→stopped edge). The caller emits its
// stopped message only on that edge.
func (l *Liveness) ObserveMiss() (demoted bool) {
	if l == nil {
		return false
	}
	demoted = l.state != livenessStopped
	if next, ok := livenessMachine.Next(l.state, livenessObserveMiss); ok {
		l.state = next
	}
	return demoted && l.state == livenessStopped
}

// Demoted reports whether the tracker has crossed the demotion threshold.
func (l *Liveness) Demoted() bool {
	return l != nil && l.state == livenessStopped
}

// Misses reports the consecutive-miss count, capped at DemotionThreshold
// (the machine stops counting once demoted). Exposed for observability and
// tests.
func (l *Liveness) Misses() int {
	if l == nil {
		return 0
	}
	switch l.state {
	case livenessMiss1:
		return 1
	case livenessStopped:
		return DemotionThreshold
	default:
		return 0
	}
}

// DemoteNow force-demotes the tracker (the hysteresis-disabled test path).
func (l *Liveness) DemoteNow() {
	if l != nil {
		l.state = livenessStopped
	}
}

// StatusFor projects the tracker's demotion state onto a session status: a
// demoted tracker maps any running-like status to stopped, leaving an
// already-stopped status untouched.
func (l *Liveness) StatusFor(current data.SessionStatus) data.SessionStatus {
	if !l.Demoted() {
		return current
	}
	if next, ok := data.NextSessionStatus(current, data.SessionObservedDead); ok {
		return next
	}
	return current
}
