package activity

import (
	"errors"
	"fmt"
	"time"

	"github.com/andyrewlee/amux/internal/fsm"
)

// AgentSignal is the reduction of a session's continuous hysteresis fields
// (score, hold timer, done window) to the single observation that drives
// semantic AgentState transitions.
type AgentSignal int

const (
	// SignalActive: the session is producing output (score at or above the
	// threshold, or inside the post-output hold window).
	SignalActive AgentSignal = iota
	// SignalQuietRecent: not active, but it went quiet within DoneWindow —
	// the transient "done" state.
	SignalQuietRecent
	// SignalQuietAged: quiet for longer than DoneWindow — decayed to idle.
	SignalQuietAged
)

func (s AgentSignal) String() string {
	switch s {
	case SignalActive:
		return "active"
	case SignalQuietRecent:
		return "quiet-recent"
	default:
		return "quiet-aged"
	}
}

// sessionActive is the active-bit predicate: score at or above the
// threshold, or inside the post-output hold window. SignalFor and the
// hysteresis scan (activeWorkspaceIDsWithHysteresis) both reduce through
// it, so the semantic Working state and the published active bit cannot
// drift apart.
func sessionActive(state *SessionState, now time.Time) bool {
	if state.Score >= ScoreThreshold {
		return true
	}
	return !state.LastActiveAt.IsZero() && now.Sub(state.LastActiveAt) < HoldDuration
}

// SignalFor reduces a session's hysteresis state to its current signal.
func SignalFor(state *SessionState, now time.Time) AgentSignal {
	if state == nil {
		return SignalQuietAged
	}
	if sessionActive(state, now) {
		return SignalActive
	}
	if !state.LastWorkingAt.IsZero() && now.Sub(state.LastWorkingAt) < DoneWindow {
		return SignalQuietRecent
	}
	return SignalQuietAged
}

// agentStateMachine is the semantic AgentState transition table: the target
// is a pure function of the signal (Active→Working, QuietRecent→Done,
// QuietAged→Idle) for every current state, which is exactly what the
// stateless ClassifyState derives — the table additionally records, per
// current state, which transitions are meaningful edges rather than
// re-observations.
var agentStateMachine = fsm.MustCompile(fsm.Machine[AgentState, AgentSignal]{
	Name:    "agent-semantic-state",
	Initial: StateIdle,
	Steps: []fsm.Step[AgentState, AgentSignal]{
		{From: StateIdle, Event: SignalActive, To: StateWorking},
		{From: StateIdle, Event: SignalQuietRecent, To: StateDone},
		{From: StateIdle, Event: SignalQuietAged, To: StateIdle},
		{From: StateWorking, Event: SignalActive, To: StateWorking},
		{From: StateWorking, Event: SignalQuietRecent, To: StateDone},
		{From: StateWorking, Event: SignalQuietAged, To: StateIdle},
		{From: StateDone, Event: SignalActive, To: StateWorking},
		{From: StateDone, Event: SignalQuietRecent, To: StateDone},
		{From: StateDone, Event: SignalQuietAged, To: StateIdle},
	},
})

// ClassifySignalTarget is the signal's target state — and, because the
// table above is total with every edge's target equal to it (a property
// AgentStateMachineCheck enforces), also the table's transition result
// from ANY current state. Call sites therefore classify through this one
// function; the table stays as the checked proof that the stateless
// classifier and the semantic transition system agree.
func ClassifySignalTarget(signal AgentSignal) AgentState {
	switch signal {
	case SignalActive:
		return StateWorking
	case SignalQuietRecent:
		return StateDone
	default:
		return StateIdle
	}
}

// AgentStateMachineCheck formally verifies the semantic-state table: every
// state reachable, each signal always lands on its unique target, and only
// an active observation yields Working.
func AgentStateMachineCheck() error {
	return agentStateMachine.Check(fsm.Checker[AgentState, AgentSignal]{
		Alphabet:             []AgentSignal{SignalActive, SignalQuietRecent, SignalQuietAged},
		AllDeclaredReachable: true,
		Edge: func(from AgentState, signal AgentSignal, to AgentState) error {
			if to != ClassifySignalTarget(signal) {
				return fmt.Errorf("signal %v must always land on %v, got %v", signal, ClassifySignalTarget(signal), to)
			}
			if to == StateWorking && signal != SignalActive {
				return errors.New("working is reachable only through an active observation")
			}
			return nil
		},
	})
}

// WorkspaceIDsBySession resolves session names to workspace IDs from local
// tab metadata and tagged tmux session metadata — the identity sources the
// activity scan and the report merge both join on.
func WorkspaceIDsBySession(infoBySession map[string]SessionInfo, sessions []TaggedSession) map[string]string {
	out := make(map[string]string, len(infoBySession)+len(sessions))
	for name, info := range infoBySession {
		if info.WorkspaceID != "" {
			out[name] = info.WorkspaceID
		}
	}
	for _, snapshot := range sessions {
		name := snapshot.Session.Name
		if name == "" {
			continue
		}
		info, ok := infoBySession[name]
		if workspaceID := WorkspaceIDForSession(snapshot.Session, info, ok); workspaceID != "" {
			out[name] = workspaceID
		}
	}
	return out
}

// ClassifyState maps a session's hysteresis state to a semantic AgentState as
// of `now`: reduce the continuous fields to a signal, then take the signal's
// target through the checked agentStateMachine. This is deterministic — no
// screen parsing. SignalFor and activeWorkspaceIDsWithHysteresis both reduce
// through the shared sessionActive predicate, so "Working" exactly matches
// the published active bit by construction.
func ClassifyState(state *SessionState, now time.Time) AgentState {
	return ClassifySignalTarget(SignalFor(state, now))
}
