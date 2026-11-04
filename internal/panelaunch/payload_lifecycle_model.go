package panelaunch

import (
	"fmt"

	"github.com/andyrewlee/amux/internal/fsm"
)

// The pane-launch payload lifecycle as a machine — the verified shape of
// the decisions in prepare.go, invoke_unix.go, and cleanup.go. State lives
// entirely in filesystem sentinels per attempt directory, so the states
// below are exactly the classifications an observer (the sweep, a helper,
// a debugger) can derive from disk; the events are the lifecycle actions
// that move an attempt between them.
type payloadState uint8

const (
	// payloadAbsent: nothing on disk — the machine's start, and also the
	// disk shape after any gone terminal (the classifier cannot tell who
	// removed an attempt, only that it is gone).
	payloadAbsent payloadState = iota
	// payloadPartial: attempt directory exists, payload file does not — a
	// writer crashed between mkdir and the payload landing.
	payloadPartial
	// payloadPrepared: directory plus payload, both owned, within the
	// launch budget — consumable and untouchable by the sweep.
	payloadPrepared
	// payloadExpired: owned attempt past the budget (payload optional). The
	// payload is no longer usable, but unlink-before-decode means a
	// consumer still removes it, and the sweep may now act.
	payloadExpired
	// payloadForeign: ownership unproven — a foreign-owned or mistyped dir
	// or payload (the sweep's ownership test is type + euid ownership, not
	// mode). An expired owned attempt whose rmdir fails on unrelated
	// children is NOT this state: the sweep unlinks its payload and leaves
	// the dir, which the classifier still sees as a partial attempt.
	// Fail-closed: nothing in this package may touch a foreign attempt.
	payloadForeign
	// payloadConsumed: removed by the pane's consumer (one-shot).
	payloadConsumed
	// payloadDiscardedInBand: removed by a discard witness that proved the
	// attempt unused.
	payloadDiscardedInBand
	// payloadSwept: removed by the expiry sweep.
	payloadSwept
)

func (s payloadState) String() string {
	switch s {
	case payloadAbsent:
		return "absent"
	case payloadPartial:
		return "partial"
	case payloadPrepared:
		return "prepared"
	case payloadExpired:
		return "expired"
	case payloadForeign:
		return "foreign"
	case payloadConsumed:
		return "consumed"
	case payloadDiscardedInBand:
		return "discarded-in-band"
	default:
		return "swept"
	}
}

type payloadEvent uint8

const (
	// evtPrepare: prepare.go Prepare — dir plus payload written atomically.
	evtPrepare payloadEvent = iota
	// evtCrashPartial: a writer died between createAttemptDir and
	// writePayload, leaving the directory without a payload.
	evtCrashPartial
	// evtConsume: invoke_unix.go runPayload — validate, prove ownership,
	// read, unlink before decode, exec. One-shot.
	evtConsume
	// evtDiscardUnused: invoke_unix.go discardPayload and prepare.go
	// Discard; tmux side — internal/tmux/detached_session.go's non-timeout
	// failure branches (`ctx.Err() == nil`) and pane_launch.go's
	// present-session / lost-race in-band discard branches.
	evtDiscardUnused
	// evtDiscardForeignWitness: pane_launch.go's lost-race branch where the
	// winning pane's pane_start_command proves our pane never ran.
	evtDiscardForeignWitness
	// evtBudgetElapse: the five-minute window closes (prepare.go stamps
	// ExpiresUnix); tmux side — internal/tmux/detached_session.go's timeout
	// branch leaves the attempt to this event and the sweep.
	evtBudgetElapse
	// evtSweepExpire: cleanup.go sweepAttempt — owned, past budget,
	// payload unlinked, empty-only rmdir, never recursive.
	evtSweepExpire
	// evtForeignClaim: external interference reclassifies a live attempt
	// off-limits — chown, mode change, symlink swap, or a foreign child
	// appearing. No code site performs this; the sweep and helpers simply
	// discover the classification and refuse.
	evtForeignClaim
)

func (e payloadEvent) String() string {
	switch e {
	case evtPrepare:
		return "prepare"
	case evtCrashPartial:
		return "crash-partial"
	case evtConsume:
		return "consume"
	case evtDiscardUnused:
		return "discard-unused"
	case evtDiscardForeignWitness:
		return "discard-foreign"
	case evtBudgetElapse:
		return "budget-elapse"
	case evtSweepExpire:
		return "sweep-expire"
	default:
		return "foreign-claim"
	}
}

