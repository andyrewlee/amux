package git

import (
	"errors"
	"slices"
	"testing"

	"github.com/andyrewlee/amux/internal/fsm"
)

// cleanupAdvance and cleanupHalt model the two outcomes every cleanup step
// can produce: advancing to the next phase, or halting the run in Done
// (with or without an error — the machine models control flow, not error
// payloads).
type cleanupStepEvent int

const (
	cleanupAdvance cleanupStepEvent = iota
	cleanupHalt
)

func (e cleanupStepEvent) String() string {
	if e == cleanupAdvance {
		return "advance"
	}
	return "halt"
}

// cleanupPhaseMachine mirrors workspaceCleanupPhaseSteps' successor graph
// (success edges plus error halts). Keeping it beside the step table makes
// drift mechanical to detect: the test below fails when a step's possible
// successors change without this graph changing with them.
var cleanupPhaseMachine = fsm.MustCompile(fsm.Machine[workspaceCleanupPhase, cleanupStepEvent]{
	Name:    "workspace-cleanup-resume",
	Initial: cleanupPhaseLegacyGuard,
	Steps: []fsm.Step[workspaceCleanupPhase, cleanupStepEvent]{
		{From: cleanupPhaseLegacyGuard, Event: cleanupAdvance, To: cleanupPhaseValidateStage},
		{From: cleanupPhaseLegacyGuard, Event: cleanupHalt, To: cleanupPhaseDone},
		{From: cleanupPhaseValidateStage, Event: cleanupAdvance, To: cleanupPhaseUnregister},
		{From: cleanupPhaseValidateStage, Event: cleanupHalt, To: cleanupPhaseDone},
		{From: cleanupPhaseUnregister, Event: cleanupAdvance, To: cleanupPhaseRemoveStage},
		{From: cleanupPhaseUnregister, Event: cleanupHalt, To: cleanupPhaseDone},
		{From: cleanupPhaseRemoveStage, Event: cleanupAdvance, To: cleanupPhaseFinalize},
		{From: cleanupPhaseRemoveStage, Event: cleanupHalt, To: cleanupPhaseDone},
		{From: cleanupPhaseFinalize, Event: cleanupHalt, To: cleanupPhaseDone},
	},
	Terminals: []workspaceCleanupPhase{cleanupPhaseDone},
})

// TestCleanupPhaseGraphFormallyChecked verifies the cleanup resume graph:
// every phase is reachable from the resume start, Done is reachable from
// every phase (an interrupted cleanup can always terminate, idempotently),
// and nothing advances out of Done.
func TestCleanupPhaseGraphFormallyChecked(t *testing.T) {
	alphabet := []cleanupStepEvent{cleanupAdvance, cleanupHalt}
	if err := cleanupPhaseMachine.Check(fsm.Checker[workspaceCleanupPhase, cleanupStepEvent]{
		Alphabet:             alphabet,
		AllDeclaredReachable: true,
		Edge: func(from workspaceCleanupPhase, event cleanupStepEvent, to workspaceCleanupPhase) error {
			if from == cleanupPhaseDone && event == cleanupAdvance {
				return errors.New("the terminal phase must never advance")
			}
			return nil
		},
	}); err != nil {
		t.Fatalf("cleanup resume machine failed model checking: %v", err)
	}
	for _, phase := range []workspaceCleanupPhase{
		cleanupPhaseLegacyGuard, cleanupPhaseValidateStage, cleanupPhaseUnregister,
		cleanupPhaseRemoveStage, cleanupPhaseFinalize,
	} {
		if !slices.Contains(cleanupPhaseMachine.ReachableFrom(phase, alphabet), cleanupPhaseDone) {
			t.Errorf("phase %d cannot reach Done: an interrupted cleanup must always be able to terminate", phase)
		}
	}
}
