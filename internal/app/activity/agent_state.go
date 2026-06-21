package activity

import "time"

// ClassifyWorkspaceStates maps workspaces to a semantic AgentState. A workspace
// is Working if it is in `active`; otherwise Done if any of its sessions went
// quiet within DoneWindow (per ClassifyState); otherwise omitted. `updated` is
// keyed by session name. Workspace IDs are resolved from local tab metadata and
// the same tagged tmux session metadata used by active classification.
func ClassifyWorkspaceStates(
	active map[string]bool,
	updated map[string]*SessionState,
	infoBySession map[string]SessionInfo,
	sessions []TaggedSession,
	now time.Time,
) map[string]AgentState {
	out := make(map[string]AgentState, len(active))
	for wsID := range active {
		out[wsID] = StateWorking
	}
	workspaceBySession := workspaceIDsBySession(infoBySession, sessions)
	for name, st := range updated {
		if ClassifyState(st, now) != StateDone {
			continue
		}
		workspaceID := workspaceBySession[name]
		if workspaceID == "" {
			continue
		}
		if _, isWorking := out[workspaceID]; !isWorking {
			out[workspaceID] = StateDone
		}
	}
	return out
}

func workspaceIDsBySession(infoBySession map[string]SessionInfo, sessions []TaggedSession) map[string]string {
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