var payloadAlphabet = []payloadEvent{
	evtPrepare, evtCrashPartial,
	evtConsume, evtDiscardUnused, evtDiscardForeignWitness,
	evtBudgetElapse, evtSweepExpire, evtForeignClaim,
}

// payloadMachine is the attempt lifecycle. The safety-critical shape:
// consume is one-shot (payloadConsumed has no outgoing edge — the
// unlink-before-decode discipline), sweep-expire is legal only from
// payloadExpired (fresh attempts are untouchable; the budget edge exists
// precisely so the sweep cannot shortcut it), and payloadForeign is an
// absorbing terminal (fail-closed ownership — no lifecycle action may
// enter it except the claim itself, and nothing may leave it).
var payloadMachine = fsm.MustCompile(fsm.Machine[payloadState, payloadEvent]{
	Name:    "pane-launch-payload-lifecycle",
	Initial: payloadAbsent,
	Steps: []fsm.Step[payloadState, payloadEvent]{
		{From: payloadAbsent, Event: evtPrepare, To: payloadPrepared},
		{From: payloadAbsent, Event: evtCrashPartial, To: payloadPartial},
		// A consumer may still eat an expired payload: expiry bounds
		// usability (decode rejects it), not consumption — the unlink
		// happens before decode, so the attempt is still destroyed once.
		{From: payloadPrepared, Event: evtConsume, To: payloadConsumed},
		{From: payloadExpired, Event: evtConsume, To: payloadConsumed},
		// In-band discards never check age: a witness that proves the
		// attempt unused beats the budget.
		{From: payloadPrepared, Event: evtDiscardUnused, To: payloadDiscardedInBand},
		{From: payloadPartial, Event: evtDiscardUnused, To: payloadDiscardedInBand},
		{From: payloadExpired, Event: evtDiscardUnused, To: payloadDiscardedInBand},
		{From: payloadPrepared, Event: evtDiscardForeignWitness, To: payloadDiscardedInBand},
		{From: payloadExpired, Event: evtDiscardForeignWitness, To: payloadDiscardedInBand},
		// The budget is the only door into the sweepable class, from both
		// a full and a partial (dir-only) attempt.
		{From: payloadPrepared, Event: evtBudgetElapse, To: payloadExpired},
		{From: payloadPartial, Event: evtBudgetElapse, To: payloadExpired},
		{From: payloadExpired, Event: evtSweepExpire, To: payloadSwept},
		// External interference moves any live attempt off-limits.
		{From: payloadPrepared, Event: evtForeignClaim, To: payloadForeign},
		{From: payloadPartial, Event: evtForeignClaim, To: payloadForeign},
		{From: payloadExpired, Event: evtForeignClaim, To: payloadForeign},
	},
	// payloadForeign is terminal by design: ownership was never proven, so
	// no code path in this package may remove it — cleanup is deliberately
	// out of band. The other terminals are the gone variants.
	Terminals: []payloadState{
		payloadConsumed, payloadDiscardedInBand, payloadSwept, payloadForeign,
	},
})

// attemptFacts are the observable inputs to the attempt classification —
// exactly what sweepAttempt looks at, stated as booleans so the decision
// can be swept exhaustively without a filesystem. Note the asymmetry with
// the in-band paths: checkAttemptDir/removeOwnedPayload additionally enforce
// 0700/0600 modes at consume and discard time, but the sweep's ownership
// test is type + euid only, so the modeled — and swept — predicates are the
// sweep's looser ones; modeling the stricter in-band gates here would
// verify a policy the sweep does not enforce.
type attemptFacts struct {
	dirPresent     bool // attempt directory exists
	dirOwned       bool // dir passes sweepAttempt's gate: real dir (not a symlink), euid-owned
	payloadPresent bool // payload file exists
	payloadOwned   bool // payload passes sweepAttempt's gate: regular file, euid-owned
	withinBudget   bool // latest mtime (dir or payload) is within launchBudget of now
}

// attemptClass is the disk-shape classification of one attempt.
type attemptClass uint8

