package app

import (
	"sync"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/coalesce"
)

// tmuxActivityState groups the tmux activity-scan bookkeeping that previously
// lived as loose fields on App: ticker/dedup tokens, the scan-coalescing
// machine, shared-scan ownership, and per-session activity hysteresis. All
// fields are mutated only from App.Update handlers (single writer) EXCEPT
// missBySession, which the scan goroutine mutates under missMu.
type tmuxActivityState struct {
	// syncToken dedups tmux session-reconcile ticker generations (TmuxSyncTick).
	syncToken int
	// token dedups activity ticker generations (tmuxActivityTick).
	token activityScanToken
	// scan is the request coalescer: a request arriving mid-scan records a
	// pending obligation instead of spawning a second scan (the formally
	// verified internal/coalesce machine).
	scan *coalesce.Entry[scanWant]
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
	// agentStates holds the per-workspace semantic agent states (Working/Done/Idle)
	// as computed by the most recent owner scan.
	agentStates map[string]activity.AgentState
	// sessionStates holds per-session activity hysteresis state.
	sessionStates map[string]*activity.SessionState
	// agentStateBaseline holds the last accepted semantic AgentState per
	// session — what @amux_agent_state was last published as. Comparing a
	// fresh classification against this enum (not a timestamp-reinterpreted
	// previous snapshot) is what lets time-driven transitions like
	// Done→Idle publish: reclassifying the same stale snapshot twice can
	// never differ from itself. Cleared on owner transitions, pruned with
	// sessionStates.
	agentStateBaseline map[string]activity.AgentState
	// missMu serializes missBySession access across scan goroutines. Scans
	// are normally serialized by scanInFlight, but
	// resetAdvisoryLatchesAfterPanic can clear that flag while a scan is
	// still running — without this lock the overlap is a fatal
	// concurrent-map-write crash, not a recoverable panic.
	missMu sync.Mutex
	// missBySession holds each session's demotion-hysteresis tracker (the
	// checked liveness machine in internal/app/activity): it counts
	// consecutive non-live observations so a single transient miss does not
	// demote a working agent. Access only under missMu.
	missBySession map[string]*activity.Liveness
}

// scanWant is the scan coalescer's (empty) payload: every scan request is
// equivalent, so the merge is trivial.
type scanWant struct{}

func mergeScanWant(a, b scanWant) scanWant { return scanWant{} }

// scanEntry returns the scan coalescer, initializing it on first use.
func (t *tmuxActivityState) scanEntry() *coalesce.Entry[scanWant] {
	if t.scan == nil {
		t.scan = coalesce.New(mergeScanWant)
	}
	return t.scan
}

// scanCoalescePanicReset clears the in-flight scan mark while keeping any
// pending rescan obligation (the advisory-latch reset contract).
func (t *tmuxActivityState) scanCoalescePanicReset() {
	t.scanEntry().PanicReset()
}

// scanInFlightState returns a scan coalescer admitted into the in-flight
// state — the state a running scan holds (tests constructing states directly
// use this in place of the old scanInFlight literal).
func scanInFlightState() *coalesce.Entry[scanWant] {
	entry := coalesce.New(mergeScanWant)
	entry.Admit(scanWant{})
	return entry
}

// scanInFlightPendingState is scanInFlightState plus one coalesced request:
// a scan running with a rescan already owed (the old scanInFlight +
// rescanPending pair).
func scanInFlightPendingState() *coalesce.Entry[scanWant] {
	entry := scanInFlightState()
	entry.Admit(scanWant{})
	return entry
}

func newTmuxActivityState() tmuxActivityState {
	return tmuxActivityState{
		activeWorkspaceIDs: make(map[string]bool),
		agentStates:        make(map[string]activity.AgentState),
		sessionStates:      make(map[string]*activity.SessionState),
		agentStateBaseline: make(map[string]activity.AgentState),
		missBySession:      make(map[string]*activity.Liveness),
	}
}
