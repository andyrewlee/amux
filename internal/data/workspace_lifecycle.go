package data

import (
	"fmt"

	"github.com/andyrewlee/amux/internal/fsm"
)

// The workspace record lifecycle lattice.
//
// A persisted Workspace carries its lifecycle in three fields — Archived,
// ArchivedAt, Shelved — and every consumer (shelve, restore, rescan, prune,
// the shelf list) re-derives "what kind of record is this?" from ad-hoc flag
// comparisons scattered across packages. The prose invariants that keep those
// comparisons consistent:
//
//   - "It never leaves Shelved-without-Archived — a record that is neither
//     live (listByRepo filters Archived) nor shelved (the shelf requires
//     both)" (workspacesvc ShelveWorkspace).
//   - "Intentional shelves never expire — only accidental archives are
//     bounded by ArchivedRetention" and "a shelved workspace's root is
//     missing by design" (WorkspaceStore.PruneStale's !Shelved gates).
//   - A shelve writes both flags in ONE store mutation precisely so the
//     Shelved-without-Archived ghost row cannot be persisted mid-crash.
//
// workspaceRecordMachine below turns that lattice into a checked table: the
// ghost state is unreachable, retention purges are legal only from
// non-shelved states, and every flag-clearing event lands on live. The
// persisted flags stay (they are the on-disk contract); in-memory legality
// derives from the table via RecordLifecycleState.
//
// The delete tombstone (.deleting) is deliberately NOT a dimension here: it
// overlays any record state orthogonally, and its own recovery protocol is
// already table-checked (workspacesvc's delete-recovery machine). Likewise
// rescan drift protection — a discovered worktree whose stored record has a
// drifted storeID must be matched by canonical (repo, root) key rather than
// flap-archived — is a call-site matching rule (workspaceLookupKey), not a
// property of the flag lattice.

// WorktreePresence is the dir-presence observation that splits the
// shelved-flag pair into its two lattice points. Unknown collapses them
// conservatively: a record whose dir was not probed counts as shelved
// (prune-exempt, shelf-listed), never as reapeable.
type WorktreePresence uint8

const (
	WorktreePresent WorktreePresence = iota
	WorktreeAbsent
	WorktreeUnknown
)

func (p WorktreePresence) String() string {
	switch p {
	case WorktreePresent:
		return "present"
	case WorktreeAbsent:
		return "absent"
	default:
		return "unknown"
	}
}

// WorkspaceRecordState is one lattice point of the record lifecycle. The
// first four are the only shapes writers produce; RecordGhost exists so the
// projection can name the forbidden combination instead of silently
// coercing it, and RecordDeleted is the machine's terminal (no row on disk).
type WorkspaceRecordState uint8

const (
	// RecordLive: !Archived && !Shelved — surfaced by listByRepo.
	RecordLive WorkspaceRecordState = iota
	// RecordShelveIntent: Archived && Shelved but the worktree still exists —
	// the crash window between persisting the intent flags and removing the
	// worktree. Restores adopt the existing worktree; rescan clears the
	// flags; prune exempts it.
	RecordShelveIntent
	// RecordShelved: Archived && Shelved and the worktree is gone — the
	// shelf is real. Persists until restored, purged, or rediscovered.
	RecordShelved
	// RecordAccidentalArchived: Archived && !Shelved — the worktree vanished
	// under us (rescan flap-archive, delete-failure archive). GC-bound after
	// ArchivedRetention; a rediscovered worktree clears it.
	RecordAccidentalArchived
	// RecordGhost: Shelved && !Archived — the forbidden point. Neither live
	// (listByRepo filters Archived) nor shelved (the shelf requires both):
	// a row invisible everywhere. No writer may produce it; the machine
	// proves no transition reaches it.
	RecordGhost
	// RecordDeleted: the metadata row is gone — prune or delete completed.
	// The projection never returns it (an absent row has no state); it is
	// the machine's terminal.
	RecordDeleted
)

