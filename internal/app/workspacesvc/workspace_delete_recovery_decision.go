package workspacesvc

import (
	"errors"
	"fmt"

	"github.com/andyrewlee/amux/internal/fsm"
)

// deleteRecoveryState is a workspace's position in the interrupted-delete
// recovery protocol. The destructive path (finishing) is reachable only from
// a tombstone plus a probe that PROVES the worktree absent.
type deleteRecoveryState int

const (
	// recoveryLive: no delete tombstone — nothing to recover.
	recoveryLive deleteRecoveryState = iota
	// recoveryTombstoned: a durable tombstone exists; the worktree probe
	// decides what recovery may do.
	recoveryTombstoned
	// recoveryKept: tombstoned but the worktree survives — an earlier delete
	// failed before removing it; the workspace stays usable.
	recoveryKept
	// recoveryDeferred: tombstoned but absence is unproven — keep the
	// tombstone and retry on a later startup.
	recoveryDeferred
	// recoveryFinishing: tombstone plus proven absence — kill sessions,
	// delete the branch, remove the metadata.
	recoveryFinishing
	// recoveryFinished: metadata removed; the delete is complete.
	recoveryFinished
)

func (s deleteRecoveryState) String() string {
	switch s {
	case recoveryTombstoned:
		return "tombstoned"
	case recoveryKept:
		return "kept-usable"
	case recoveryDeferred:
		return "deferred"
	case recoveryFinishing:
		return "finishing"
	case recoveryFinished:
		return "finished"
	default:
		return "live"
	}
}

// recoveryEvent is the recovery protocol's alphabet: the durable tombstone
// write, the three DirState probe outcomes, and the metadata removal that
// completes a delete.
type recoveryEvent int

const (
	evtTombstoneWritten recoveryEvent = iota
	evtProbePresent
	evtProbeAbsent
	evtProbeUnknown
	evtMetadataDeleted
)

func (e recoveryEvent) String() string {
	switch e {
	case evtTombstoneWritten:
		return "tombstone-written"
	case evtProbePresent:
		return "probe-present"
	case evtProbeAbsent:
		return "probe-absent"
	case evtMetadataDeleted:
		return "metadata-deleted"
	default:
		return "probe-unknown"
	}
}

// deleteRecoveryMachine is the interrupted-delete recovery protocol. The
// safety-critical property: recoveryFinishing is entered ONLY via
// evtProbeAbsent from a tombstoned state — a surviving or unproven worktree
// can never authorize destruction.
var deleteRecoveryMachine = fsm.MustCompile(fsm.Machine[deleteRecoveryState, recoveryEvent]{
	Name:    "delete-recovery",
	Initial: recoveryLive,
	Steps: []fsm.Step[deleteRecoveryState, recoveryEvent]{
		// Delete start: the durable tombstone precedes any teardown.
		{From: recoveryLive, Event: evtTombstoneWritten, To: recoveryTombstoned},
		// A live workspace ignores probe outcomes entirely.
		{From: recoveryLive, Event: evtProbePresent, To: recoveryLive},
		{From: recoveryLive, Event: evtProbeAbsent, To: recoveryLive},
		{From: recoveryLive, Event: evtProbeUnknown, To: recoveryLive},
		// Tombstoned: the probe decides. Present keeps the workspace; absent
		// finishes the delete; unknown defers with the tombstone kept.
		{From: recoveryTombstoned, Event: evtProbePresent, To: recoveryKept},
		{From: recoveryTombstoned, Event: evtProbeAbsent, To: recoveryFinishing},
		{From: recoveryTombstoned, Event: evtProbeUnknown, To: recoveryDeferred},
		// Kept-usable re-probes like tombstoned (a later retry may finish).
		{From: recoveryKept, Event: evtProbePresent, To: recoveryKept},
		{From: recoveryKept, Event: evtProbeAbsent, To: recoveryFinishing},
		{From: recoveryKept, Event: evtProbeUnknown, To: recoveryKept},
		// Deferred retries the probe on the next startup.
		{From: recoveryDeferred, Event: evtProbeUnknown, To: recoveryDeferred},
		{From: recoveryDeferred, Event: evtProbeAbsent, To: recoveryFinishing},
		{From: recoveryDeferred, Event: evtProbePresent, To: recoveryKept},
		// Finishing completes by removing the metadata.
		{From: recoveryFinishing, Event: evtMetadataDeleted, To: recoveryFinished},
	},
	Terminals: []deleteRecoveryState{recoveryFinished},
})

// deleteRecoveryAction is the caller-facing decision derived from the
// protocol state.
type deleteRecoveryAction int

const (
	// actionNone: nothing to recover; surface the workspace normally.
	actionNone deleteRecoveryAction = iota
	// actionKeepWorkspace: surface it and keep it usable.
	actionKeepWorkspace
	// actionDefer: keep the tombstone, skip surfacing decisions to later.
	actionDefer
	// actionFinishDelete: authorized to kill sessions, delete the branch,
	// and remove the metadata.
	actionFinishDelete
)

// decideDeleteRecovery runs one recovery step: the entry state is derived
// from the durable tombstone, the event from the probed DirState, and the
// action from the resulting protocol state.
func decideDeleteRecovery(tombstone bool, dir DirState) deleteRecoveryAction {
	state := recoveryLive
	if tombstone {
		state = recoveryTombstoned
	}
	var event recoveryEvent
	switch dir {
	case DirPresent:
		event = evtProbePresent
	case DirAbsent:
		event = evtProbeAbsent
	default:
		event = evtProbeUnknown
	}
	next, _ := deleteRecoveryMachine.Next(state, event)
	switch next {
	case recoveryKept:
		return actionKeepWorkspace
	case recoveryDeferred:
		return actionDefer
	case recoveryFinishing, recoveryFinished:
		return actionFinishDelete
	default:
		return actionNone
	}
}

// DeleteRecoveryCheck formally verifies the recovery protocol: every state
// reachable, the destructive state entered only through proven absence, and
// the full decision table (tombstone × DirState) authorizing destruction
// exactly when a tombstone exists AND the worktree is proven gone.
func DeleteRecoveryCheck() error {
	if err := deleteRecoveryMachine.Check(fsm.Checker[deleteRecoveryState, recoveryEvent]{
		Alphabet: []recoveryEvent{
			evtTombstoneWritten, evtProbePresent, evtProbeAbsent, evtProbeUnknown, evtMetadataDeleted,
		},
		AllDeclaredReachable: true,
		Edge: func(from deleteRecoveryState, event recoveryEvent, to deleteRecoveryState) error {
			if to == recoveryFinishing && event != evtProbeAbsent {
				return fmt.Errorf("the destructive state is entered via %v: only proven absence may authorize it", event)
			}
			if to == recoveryKept && event == evtProbeAbsent {
				return errors.New("a proven-absent worktree must never keep the workspace usable")
			}
			return nil
		},
	}); err != nil {
		return err
	}
	// Exhaustive decision-table sweep: 2 tombstone states x 3 DirStates.
	for _, tombstone := range []bool{false, true} {
		for _, dir := range []DirState{DirPresent, DirAbsent, DirUnknown} {
			action := decideDeleteRecovery(tombstone, dir)
			if wantFinish := tombstone && dir == DirAbsent; (action == actionFinishDelete) != wantFinish {
				return fmt.Errorf("decision (tombstone=%v, dir=%d) = %d: destructive recovery must require tombstone AND proven absence", tombstone, dir, action)
			}
		}
	}
	return nil
}
