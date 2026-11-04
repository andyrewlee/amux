package activity

import (
	"testing"
	"time"
)

// TestAgentStateMachineFormallyChecked verifies the semantic AgentState table
// by exhaustive model checking: every state reachable, each signal lands on
// its unique target, and Working only through an active observation.
func TestAgentStateMachineFormallyChecked(t *testing.T) {
	if err := AgentStateMachineCheck(); err != nil {
		t.Fatalf("agent semantic-state machine failed model checking: %v", err)
	}
}

// TestSignalForReduction pins the continuous-field reduction: score at or
// above the threshold is active; the hold window keeps a recently-active
// session active; the done window classifies quiet-recent; everything older
// is quiet-aged.
func TestSignalForReduction(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name  string
		state *SessionState
		want  AgentSignal
	}{
		{"nil state", nil, SignalQuietAged},
		{"score at threshold", &SessionState{Score: ScoreThreshold}, SignalActive},
		{"score below threshold with fresh hold", &SessionState{
			LastActiveAt: now.Add(-HoldDuration + time.Second),
		}, SignalActive},
		{"score below threshold with expired hold", &SessionState{
			LastActiveAt: now.Add(-HoldDuration - time.Second),
		}, SignalQuietAged},
		{"quiet within done window", &SessionState{
			LastWorkingAt: now.Add(-DoneWindow + time.Second),
		}, SignalQuietRecent},
		{"quiet past done window", &SessionState{
			LastWorkingAt: now.Add(-DoneWindow - time.Second),
		}, SignalQuietAged},
		{"never worked", &SessionState{}, SignalQuietAged},
	}
	for _, tc := range cases {
		if got := SignalFor(tc.state, now); got != tc.want {
			t.Errorf("%s: SignalFor = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestAgentStateTableMatchesSignalTarget differentially pins the table to
// the stateless classification: for every (prev, signal) the table's target
// equals the signal's direct target — the property that lets call sites
// classify through ClassifySignalTarget alone while the table remains the
// checked proof that the two agree.
func TestAgentStateTableMatchesSignalTarget(t *testing.T) {
	for _, prev := range []AgentState{StateIdle, StateWorking, StateDone} {
		for _, signal := range []AgentSignal{SignalActive, SignalQuietRecent, SignalQuietAged} {
			got, ok := agentStateMachine.Next(prev, signal)
			if !ok {
				t.Errorf("table rejects %v --%v-->: it must be total", prev, signal)
				continue
			}
			if want := ClassifySignalTarget(signal); got != want {
				t.Errorf("table %v --%v--> %v, want %v", prev, signal, got, want)
			}
		}
	}
}
