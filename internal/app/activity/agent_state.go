package activity

import "time"

// ClassifyWorkspaceStates maps workspaces to a semantic AgentState. A workspace
// is Working if it is in `active`; otherwise Done if any of its sessions went
// quiet within DoneWindow (per ClassifyState); otherwise omitted (Idle is the
// absence of an entry). `updated` is keyed by session name; infoBySession maps
// session name -> SessionInfo (which carries WorkspaceID).
func ClassifyWorkspaceStates(
	active map[string]bool,
	updated map[string]*SessionState,
	infoBySession map[string]SessionInfo,
	now time.Time,
) map[string]AgentState {
	out := make(map[string]AgentState, len(active))
	for wsID := range active {
		out[wsID] = StateWorking
	}
	for name, st := range updated {
		if ClassifyState(st, now) != StateDone {
			continue
		}
		info, ok := infoBySession[name]
		if !ok || info.WorkspaceID == "" {
			continue
		}
		if _, isWorking := out[info.WorkspaceID]; !isWorking {
			out[info.WorkspaceID] = StateDone
		}
	}
	return out
}

// ClassifyState maps a session's hysteresis state to a semantic AgentState as
// of `now`. Working = currently active; Done = not active but went quiet within
// DoneWindow; Idle otherwise. This is deterministic — no screen parsing.
//
// The isActive derivation is kept identical to the one in
// activeWorkspaceIDsWithHysteresisWithSeen so "Working" exactly matches the
// existing active bit. If the hysteresis logic changes, update both in
// lockstep (or refactor to a shared predicate — a good Phase 2 cleanup).
func ClassifyState(state *SessionState, now time.Time) AgentState {
	if state == nil {
		return StateIdle
	}
	isActive := state.Score >= ScoreThreshold
	if !isActive && !state.LastActiveAt.IsZero() && now.Sub(state.LastActiveAt) < HoldDuration {
		isActive = true
	}
	if isActive {
		return StateWorking
	}
	if !state.LastWorkingAt.IsZero() && now.Sub(state.LastWorkingAt) < DoneWindow {
		return StateDone
	}
	return StateIdle
}
