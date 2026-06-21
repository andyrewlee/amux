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

// TestLastWorkingAtRecorded drives activeWorkspaceIDsWithHysteresisWithSeen
// via a mock CaptureFn (changing content → active) and asserts that
// LastWorkingAt was set on the returned state (proving Step 2 records it).
func TestLastWorkingAtRecorded(t *testing.T) {
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

	// First scan: initializes state (score set to threshold, immediately active).
	active, updated := activeIDsWithHysteresis(infoBySession, sessions, states, tmux.Options{}, captureFn, hashFn)
	if !active["ws1"] {
		t.Fatal("expected ws1 to be active after initialization scan")
	}
	state := updated["amux-ws1-tab-1"]
	if state == nil {
		t.Fatal("expected updated state after first scan")
	}
	if state.LastWorkingAt.IsZero() {
		t.Fatal("expected LastWorkingAt to be set when session is active")
	}

	// ClassifyState on the returned state must report Working.
	got := ClassifyState(state, time.Now())
	if got != StateWorking {
		t.Errorf("ClassifyState after active scan = %v, want StateWorking", got)
	}
}

func TestClassifyWorkspaceStates(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name          string
		active        map[string]bool
		updated       map[string]*SessionState
		infoBySession map[string]SessionInfo
		want          map[string]AgentState
	}{
		{
			name:          "workspace in active gets StateWorking",
			active:        map[string]bool{"ws1": true},
			updated:       map[string]*SessionState{},
			infoBySession: map[string]SessionInfo{"sess1": {WorkspaceID: "ws1"}},
			want:          map[string]AgentState{"ws1": StateWorking},
		},
		{
			name:   "session with LastWorkingAt within DoneWindow gets StateDone",
			active: map[string]bool{},
			updated: map[string]*SessionState{
				"sess2": {LastWorkingAt: now.Add(-DoneWindow / 2)},
			},
			infoBySession: map[string]SessionInfo{"sess2": {WorkspaceID: "ws2"}},
			want:          map[string]AgentState{"ws2": StateDone},
		},
		{
			name:   "working workspace is not downgraded to Done",
			active: map[string]bool{"ws3": true},
			updated: map[string]*SessionState{
				"sess3": {LastWorkingAt: now.Add(-DoneWindow / 2)},
			},
			infoBySession: map[string]SessionInfo{"sess3": {WorkspaceID: "ws3"}},
			want:          map[string]AgentState{"ws3": StateWorking},
		},
		{
			name:   "idle session contributes no entry",
			active: map[string]bool{},
			updated: map[string]*SessionState{
				"sess4": {LastWorkingAt: now.Add(-DoneWindow - time.Second)},
			},
			infoBySession: map[string]SessionInfo{"sess4": {WorkspaceID: "ws4"}},
			want:          map[string]AgentState{},
		},
		{
			name:          "session without workspace ID is skipped",
			active:        map[string]bool{},
			updated:       map[string]*SessionState{"sess5": {LastWorkingAt: now.Add(-DoneWindow / 2)}},
			infoBySession: map[string]SessionInfo{"sess5": {WorkspaceID: ""}},
			want:          map[string]AgentState{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyWorkspaceStates(tt.active, tt.updated, tt.infoBySession, now)
			if len(got) != len(tt.want) {
				t.Errorf("ClassifyWorkspaceStates() len = %d, want %d; got %v", len(got), len(tt.want), got)
				return
			}
			for wsID, wantState := range tt.want {
				gotState, ok := got[wsID]
				if !ok {
					t.Errorf("ClassifyWorkspaceStates() missing workspace %q", wsID)
					continue
				}
				if gotState != wantState {
					t.Errorf("ClassifyWorkspaceStates()[%q] = %v, want %v", wsID, gotState, wantState)
				}
			}
		})
	}
}
