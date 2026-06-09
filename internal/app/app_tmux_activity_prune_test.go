package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

// TestApplyTmuxActivityPayload_PrunesRemovedStates proves the merge step deletes
// session states the scan dropped (RemovedStates) while keeping merged ones,
// bounding the growth of sessionActivityStates.
func TestApplyTmuxActivityPayload_PrunesRemovedStates(t *testing.T) {
	app := &App{
		sessionActivityStates: map[string]*activity.SessionState{
			"keep": {Score: activity.ScoreThreshold},
			"drop": {Score: activity.ScoreThreshold},
		},
		tmuxActiveWorkspaceIDs: map[string]bool{},
		dashboard:              dashboard.New(),
	}

	app.applyTmuxActivityPayload(tmuxActivityResult{
		ActiveWorkspaceIDs: map[string]bool{},
		UpdatedStates: map[string]*activity.SessionState{
			"keep": {Score: activity.ScoreMax},
		},
		RemovedStates: []string{"drop"},
	})

	if _, ok := app.sessionActivityStates["drop"]; ok {
		t.Fatal("expected pruned session state to be deleted")
	}
	state, ok := app.sessionActivityStates["keep"]
	if !ok {
		t.Fatal("expected kept session state to remain")
	}
	if state.Score != activity.ScoreMax {
		t.Fatalf("expected kept state to be merged (score %d), got %d", activity.ScoreMax, state.Score)
	}
}

// TestApplyTmuxActivityPayload_RemoveDoesNotUndoSameScanReadd proves a name that
// is both merged and (defensively) listed in RemovedStates is deleted — removal
// runs after the merge — but this never happens in practice since the scan only
// lists pruned (omitted-from-updatedStates) sessions.
func TestApplyTmuxActivityPayload_RemoveRunsAfterMerge(t *testing.T) {
	app := &App{
		sessionActivityStates:  map[string]*activity.SessionState{},
		tmuxActiveWorkspaceIDs: map[string]bool{},
		dashboard:              dashboard.New(),
	}

	app.applyTmuxActivityPayload(tmuxActivityResult{
		ActiveWorkspaceIDs: map[string]bool{},
		UpdatedStates: map[string]*activity.SessionState{
			"x": {Score: activity.ScoreMax},
		},
		RemovedStates: []string{"x"},
	})

	if _, ok := app.sessionActivityStates["x"]; ok {
		t.Fatal("expected removal to run after merge (delete wins)")
	}
}