const (
	classAbsent attemptClass = iota
	classPartialFresh
	classPartialExpired
	classPreparedFresh
	classPreparedExpired
	classForeign
)

func (c attemptClass) String() string {
	switch c {
	case classAbsent:
		return "absent"
	case classPartialFresh:
		return "partial-fresh"
	case classPartialExpired:
		return "partial-expired"
	case classPreparedFresh:
		return "prepared-fresh"
	case classPreparedExpired:
		return "prepared-expired"
	default:
		return "foreign"
	}
}

// machineState maps a classification onto the lifecycle machine's states:
// the machine merges the fresh/expired payload-less shapes into partial and
// expired because their transitions are identical.
func (c attemptClass) machineState() payloadState {
	switch c {
	case classAbsent:
		return payloadAbsent
	case classPartialFresh:
		return payloadPartial
	case classPartialExpired, classPreparedExpired:
		return payloadExpired
	case classPreparedFresh:
		return payloadPrepared
	default:
		return payloadForeign
	}
}

// classifyAttempt derives the attempt class from observable facts,
// mirroring sweepAttempt's decision inputs: absent wins, then ownership
// (fail closed on any unproven artifact, payload included), then payload
// presence, then age.
func classifyAttempt(f attemptFacts) attemptClass {
	if !f.dirPresent {
		return classAbsent
	}
	if !f.dirOwned || (f.payloadPresent && !f.payloadOwned) {
		return classForeign
	}
	if f.payloadPresent {
		if f.withinBudget {
			return classPreparedFresh
		}
		return classPreparedExpired
	}
	if f.withinBudget {
		return classPartialFresh
	}
	return classPartialExpired
}

// sweepAction is what SweepExpired may do to an attempt class.
type sweepAction uint8

const (
	// actionNone: nothing on disk — nothing to do.
	actionNone sweepAction = iota
	// actionRefuse: the sweep must touch nothing — off-limits or fresh.
	actionRefuse
	// actionRemove: unlink the owned payload (if present) and rmdir the
	// attempt; never recursive, so unrelated children survive.
	actionRemove
)

func (a sweepAction) String() string {
	switch a {
	case actionNone:
		return "none"
	case actionRefuse:
		return "refuse"
	default:
		return "remove"
	}
}

// sweepActionForClass is the sweep's per-class intent.
func sweepActionForClass(c attemptClass) sweepAction {
	switch c {
	case classAbsent:
		return actionNone
	case classForeign, classPartialFresh, classPreparedFresh:
		return actionRefuse
	default:
		return actionRemove
	}
}

// PaneLaunchLifecycleCheck formally verifies the payload lifecycle: the
// machine is well-formed (alphabet complete, every state reachable,
// terminals without outgoing edges), consume is one-shot, sweep-expire is
// legal only from expired, foreign is enterable only by the claim event
// and touches nothing, every non-foreign state can still reach a gone
// terminal (no eternal litter), and the classification is exact over its
// whole input space. Runs in panelaunch's tests.
func PaneLaunchLifecycleCheck() error {
	if err := payloadMachine.Check(fsm.Checker[payloadState, payloadEvent]{
		Alphabet:             payloadAlphabet,
		AllDeclaredReachable: true,
		Edge:                 payloadDisciplineEdge,
	}); err != nil {
		return err
	}
	if err := payloadAlphabetComplete(); err != nil {
		return err
	}
	if err := payloadAlwaysReachableGone(); err != nil {
		return err
	}
	return payloadClassifySweep()
}

// payloadDisciplineEdge pins the three load-bearing rules as edge
// properties: consume never repeats, the sweep only ever acts on expired
// attempts, and foreign is never the target of a lifecycle action.
func payloadDisciplineEdge(from payloadState, event payloadEvent, to payloadState) error {
	if from == payloadConsumed {
		return fmt.Errorf("consume must be one-shot: %v --%v--> %v escapes the consumed terminal", from, event, to)
	}
	if event == evtSweepExpire && from != payloadExpired {
		return fmt.Errorf("sweep-expire from %v: only an expired attempt is sweepable (fresh attempts are untouchable)", from)
	}
	if to == payloadForeign && event != evtForeignClaim {
		return fmt.Errorf("foreign entered via %v: off-limits artifacts are discovered, never created or destroyed by lifecycle actions", event)
	}
	return nil
}

