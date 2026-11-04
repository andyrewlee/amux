package workspacesvc

import "testing"

// TestDeleteRecoveryFormallyChecked verifies the delete-recovery
// authorization table by exhaustive model checking plus the full
// decision-table sweep: destructive recovery requires a tombstone AND proven
// absence, and every other combination fails safe.
func TestDeleteRecoveryFormallyChecked(t *testing.T) {
	if err := DeleteRecoveryCheck(); err != nil {
		t.Fatalf("delete-recovery authorization failed verification: %v", err)
	}
}

func TestDecideDeleteRecoveryFailsSafe(t *testing.T) {
	cases := []struct {
		tombstone bool
		dir       DirState
		want      deleteRecoveryAction
	}{
		{false, DirPresent, actionNone},
		{false, DirAbsent, actionNone},
		{false, DirUnknown, actionNone},
		{true, DirPresent, actionKeepWorkspace},
		{true, DirAbsent, actionFinishDelete},
		{true, DirUnknown, actionDefer},
	}
	for _, tc := range cases {
		if got := decideDeleteRecovery(tc.tombstone, tc.dir); got != tc.want {
			t.Errorf("decideDeleteRecovery(tombstone=%v, dir=%d) = %d, want %d",
				tc.tombstone, tc.dir, got, tc.want)
		}
	}
}