func (s WorkspaceRecordState) String() string {
	switch s {
	case RecordShelveIntent:
		return "shelve-intent"
	case RecordShelved:
		return "shelved"
	case RecordAccidentalArchived:
		return "accidental-archived"
	case RecordGhost:
		return "ghost"
	case RecordDeleted:
		return "deleted"
	default:
		return "live"
	}
}

// Shelved reports whether the state is a real shelf — the records the
// shelf-list and skip-if-already-live checks treat as shelved. It is NOT
// the prune-exemption predicate: PruneStale's gates use ShelvedFlag
// below, which additionally exempts ghost rows (Shelved && !Archived)
// from the missing-root reap, exactly as the historical !ws.Shelved
// comparison did. Ghost deliberately reports false here: it is not a
// shelf, and callers treating it as one would legitimize it.
func (s WorkspaceRecordState) Shelved() bool {
	return s == RecordShelveIntent || s == RecordShelved
}

// ShelvedFlag reports whether the persisted row carries the raw Shelved
// bit, ghost included. The pre-table prune gate exempted anything with the
// bit set from the missing-root reap — including ghost rows, which are
// corruption, not a verdict that the worktree was intentionally removed —
// and the missing-root exemption deliberately preserves that: reaping a
// ghost is a destructive repair, and repairs to hand-corrupted state belong
// to an explicit recovery path, not a background sweep.
func (s WorkspaceRecordState) ShelvedFlag() bool {
	return s == RecordShelveIntent || s == RecordShelved || s == RecordGhost
}

// RecordLifecycleState derives the lattice point from the persisted flags
// plus the dir-presence observation. It is total: every flag combination
// maps to a named state, so call sites cannot invent a seventh shape. A
// ghost-flagged record (Shelved && !Archived) is named, not repaired —
// callers surface or log it rather than silently coercing it into a shelf.
func (w *Workspace) RecordLifecycleState(presence WorktreePresence) WorkspaceRecordState {
	if w == nil {
		return RecordLive
	}
	switch {
	case w.Shelved && !w.Archived:
		return RecordGhost
	case w.Shelved && w.Archived:
		if presence == WorktreePresent {
			return RecordShelveIntent
		}
		return RecordShelved
	case w.Archived:
		return RecordAccidentalArchived
	default:
		return RecordLive
	}
}

// WorkspaceRecordEvent is the alphabet of record-lifecycle writes. Each is
// emitted by exactly one writer family, so a violating table row points at
// the code that must change.
type WorkspaceRecordEvent uint8

const (
	// RecordShelveIntentMarked: ShelveWorkspace persisted Shelved+Archived
	// in one store mutation before removing the worktree.
	RecordShelveIntentMarked WorkspaceRecordEvent = iota
	// RecordShelveAborted: the worktree removal failed while the worktree
	// still exists — the intent flags are rolled back (clearShelveIntent).
	RecordShelveAborted
	// RecordWorktreeRemoved: the shelve's worktree removal completed.
	RecordWorktreeRemoved
	// RecordWorktreeReappeared: the dir came back while the flags stay set —
	// a restore that crashed after `worktree add` but before clearing flags.
	RecordWorktreeReappeared
	// RecordDiscovered: rescan found the worktree on disk; the discovery
	// merge clears both flags (a present worktree ends every archive kind).
	RecordDiscovered
	// RecordRescanMissing: rescan could not discover the worktree (gone or
	// unmanaged) and flap-archived the record.
	RecordRescanMissing
	// RecordRestored: RestoreWorkspace cleared the flags after the worktree
	// exists again (recreated or adopted).
	RecordRestored
	// RecordPurgeRetention: PruneStale removed the record via the archived
	// or missing-root reasons — legal only from non-shelved states.
	RecordPurgeRetention
	// RecordPurgeUnregistered: PruneStale removed the record because its
	// repo left the project registry (no Shelved gate — an unreachable
	// shelf row is as dead as an unreachable live one).
	RecordPurgeUnregistered
	// RecordPurgeExplicit: the user deleted/purged the record.
	RecordPurgeExplicit
)

