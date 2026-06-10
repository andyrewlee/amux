package app

import "github.com/andyrewlee/amux/internal/app/activity"

// tmuxActivityState groups the tmux activity-scan bookkeeping that previously
// lived as loose fields on App: ticker/dedup tokens, scan-coalescing flags,
// shared-scan ownership, and per-session activity hysteresis. It is mutated
// only from App.Update handlers (single writer).
type tmuxActivityState struct {
	// syncToken dedups tmux session-reconcile ticker generations (TmuxSyncTick).
	syncToken int
	// token dedups activity ticker generations (tmuxActivityTick).
	token int
	// scanInFlight and rescanPending coalesce overlapping scan requests: a
	// request that arrives mid-scan marks rescanPending instead of spawning a
	// second scan.
	scanInFlight  bool
	rescanPending bool
	// settled and settledScans track post-startup scan settling: activity is
	// not trusted until a few consecutive scans agree.
	settled      bool
	settledScans int
	// scannerOwner, ownershipSet and ownerEpoch hold shared-scan leadership
	// state: one amux instance scans on behalf of all instances on the host.
	scannerOwner bool
	ownershipSet bool
	ownerEpoch   int64
	// activeWorkspaceIDs is the latest set of workspace IDs with active agent
	// sessions, as published by the most recent scan.
	activeWorkspaceIDs map[string]bool
	// sessionStates holds per-session activity hysteresis state.
	sessionStates map[string]*activity.SessionState
	// missBySession counts consecutive non-live activity observations per
	// session so a single transient miss does not demote a working agent.
	missBySession map[string]int
}

func newTmuxActivityState() tmuxActivityState {
	return tmuxActivityState{
		activeWorkspaceIDs: make(map[string]bool),
		sessionStates:      make(map[string]*activity.SessionState),
		missBySession:      make(map[string]int),
	}
}
