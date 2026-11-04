package data

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/andyrewlee/amux/internal/fsm"
)

// Transient reservation holds
//
// A workspace without a persisted store key cannot mint a durable
// reservation — but a purely local allocation is invisible to every other
// amux instance, so a second instance's durable mint (or another degraded
// instance's local pick) could select the same interval and hand two
// workspaces the same ports. Transient holds solve this by publishing the
// degraded allocation into the shared registry under a
// `transient-<ownerPID>-<rootHash>` key: the registry's mint scan and every
// used-set union then cover it exactly like a durable interval.
//
// Lifecycle contract (deliberately different from durable reservations):
//
//   - A transient hold lives as long as its OWNING PROCESS does. Dead-owner
//     holds are swept inside Reserve's write lock, so a crashed instance's
//     intervals free for reissue instead of burning forever. This is the
//     one sense in which transient ownership can legitimately end while a
//     consumer might exist: a tmux session may outlive its amux process —
//     the residual collision risk is the same one any port registry
//     carries for processes it cannot name, and it is the documented
//     trade-off for self-healing holds.
//   - Deleting the workspace releases the caller's own transient hold
//     (ReleasePort) — the workspace's sessions are torn down by the delete
//     path, so its ports are free.
//   - Transient holds are per-process and never durable: they are filtered
//     out of the reclaim enumeration (ReservedIntervals) so orphan
//     reclamation never lists them.
//   - A workspace that later gains a StoredID mints a normal durable
//     reservation alongside its transient hold — the two keys coexist,
//     intervals disjoint by construction, and the transient hold is swept
//     when the owning process exits.

const transientReservationPrefix = "transient-"

// TransientReservationKey mints the published-hold key for a workspace with
// no durable identity: the owner PID scopes liveness (dead-owner holds are
// swept on the next reservation write); the normalized-root hash scopes the
// workspace deterministically across instances.
func TransientReservationKey(ownerPID int, workspaceRoot string) string {
	sum := sha256.Sum256([]byte(NormalizePath(workspaceRoot)))
	return fmt.Sprintf("%s%d-%s", transientReservationPrefix, ownerPID, hex.EncodeToString(sum[:8]))
}

// IsTransientReservationID reports whether id is a transient published hold.
// Callers enumerating the registry for reclamation must exclude these —
// they are process-scoped and release only through owner death, workspace
// deletion, or the dead-owner sweep.
func IsTransientReservationID(id string) bool {
	return strings.HasPrefix(id, transientReservationPrefix)
}