func (e WorkspaceRecordEvent) String() string {
	switch e {
	case RecordShelveIntentMarked:
		return "shelve-intent-marked"
	case RecordShelveAborted:
		return "shelve-aborted"
	case RecordWorktreeRemoved:
		return "worktree-removed"
	case RecordWorktreeReappeared:
		return "worktree-reappeared"
	case RecordDiscovered:
		return "discovered"
	case RecordRescanMissing:
		return "rescan-missing"
	case RecordRestored:
		return "restored"
	case RecordPurgeRetention:
		return "purge-retention"
	case RecordPurgeUnregistered:
		return "purge-unregistered"
	default:
		return "purge-explicit"
	}
}

// workspaceRecordMachine is the record lifecycle lattice. Ghost is not
// merely missing from the table — Check proves it unreachable, which is the
// formal version of ShelveWorkspace's "never leaves
// Shelved-without-Archived" contract: no sequence of legal writes starting
// from a live record can reach it.
var workspaceRecordMachine = fsm.MustCompile(fsm.Machine[WorkspaceRecordState, WorkspaceRecordEvent]{
	Name:    "workspace-record-lifecycle",
	Initial: RecordLive,
	Steps: []fsm.Step[WorkspaceRecordState, WorkspaceRecordEvent]{
		// Shelve: intent flags first (one write), worktree removal second.
		{From: RecordLive, Event: RecordShelveIntentMarked, To: RecordShelveIntent},
		{From: RecordShelveIntent, Event: RecordWorktreeRemoved, To: RecordShelved},
		// The removal failed while the worktree lives: roll the flags back.
		{From: RecordShelveIntent, Event: RecordShelveAborted, To: RecordLive},
		// Crash windows resolve without operator action: discovery clears
		// intent flags (the worktree still exists), restore adopts them.
		{From: RecordShelveIntent, Event: RecordRestored, To: RecordLive},
		{From: RecordShelveIntent, Event: RecordDiscovered, To: RecordLive},
		// A restore that crashed after `worktree add` leaves flags set on a
		// present dir — back to the intent window, retried idempotently.
		{From: RecordShelved, Event: RecordWorktreeReappeared, To: RecordShelveIntent},
		{From: RecordShelved, Event: RecordRestored, To: RecordLive},
		{From: RecordShelved, Event: RecordDiscovered, To: RecordLive},
		// The accidental archive: the worktree vanished or drifted out from
		// under a live record; rediscovery revives it.
		{From: RecordLive, Event: RecordRescanMissing, To: RecordAccidentalArchived},
		{From: RecordAccidentalArchived, Event: RecordDiscovered, To: RecordLive},
		{From: RecordLive, Event: RecordDiscovered, To: RecordLive},
		// Purges. Retention purges never touch shelved states — that is the
		// prune gate this table formalizes; unregistered purges may.
		{From: RecordAccidentalArchived, Event: RecordPurgeRetention, To: RecordDeleted},
		{From: RecordLive, Event: RecordPurgeRetention, To: RecordDeleted},
		{From: RecordLive, Event: RecordPurgeUnregistered, To: RecordDeleted},
		{From: RecordShelveIntent, Event: RecordPurgeUnregistered, To: RecordDeleted},
		{From: RecordShelved, Event: RecordPurgeUnregistered, To: RecordDeleted},
		{From: RecordAccidentalArchived, Event: RecordPurgeUnregistered, To: RecordDeleted},
		{From: RecordLive, Event: RecordPurgeExplicit, To: RecordDeleted},
		{From: RecordShelveIntent, Event: RecordPurgeExplicit, To: RecordDeleted},
		{From: RecordShelved, Event: RecordPurgeExplicit, To: RecordDeleted},
		{From: RecordAccidentalArchived, Event: RecordPurgeExplicit, To: RecordDeleted},
	},
	Terminals: []WorkspaceRecordState{RecordDeleted},
})

