package data

import "testing"

// TestWorkspaceRecordLifecycleFormallyChecked verifies the record lattice by
// exhaustive model checking: the ghost row is unreachable, retention purges
// never touch shelves, flag clears always land on live, and the projection
// is total over every flag × presence combination.
func TestWorkspaceRecordLifecycleFormallyChecked(t *testing.T) {
	if err := WorkspaceRecordLifecycleCheck(); err != nil {
		t.Fatalf("workspace record lifecycle machine failed model checking: %v", err)
	}
}

func TestRecordLifecycleStateProjection(t *testing.T) {
	cases := []struct {
		archived, shelved bool
		presence          WorktreePresence
		want              WorkspaceRecordState
	}{
		{false, false, WorktreePresent, RecordLive},
		{false, false, WorktreeAbsent, RecordLive},
		{false, false, WorktreeUnknown, RecordLive},
		{true, false, WorktreePresent, RecordAccidentalArchived},
		{true, false, WorktreeAbsent, RecordAccidentalArchived},
		{true, false, WorktreeUnknown, RecordAccidentalArchived},
		{true, true, WorktreePresent, RecordShelveIntent},
		{true, true, WorktreeAbsent, RecordShelved},
		// Unprobed dir collapses conservatively into the shelf (prune-exempt)
		// rather than the intent window.
		{true, true, WorktreeUnknown, RecordShelved},
		{false, true, WorktreePresent, RecordGhost},
		{false, true, WorktreeAbsent, RecordGhost},
		{false, true, WorktreeUnknown, RecordGhost},
	}
	for _, tc := range cases {
		w := &Workspace{Archived: tc.archived, Shelved: tc.shelved}
		if got := w.RecordLifecycleState(tc.presence); got != tc.want {
			t.Errorf("RecordLifecycleState(archived=%v shelved=%v presence=%v) = %v, want %v",
				tc.archived, tc.shelved, tc.presence, got, tc.want)
		}
	}
}

func TestRecordLifecycleStateNilSafe(t *testing.T) {
	var w *Workspace
	if got := w.RecordLifecycleState(WorktreeAbsent); got != RecordLive {
		t.Fatalf("nil workspace must project live, got %v", got)
	}
}

// TestRecordShelveIntentAlwaysResolves proves the crash-window contract from
// the machine's own table: every edge OUT of shelve-intent lands on shelved,
// live, or deleted — the intent window can only resolve (removal completes,
// flags roll back, recovery clears them, or the record is purged). It can
// never decay directly into an accidental archive or a ghost. (A record that
// resolved back to live may of course be archived again later by an ordinary
// rescan — that is a new lifecycle, not a stuck intent.)
func TestRecordShelveIntentAlwaysResolves(t *testing.T) {
	for _, event := range workspaceRecordMachine.Events() {
		to, ok := workspaceRecordMachine.Next(RecordShelveIntent, event)
		if !ok {
			continue
		}
		switch to {
		case RecordShelved, RecordLive, RecordDeleted, RecordShelveIntent:
		default:
			t.Fatalf("shelve-intent --%v--> %v: the crash window must resolve to shelved, live, or deletion", event, to)
		}
	}
}

// TestRecordPurgeGatesMatchPrune pins the equivalence between the table's
// purge gates and the flag comparisons PruneStale historically made inline:
// the "archived" reason is exactly the accidental state, and the
// missing-root gate is exactly !ShelvedFlag() == !Shelved — including the
// ghost row (Shelved && !Archived), which the historical gate exempted and
// the table-checked gate must keep exempting — so rewiring prune onto the
// table cannot change which records it reaps.
func TestRecordPurgeGatesMatchPrune(t *testing.T) {
	for _, archived := range []bool{false, true} {
		for _, shelved := range []bool{false, true} {
			w := &Workspace{Archived: archived, Shelved: shelved}
			state := w.RecordLifecycleState(WorktreeAbsent)
			if (state == RecordAccidentalArchived) != (archived && !shelved) {
				t.Fatalf("state %v must equal the archived-reason gate (Archived && !Shelved)", state)
			}
			if state.ShelvedFlag() != shelved {
				t.Fatalf("state %v ShelvedFlag()=%v must equal the historical missing-root gate !Shelved", state, state.ShelvedFlag())
			}
		}
	}
}
