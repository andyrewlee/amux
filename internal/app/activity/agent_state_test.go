package activity

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestClassifyState(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name  string
		state *SessionState
		want  AgentState
	}{
		{
			name:  "nil state returns idle",
			state: nil,
			want:  StateIdle,
		},
		{
			name:  "zero-value state returns idle",
			state: &SessionState{},
			want:  StateIdle,
		},
		{
			name: "score at threshold returns working",
			state: &SessionState{
				Score:       ScoreThreshold,
				Initialized: true,
			},
			want: StateWorking,
		},
		{
			name: "score above threshold returns working",
			state: &SessionState{
				Score:       ScoreMax,
				Initialized: true,
			},
			want: StateWorking,
		},
		{
			name: "score below threshold but LastActiveAt within HoldDuration returns working",
			state: &SessionState{
				Score:        ScoreThreshold - 1,
				Initialized:  true,
				LastActiveAt: now.Add(-HoldDuration / 2),
			},
			want: StateWorking,
		},
		{
			name: "score below threshold and LastActiveAt just expired returns not working",
			state: &SessionState{
				Score:         ScoreThreshold - 1,
				Initialized:   true,
				LastActiveAt:  now.Add(-HoldDuration - time.Millisecond),
				LastWorkingAt: now.Add(-time.Second),
			},
			want: StateDone,
		},
		{
			name: "inactive with LastWorkingAt within DoneWindow returns done",
			state: &SessionState{
				Score:         0,
				Initialized:   true,
				LastWorkingAt: now.Add(-DoneWindow / 2),
			},
			want: StateDone,
		},
		{
			name: "inactive with LastWorkingAt exactly at DoneWindow boundary (just over) returns idle",
			state: &SessionState{
				Score:         0,
				Initialized:   true,
				LastWorkingAt: now.Add(-DoneWindow - time.Millisecond),
			},
			want: StateIdle,
		},
		{
			name: "inactive and never worked returns idle",
			state: &SessionState{
				Score:       0,
				Initialized: true,
			},
			want: StateIdle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyState(tt.state, now)
			if got != tt.want {
				t.Errorf("ClassifyState() = %v (%d), want %v (%d)", got, got, tt.want, tt.want)
			}
		})
	}
}

func TestAgentStateString(t *testing.T) {
	tests := []struct {
		state AgentState
		want  string
	}{
		{StateWorking, "working"},
		{StateDone, "done"},
		{StateIdle, "idle"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.state.String()
			if got != tt.want {
				t.Errorf("AgentState(%d).String() = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

// TestLastWorkingAtRecordsRealWorkOnly drives
// activeWorkspaceIDsWithHysteresisWithSeen via a mock CaptureFn and asserts
// that initialization does not count as finished work, while a real content
// delta does.
func TestLastWorkingAtRecordsRealWorkOnly(t *testing.T) {
	infoBySession := map[string]SessionInfo{
		"amux-ws1-tab-1": {WorkspaceID: "ws1", IsChat: true},
	}
	sessions := []tmux.SessionActivity{
		{Name: "amux-ws1-tab-1", WorkspaceID: "ws1", Type: "agent"},
	}

	callCount := 0
	hashes := [][16]byte{{1}, {2}, {3}}
	captureFn := func(string, int, tmux.Options) (string, bool) {
		return "content", true
	}
	hashFn := func(string) [16]byte {
		h := hashes[callCount%len(hashes)]
		callCount++
		return h
	}

	states := map[string]*SessionState{}

	// First scan: seeds the baseline (score 0, no activity claim, no
	// LastWorkingAt — initialization is not finished work).
	active, updated, _ := activeWorkspaceIDsWithHysteresisWithSeen(infoBySession, sessions, states, nil, nil, tmux.Options{}, captureFn, hashFn)
	if active["ws1"] {
		t.Fatal("first observation must not claim activity without a delta")
	}
	state := updated["amux-ws1-tab-1"]
	if state == nil {
		t.Fatal("expected updated state after first scan")
	}
	if !state.LastWorkingAt.IsZero() {
		t.Fatal("expected first-scan baseline to not set LastWorkingAt")
	}
	if state.Score != 0 {
		t.Fatalf("expected seeded score 0, got %d", state.Score)
	}

	// ClassifyState on the seeded state must report Idle, not Working.
	got := ClassifyState(state, time.Now())
	if got != StateIdle {
		t.Errorf("ClassifyState after seeded scan = %v, want StateIdle", got)
	}

	// Second scan: a real content delta (+2 → score 2) is still below the
	// threshold; LastWorkingAt records only crossing it.
	active, updated, _ = activeWorkspaceIDsWithHysteresisWithSeen(infoBySession, sessions, updated, nil, nil, tmux.Options{}, captureFn, hashFn)
	if active["ws1"] {
		t.Fatal("one changed scan (score=2) should not yet reach the threshold")
	}
	state = updated["amux-ws1-tab-1"]
	if state == nil {
		t.Fatal("expected updated state after second scan")
	}
	if !state.LastWorkingAt.IsZero() {
		t.Fatal("expected LastWorkingAt to stay unset below the threshold")
	}

	// Third scan: another delta crosses the threshold — work is recorded.
	active, updated, _ = activeWorkspaceIDsWithHysteresisWithSeen(infoBySession, sessions, updated, nil, nil, tmux.Options{}, captureFn, hashFn)
	if !active["ws1"] {
		t.Fatal("expected ws1 to be active after deltas cross the threshold")
	}
	state = updated["amux-ws1-tab-1"]
	if state == nil {
		t.Fatal("expected updated state after third scan")
	}
	if state.LastWorkingAt.IsZero() {
		t.Fatal("expected LastWorkingAt to be set after a real content delta")
	}
}
