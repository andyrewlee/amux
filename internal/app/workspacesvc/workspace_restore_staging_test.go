package workspacesvc

import "testing"

// TestRestoreStagingMachineFormallyChecked verifies the staging table by
// exhaustive model checking: rollback only from the created lineage, adoption
// only from a present dir with a verified identity, and every in-flight
// stage still able to complete.
func TestRestoreStagingMachineFormallyChecked(t *testing.T) {
	if err := RestoreStagingCheck(); err != nil {
		t.Fatalf("restore staging machine failed model checking: %v", err)
	}
}

// TestRestoreStagingStepRejectsLineageCrossing pins the fail-closed behavior
// of the cursor helper: an out-of-contract observation is a no-op, so a bug
// at one call site cannot silently move the restore into the other lineage
// (which would either leak a created worktree or roll back an adopted one).
func TestRestoreStagingStepRejectsLineageCrossing(t *testing.T) {
	cases := []struct {
		from  restoreStagingState
		event restoreStagingEvent
		want  restoreStagingState
	}{
		// The dir-probe fork is only legal from validation.
		{restoreAdopting, evtRestoreDirAbsent, restoreAdopting},
		{restoreHeldCreated, evtRestoreDirPresent, restoreHeldCreated},
		{restoreHeldAdopted, evtRestoreDirAbsent, restoreHeldAdopted},
		// Attach events are ignored outside the attaching stage.
		{restoreHeldAdopted, evtRestoreAttached, restoreHeldAdopted},
		// Flag clears mean nothing before the git wait.
		{restoreValidating, evtRestoreFlagsCleared, restoreValidating},
	}
	for _, tc := range cases {
		if got := restoreStagingStep(tc.from, tc.event); got != tc.want {
			t.Errorf("restoreStagingStep(%v, %v) = %v, want %v", tc.from, tc.event, got, tc.want)
		}
	}
}

// TestRestoreStagingRollbackObligation pins the obligation predicate against
// the table's failure edges: exactly the created-lineage stages owe a
// rollback, and exactly they transition to restoreRolledBack on failure.
func TestRestoreStagingRollbackObligation(t *testing.T) {
	for _, from := range []restoreStagingState{
		restoreValidating, restoreAdopting, restoreAttaching,
		restoreHeldAdopted, restoreHeldCreated,
		restoreUnarchivingAdopted, restoreUnarchivingCreated,
	} {
		failed := restoreStagingStep(from, evtRestoreStageFailed)
		if owes := restoreStagingOwesRollback(from); owes != (failed == restoreRolledBack) {
			t.Errorf("stage %v owes=%v but failure lands on %v: predicate and table disagree", from, owes, failed)
		}
	}
}