// payloadAlphabetComplete proves the declared alphabet covers every table
// event and every alphabet event is live in the table.
func payloadAlphabetComplete() error {
	tableEvents := payloadMachine.Events()
	for _, e := range payloadAlphabet {
		found := false
		for _, te := range tableEvents {
			if te == e {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("alphabet event %v has no table edge: dead declaration", e)
		}
	}
	for _, te := range tableEvents {
		found := false
		for _, e := range payloadAlphabet {
			if te == e {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("table event %v missing from the declared alphabet", te)
		}
	}
	return nil
}

// payloadAlwaysReachableGone proves every state except the deliberately
// off-limits foreign terminal can still reach a gone terminal — no eternal
// litter inside the ownership boundary.
func payloadAlwaysReachableGone() error {
	gone := map[payloadState]bool{
		payloadConsumed: true, payloadDiscardedInBand: true, payloadSwept: true,
	}
	for from := payloadAbsent; from <= payloadSwept; from++ {
		if from == payloadForeign {
			continue // off-limits by design; cleanup is out of band
		}
		ok := false
		for _, st := range payloadMachine.ReachableFrom(from, payloadAlphabet) {
			if gone[st] {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("state %v cannot reach a gone terminal: an owned attempt must never litter eternally", from)
		}
	}
	return nil
}

// payloadClassifySweep sweeps the classification's full input space (five
// booleans, thirty-two combinations) and checks it against the sweep rule
// stated independently from the classifier, in the style of the git
// package's removal decision sweeps.
func payloadClassifySweep() error {
	for _, dirPresent := range []bool{false, true} {
		for _, dirOwned := range []bool{false, true} {
			for _, payloadPresent := range []bool{false, true} {
				for _, payloadOwned := range []bool{false, true} {
					for _, withinBudget := range []bool{false, true} {
						f := attemptFacts{
							dirPresent:     dirPresent,
							dirOwned:       dirOwned,
							payloadPresent: payloadPresent,
							payloadOwned:   payloadOwned,
							withinBudget:   withinBudget,
						}
						if err := payloadClassifyProbes(f); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}

// payloadClassifyProbes checks one facts combination against the
// independently stated sweep rule and the fresh-untouchable property.
func payloadClassifyProbes(f attemptFacts) error {
	got := classifyAttempt(f)
	// The sweep rule, derived from the facts directly (sweepAttempt's own
	// decision order): absent does nothing; unproven ownership of any
	// artifact refuses; a fresh attempt refuses; otherwise remove.
	var want sweepAction
	switch {
	case !f.dirPresent:
		want = actionNone
	case !f.dirOwned || (f.payloadPresent && !f.payloadOwned):
		want = actionRefuse
	case f.withinBudget:
		want = actionRefuse
	default:
		want = actionRemove
	}
	if sweepActionForClass(got) != want {
		return fmt.Errorf("facts %+v classified %v: sweep intent %v, want %v",
			f, got, sweepActionForClass(got), want)
	}
	// A fresh attempt is never removable, whatever else is true.
	if f.withinBudget && f.dirPresent && sweepActionForClass(got) == actionRemove {
		return fmt.Errorf("facts %+v classified %v: a fresh attempt must be untouchable", f, got)
	}
	// Unproven ownership is always off-limits, fresh or not.
	if f.dirPresent && (!f.dirOwned || (f.payloadPresent && !f.payloadOwned)) && got != classForeign {
		return fmt.Errorf("facts %+v classified %v: unproven ownership must classify foreign (fail closed)", f, got)
	}
	// The machine echoes the sweep intent: only the expired machine state
	// is sweepable (the table's sweep-expire edge invariant), foreign maps
	// onto the absorbing fail-closed state, absence onto the initial one.
	ms := got.machineState()
	if sweepActionForClass(got) == actionRemove && ms != payloadExpired {
		return fmt.Errorf("facts %+v classified %v (%v): a removable attempt must map onto expired", f, got, ms)
	}
	if got == classForeign && ms != payloadForeign {
		return fmt.Errorf("facts %+v: foreign must map onto the fail-closed state", f)
	}
	if got == classAbsent && ms != payloadAbsent {
		return fmt.Errorf("facts %+v: absence must map onto the initial state", f)
	}
	return nil
}
