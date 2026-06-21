package app

import (
	"testing"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/ui/common"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

// TestApplyTmuxActivityPayload_StoresAgentStates verifies that applyTmuxActivityPayload
// stores the AgentStates from the result onto tmuxActivity.agentStates.
func TestApplyTmuxActivityPayload_StoresAgentStates(t *testing.T) {
	app := &App{
		tmuxActivity: newTmuxActivityState(),
		dashboard:    dashboard.New(),
	}
	app.tmuxActivity.settled = true

	states := map[string]activity.AgentState{
		"ws-1": activity.StateWorking,
		"ws-2": activity.StateDone,
	}
	msg := tmuxActivityResult{
		ActiveWorkspaceIDs: map[string]bool{"ws-1": true},
		AgentStates:        states,
		UpdatedStates:      map[string]*activity.SessionState{},
	}

	app.applyTmuxActivityPayload(msg)

	if len(app.tmuxActivity.agentStates) != 2 {
		t.Fatalf("expected 2 agent states, got %d", len(app.tmuxActivity.agentStates))
	}
	if app.tmuxActivity.agentStates["ws-1"] != activity.StateWorking {
		t.Errorf("expected ws-1 to be StateWorking, got %v", app.tmuxActivity.agentStates["ws-1"])
	}
	if app.tmuxActivity.agentStates["ws-2"] != activity.StateDone {
		t.Errorf("expected ws-2 to be StateDone, got %v", app.tmuxActivity.agentStates["ws-2"])
	}
}

// TestApplyTmuxActivityPayload_NilAgentStatesDoesNotPanic verifies that a follower
// result with nil AgentStates does not panic during apply.
func TestApplyTmuxActivityPayload_NilAgentStatesDoesNotPanic(t *testing.T) {
	app := &App{
		tmuxActivity: newTmuxActivityState(),
		dashboard:    dashboard.New(),
	}
	app.tmuxActivity.settled = true

	msg := tmuxActivityResult{
		ActiveWorkspaceIDs: map[string]bool{},
		AgentStates:        nil, // follower path leaves this nil
		UpdatedStates:      map[string]*activity.SessionState{},
	}

	// Must not panic.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("applyTmuxActivityPayload panicked with nil AgentStates: %v", r)
		}
	}()
	app.applyTmuxActivityPayload(msg)
}

// TestCountWorkingToDone_TransitionFires verifies that a strict working→done
// transition is counted.
func TestCountWorkingToDone_TransitionFires(t *testing.T) {
	prev := map[string]activity.AgentState{
		"ws-1": activity.StateWorking,
	}
	next := map[string]activity.AgentState{
		"ws-1": activity.StateDone,
	}
	got := countWorkingToDone(prev, next)
	if got != 1 {
		t.Errorf("expected 1 transition, got %d", got)
	}
}

// TestCountWorkingToDone_NoSpuriousOnFirstScan verifies that a workspace
// already at StateDone with no prior state (empty prev) does not count as a
// transition. This prevents toast spam on the first scan.
func TestCountWorkingToDone_NoSpuriousOnFirstScan(t *testing.T) {
	prev := map[string]activity.AgentState{} // empty — first scan
	next := map[string]activity.AgentState{
		"ws-1": activity.StateDone,
	}
	got := countWorkingToDone(prev, next)
	if got != 0 {
		t.Errorf("expected 0 transitions on first scan, got %d", got)
	}
}

// TestCountWorkingToDone_IdempotentDone verifies that done→done does not count.
func TestCountWorkingToDone_IdempotentDone(t *testing.T) {
	prev := map[string]activity.AgentState{
		"ws-1": activity.StateDone,
	}
	next := map[string]activity.AgentState{
		"ws-1": activity.StateDone,
	}
	got := countWorkingToDone(prev, next)
	if got != 0 {
		t.Errorf("expected 0 transitions for done→done, got %d", got)
	}
}

// TestCountWorkingToDone_MultipleTransitions verifies that multiple
// simultaneous working→done transitions are counted together.
func TestCountWorkingToDone_MultipleTransitions(t *testing.T) {
	prev := map[string]activity.AgentState{
		"ws-1": activity.StateWorking,
		"ws-2": activity.StateWorking,
		"ws-3": activity.StateDone,
	}
	next := map[string]activity.AgentState{
		"ws-1": activity.StateDone,
		"ws-2": activity.StateDone,
		"ws-3": activity.StateDone,
	}
	got := countWorkingToDone(prev, next)
	if got != 2 {
		t.Errorf("expected 2 transitions, got %d", got)
	}
}

// TestApplyTmuxActivityPayload_ToastFiredOnWorkingToDone verifies that
// applyTmuxActivityPayload returns a non-nil command (toast batched) when a
// strict working→done transition occurs.
func TestApplyTmuxActivityPayload_ToastFiredOnWorkingToDone(t *testing.T) {
	app := &App{
		tmuxActivity: newTmuxActivityState(),
		dashboard:    dashboard.New(),
		toast:        common.NewToastModel(),
	}
	app.tmuxActivity.settled = true
	app.tmuxActivity.agentStates = map[string]activity.AgentState{
		"ws-1": activity.StateWorking,
	}

	msg := tmuxActivityResult{
		ActiveWorkspaceIDs: map[string]bool{},
		AgentStates: map[string]activity.AgentState{
			"ws-1": activity.StateDone,
		},
		UpdatedStates: map[string]*activity.SessionState{},
	}

	cmd := app.applyTmuxActivityPayload(msg)
	if cmd == nil {
		t.Error("expected a non-nil command (toast) on working→done transition, got nil")
	}
	if app.tmuxActivity.agentStates["ws-1"] != activity.StateDone {
		t.Errorf("expected ws-1 to be StateDone after apply, got %v", app.tmuxActivity.agentStates["ws-1"])
	}
}

// TestApplyTmuxActivityPayload_NoToastOnFirstScan verifies that no toast is
// fired when the first scan shows StateDone with no prior StateWorking (prev is
// empty). The returned cmd should be nil (no toast cmd, no spinner needed).
func TestApplyTmuxActivityPayload_NoToastOnFirstScan(t *testing.T) {
	app := &App{
		tmuxActivity: newTmuxActivityState(),
		dashboard:    dashboard.New(),
		toast:        common.NewToastModel(),
	}
	app.tmuxActivity.settled = true
	// prev is empty — first scan, no prior state

	msg := tmuxActivityResult{
		ActiveWorkspaceIDs: map[string]bool{},
		AgentStates: map[string]activity.AgentState{
			"ws-1": activity.StateDone,
		},
		UpdatedStates: map[string]*activity.SessionState{},
	}

	// The toast must not have been shown; we verify via ToastModel.Visible().
	app.applyTmuxActivityPayload(msg)
	if app.toast.Visible() {
		t.Error("expected no toast on first scan (no prior StateWorking), but toast is visible")
	}
}
