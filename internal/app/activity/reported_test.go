package activity

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/agentstatus"
	"github.com/andyrewlee/amux/internal/data"
)

func reportFor(state, kind string) agentstatus.Report {
	return agentstatus.Report{State: state, Kind: kind, App: "reporter"}
}

// TestMergeReportedSessionStates pins the authority rule: a known reported
// state replaces the heuristic classification for that session, everything
// else keeps the heuristic view.
func TestMergeReportedSessionStates(t *testing.T) {
	now := time.Now()
	states := map[string]*SessionState{
		"reporter-blocked": {Score: 0},                     // heuristic idle
		"reporter-working": {Score: 0},                     // heuristic idle
		"reporter-idle":    {Score: ScoreThreshold},        // heuristic working
		"silent":           {Score: 0, LastWorkingAt: now}, // heuristic done
	}
	reports := map[string]agentstatus.Report{
		"reporter-blocked": reportFor("blocked", "question"),
		"reporter-working": reportFor("working", ""),
		"reporter-idle":    reportFor("idle", ""),
	}
	got := MergeReportedSessionStates(states, reports, now)
	if got["reporter-blocked"] != data.StateBlocked {
		t.Errorf("blocked report must override heuristic idle, got %v", got["reporter-blocked"])
	}
	if got["reporter-working"] != data.StateWorking {
		t.Errorf("working report must override heuristic idle, got %v", got["reporter-working"])
	}
	if got["reporter-idle"] != data.StateIdle {
		t.Errorf("idle report must override heuristic working, got %v", got["reporter-idle"])
	}
	if got["silent"] != data.StateDone {
		t.Errorf("silent session keeps heuristic classification, got %v", got["silent"])
	}
	// No reports at all: pure heuristic.
	got = MergeReportedSessionStates(states, nil, now)
	if got["reporter-blocked"] != data.StateIdle {
		t.Errorf("nil reports must keep heuristic idle, got %v", got["reporter-blocked"])
	}
}

// TestMergeDropsTransientReportsOnDeadPanes pins the protocol's
// process-exit lifetime at the consumer: a dead pane's working/blocked
// report is void (its process exited), while done/error/idle survive.
func TestMergeDropsTransientReportsOnDeadPanes(t *testing.T) {
	now := time.Now()
	states := map[string]*SessionState{
		"dead-blocked": {Score: 0, LastWorkingAt: now},
		"dead-done":    {Score: 0, LastWorkingAt: now},
	}
	reports := map[string]agentstatus.Report{
		"dead-blocked": {State: "blocked", PaneDead: true},
		"dead-done":    {State: "done", PaneDead: true},
	}
	got := MergeReportedSessionStates(states, reports, now)
	if got["dead-blocked"] != data.StateDone {
		t.Errorf("dead pane's blocked report must fall back to heuristic, got %v", got["dead-blocked"])
	}
	if got["dead-done"] != data.StateDone {
		t.Errorf("dead pane's done report must survive, got %v", got["dead-done"])
	}
}

// TestFoldWorkspaceStates pins the severity fold: reported blocked outranks
// the heuristic active bit; a reported idle never masks it; done decays as
// before; sessions of unknown workspaces are ignored.
func TestFoldWorkspaceStates(t *testing.T) {
	active := map[string]bool{"ws-active": true}
	semantics := map[string]data.AgentState{
		"blocked-sess": data.StateBlocked,
		"idle-sess":    data.StateIdle,
		"done-sess":    data.StateDone,
		"error-sess":   data.StateError,
		"orphan-sess":  data.StateBlocked,
	}
	bySession := map[string]string{
		"blocked-sess": "ws-blocked",
		"idle-sess":    "ws-active",
		"done-sess":    "ws-done",
		"error-sess":   "ws-error",
		"orphan-sess":  "",
	}
	got := FoldWorkspaceStates(active, semantics, bySession)
	if got["ws-blocked"] != data.StateBlocked {
		t.Errorf("ws-blocked = %v, want Blocked", got["ws-blocked"])
	}
	if got["ws-active"] != data.StateWorking {
		t.Errorf("reported idle must not mask the active bit: %v", got["ws-active"])
	}
	if got["ws-done"] != data.StateDone {
		t.Errorf("ws-done = %v, want Done", got["ws-done"])
	}
	if got["ws-error"] != data.StateError {
		t.Errorf("ws-error = %v, want Error", got["ws-error"])
	}
	if _, exists := got["ws-active-orphan"]; exists {
		t.Error("session without workspace must be dropped")
	}
	if len(got) != 4 {
		t.Errorf("fold produced %d entries, want 4: %#v", len(got), got)
	}
}

// TestFoldBlockedReportOutranksActive pins the headline property: a blocked
// report on ANY session of a workspace whose agent tab is heuristically
// active still surfaces Blocked for the workspace.
func TestFoldBlockedReportOutranksActive(t *testing.T) {
	active := map[string]bool{"ws-1": true}
	semantics := map[string]data.AgentState{
		"agent-tab":   data.StateWorking, // heuristic: still streaming
		"helper-sess": data.StateBlocked, // reported: needs the user
	}
	bySession := map[string]string{"agent-tab": "ws-1", "helper-sess": "ws-1"}
	got := FoldWorkspaceStates(active, semantics, bySession)
	if got["ws-1"] != data.StateBlocked {
		t.Fatalf("blocked helper must outrank active agent tab, got %v", got["ws-1"])
	}
}