// transientOwnerPID parses the owner PID out of a transient hold key,
// reporting ok=false for durable IDs and malformed keys.
func transientOwnerPID(id string) (int, bool) {
	if !IsTransientReservationID(id) {
		return 0, false
	}
	rest := id[len(transientReservationPrefix):]
	dash := strings.IndexByte(rest, '-')
	if dash <= 0 {
		return 0, false
	}
	pid, err := strconv.Atoi(rest[:dash])
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// The transient-hold lifecycle machine.
//
// Each registry entry lives one of five control states. The safety rule the
// machine checks: the Reserve-time sweep may remove a hold ONLY when its
// owner process is proven dead — a malformed key (conservative keep), a
// living owner, or a non-transient (durable) reservation is never touched
// by the sweep. Explicit release (ReleaseMany with the exact key) is the
// only other removal, and a durable mint alongside the hold never disturbs
// it (the no-identity → gained-StoredID coexistence: both keys stay in the
// registry, intervals disjoint by construction, until the transient one is
// swept or released). Intervals, PIDs, and timestamps are extended state;
// the table below is the checked spec of what may remove what.
type transientHoldState uint8

const (
	// holdClassifying: a registry key is being classified by shape (the
	// machine's entry state; classification itself is the first edge).
	holdClassifying transientHoldState = iota
	// holdDurable: a non-transient reservation ID — the sweep never touches
	// it (durable reservations release only through explicit release).
	holdDurable
	// holdOwnerLive: transient key, parseable owner, owner alive.
	holdOwnerLive
	// holdOwnerDead: transient key, parseable owner, owner gone — the one
	// state the sweep reclaims.
	holdOwnerDead
	// holdMalformed: transient-prefixed but unparseable owner — kept
	// conservatively (a leaked interval is recoverable; a premature release
	// is a collision).
	holdMalformed
	// holdReleased: the entry is gone from the registry (terminal).
	holdReleased
)

func (s transientHoldState) String() string {
	switch s {
	case holdClassifying:
		return "classifying"
	case holdDurable:
		return "durable"
	case holdOwnerLive:
		return "owner-live"
	case holdOwnerDead:
		return "owner-dead"
	case holdMalformed:
		return "malformed"
	default:
		return "released"
	}
}

type transientHoldEvent uint8

const (
	// evtKeyClassifiedDurable/Live/Dead/Malformed: the classification of a
	// registry key — the machine's entry edges.
	evtKeyClassifiedDurable transientHoldEvent = iota
	evtKeyClassifiedLive
	evtKeyClassifiedDead
	evtKeyClassifiedMalformed
	// evtOwnerDies: the owner process was observed gone.
	evtOwnerDies
	// evtTransientSweep: the Reserve-time dead-owner sweep ran.
	evtTransientSweep
	// evtOwnerReleased: explicit ReleaseMany removed the key.
	evtOwnerReleased
	// evtDurableMinted: the workspace gained a StoredID and minted a
	// durable reservation alongside the hold.
	evtDurableMinted
)

func (e transientHoldEvent) String() string {
	switch e {
	case evtKeyClassifiedDurable:
		return "key-durable"
	case evtKeyClassifiedLive:
		return "key-live"
	case evtKeyClassifiedDead:
		return "key-dead"
	case evtKeyClassifiedMalformed:
		return "key-malformed"
	case evtOwnerDies:
		return "owner-dies"
	case evtTransientSweep:
		return "sweep"
	case evtOwnerReleased:
		return "owner-released"
	default:
		return "durable-minted"
	}
}

var transientHoldMachine = fsm.MustCompile(fsm.Machine[transientHoldState, transientHoldEvent]{
	Name:    "transient-port-hold",
	Initial: holdClassifying,
	Steps: []fsm.Step[transientHoldState, transientHoldEvent]{
		// Entry: classify the key by shape.
		{From: holdClassifying, Event: evtKeyClassifiedDurable, To: holdDurable},
		{From: holdClassifying, Event: evtKeyClassifiedLive, To: holdOwnerLive},
		{From: holdClassifying, Event: evtKeyClassifiedDead, To: holdOwnerDead},
		{From: holdClassifying, Event: evtKeyClassifiedMalformed, To: holdMalformed},
		// The sweep is a no-op everywhere except a dead owner.
		{From: holdDurable, Event: evtTransientSweep, To: holdDurable},
		{From: holdOwnerLive, Event: evtTransientSweep, To: holdOwnerLive},
		{From: holdOwnerDead, Event: evtTransientSweep, To: holdReleased},
		{From: holdMalformed, Event: evtTransientSweep, To: holdMalformed},
		// Owner liveness moves between the live/dead pair only.
		{From: holdOwnerLive, Event: evtOwnerDies, To: holdOwnerDead},
		{From: holdOwnerDead, Event: evtOwnerDies, To: holdOwnerDead},
		// Explicit release removes any shape (the caller named the key).
		{From: holdDurable, Event: evtOwnerReleased, To: holdReleased},
		{From: holdOwnerLive, Event: evtOwnerReleased, To: holdReleased},
		{From: holdOwnerDead, Event: evtOwnerReleased, To: holdReleased},
		{From: holdMalformed, Event: evtOwnerReleased, To: holdReleased},
		// The durable mint coexists with the hold: no state may change.
		{From: holdDurable, Event: evtDurableMinted, To: holdDurable},
		{From: holdOwnerLive, Event: evtDurableMinted, To: holdOwnerLive},
		{From: holdOwnerDead, Event: evtDurableMinted, To: holdOwnerDead},
		{From: holdMalformed, Event: evtDurableMinted, To: holdMalformed},
	},
	Terminals: []transientHoldState{holdReleased},
})

// classifyTransientHold reduces a registry key plus a liveness probe to its
// control state. alive is injected so tests can fake owner PIDs.
func classifyTransientHold(id string, alive func(int) bool) transientHoldState {
	if !IsTransientReservationID(id) {
		return holdDurable
	}
	pid, ok := transientOwnerPID(id)
	if !ok {
		return holdMalformed
	}
	if alive(pid) {
		return holdOwnerLive
	}
	return holdOwnerDead
}

// TransientHoldCheck formally verifies the hold lifecycle: the sweep
// removes a hold only from the dead-owner state (never a durable, living,
// or malformed entry), an explicit release removes anything, a durable mint
// never disturbs a hold, and every state is reachable. Runs in data's
// tests.
func TransientHoldCheck() error {
	return transientHoldMachine.Check(fsm.Checker[transientHoldState, transientHoldEvent]{
		Alphabet: []transientHoldEvent{
			evtKeyClassifiedDurable, evtKeyClassifiedLive, evtKeyClassifiedDead,
			evtKeyClassifiedMalformed, evtOwnerDies, evtTransientSweep,
			evtOwnerReleased, evtDurableMinted,
		},
		AllDeclaredReachable: true,
		Edge: func(from transientHoldState, event transientHoldEvent, to transientHoldState) error {
			if event == evtTransientSweep && to == holdReleased && from != holdOwnerDead {
				return fmt.Errorf("sweep released %v: only a proven-dead owner's hold may be swept", from)
			}
			if event == evtTransientSweep && from != holdOwnerDead && from != to {
				return fmt.Errorf("sweep moved %v to %v: the sweep must leave every other hold untouched", from, to)
			}
			if event == evtDurableMinted && from != to {
				return fmt.Errorf("durable mint moved %v to %v: the durable reservation and the transient hold must coexist", from, to)
			}
			return nil
		},
	})
}

// sweepDeadTransientReservationsLocked drops every transient hold whose
// owning process is gone. Call inside a write-locked registry transaction —
// the mutation is committed by the enclosing fsatomic write, so a read
// never observes a partially swept file. The decision is the checked
// machine's: classify, step the sweep event, delete only on released. An
// unparseable or still-living owner keeps its hold (the conservative
// direction: a leaked interval is recoverable, a premature release is a
// collision).
func sweepDeadTransientReservationsLocked(file *portReservationFile) {
	for id := range file.Reservations {
		state := classifyTransientHold(id, transientOwnerAlive)
		next, _ := transientHoldMachine.Next(state, evtTransientSweep)
		if next == holdReleased {
			delete(file.Reservations, id)
		}
	}
}