// WorkspaceRecordLifecycleCheck formally verifies the record lattice:
//
//   - the ghost state (Shelved-without-Archived) is unreachable — the
//     shelve flow's one-write contract, proven over every write sequence;
//   - every state can reach live (an accidental archive or shelf is never a
//     one-way door except by explicit purge);
//   - retention purges never fire from shelved states (the prune gate);
//   - only discovery/restore/abort ever clear flags, and they always land
//     on live;
//   - the projection is total and consistent: each flag combination maps to
//     exactly one state, ghost arises only from Shelved&&!Archived, and
//     ProjectedFlags round-trips.
//
// Runs in data's tests.
func WorkspaceRecordLifecycleCheck() error {
	if err := workspaceRecordMachine.Check(fsm.Checker[WorkspaceRecordState, WorkspaceRecordEvent]{
		AllDeclaredReachable: true,
		ForbidReachable:      []WorkspaceRecordState{RecordGhost},
		Edge: func(from WorkspaceRecordState, event WorkspaceRecordEvent, to WorkspaceRecordState) error {
			if event == RecordPurgeRetention && from.Shelved() {
				return fmt.Errorf("retention purge from %v: a shelf never expires (prune's !Shelved gate)", from)
			}
			if to == RecordLive && from != RecordLive {
				switch event {
				case RecordDiscovered, RecordRestored, RecordShelveAborted:
				default:
					return fmt.Errorf("%v --%v--> live: only discovery, restore, or shelve-abort may clear flags", from, event)
				}
			}
			if to == RecordShelved && event != RecordWorktreeRemoved {
				return fmt.Errorf("shelved entered via %v: only a completed worktree removal makes a shelf real", event)
			}
			if to == RecordShelveIntent && !(event == RecordShelveIntentMarked && from == RecordLive ||
				event == RecordWorktreeReappeared && from == RecordShelved) {
				return fmt.Errorf("shelve-intent entered via %v from %v: intent is marked on live records or revisited by a reappearing worktree", event, from)
			}
			return nil
		},
	}); err != nil {
		return err
	}
	// No shelf or accidental archive is a one-way door: everything except
	// the terminal can still reach live.
	for _, from := range []WorkspaceRecordState{
		RecordLive, RecordShelveIntent, RecordShelved, RecordAccidentalArchived,
	} {
		reachable := workspaceRecordMachine.ReachableFrom(from, nil)
		live := false
		for _, st := range reachable {
			if st == RecordLive {
				live = true
				break
			}
		}
		if !live {
			return fmt.Errorf("state %v cannot return to live: archives and shelves must stay recoverable", from)
		}
	}
	// Decision-table sweep over the full flag × presence space: the
	// projection is total, ghost has exactly one preimage, and the shelved
	// predicate tracks the Shelved flag everywhere except ghost (where
	// honoring it would legitimize the forbidden row).
	for _, archived := range []bool{false, true} {
		for _, shelved := range []bool{false, true} {
			for _, presence := range []WorktreePresence{WorktreePresent, WorktreeAbsent, WorktreeUnknown} {
				w := &Workspace{Archived: archived, Shelved: shelved}
				got := w.RecordLifecycleState(presence)
				if got == RecordDeleted {
					return fmt.Errorf("projection (archived=%v shelved=%v presence=%v) = deleted: an absent row is not a state", archived, shelved, presence)
				}
				if wantGhost := shelved && !archived; (got == RecordGhost) != wantGhost {
					return fmt.Errorf("projection (archived=%v shelved=%v) = %v: ghost must arise exactly from Shelved-without-Archived", archived, shelved, got)
				}
				if got.Shelved() != (shelved && archived) {
					return fmt.Errorf("projection (archived=%v shelved=%v) = %v: Shelved() must track the flag pair", archived, shelved, got)
				}
				if got == RecordShelveIntent && presence != WorktreePresent {
					return fmt.Errorf("projection (shelved, presence=%v) = shelve-intent: intent requires a present worktree", presence)
				}
			}
		}
	}
	return nil
}
