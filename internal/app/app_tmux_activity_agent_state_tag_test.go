package app

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

// ---------------------------------------------------------------------------
// sessionAgentStateChanges — pure coalescing logic (no tmux involved).
// ---------------------------------------------------------------------------

// TestSessionAgentStateChanges_CoalescesToRealTransitions proves that only
// sessions whose classified AgentState actually changed this scan are
// returned — an unchanged session must not appear, which is the coalescing
// plan 057 requires to bound @amux_agent_state writes to real transitions
// instead of firing on every ~5s scan tick.
func TestSessionAgentStateChanges_CoalescesToRealTransitions(t *testing.T) {
	now := time.Now()
	prev := map[string]*activity.SessionState{
		// "idle-to-working" intentionally absent: a nil prev state classifies
		// as StateIdle (activity.ClassifyState's documented default).
		"working-to-done": {Score: activity.ScoreThreshold, LastActiveAt: now},
		"unchanged":       {Score: activity.ScoreThreshold},
	}
	updated := map[string]*activity.SessionState{
		"idle-to-working": {Score: activity.ScoreThreshold},
		"working-to-done": {Score: 0, LastWorkingAt: now},
		"unchanged":       {Score: activity.ScoreThreshold},
	}

	changes := sessionAgentStateChanges(prev, updated, now)

	got := make(map[string]activity.AgentState, len(changes))
	for _, c := range changes {
		got[c.sessionName] = c.state
	}
	if len(changes) != 2 {
		t.Fatalf("expected exactly 2 coalesced changes, got %d: %#v", len(changes), changes)
	}
	if got["idle-to-working"] != activity.StateWorking {
		t.Errorf("idle-to-working: got %v, want StateWorking", got["idle-to-working"])
	}
	if got["working-to-done"] != activity.StateDone {
		t.Errorf("working-to-done: got %v, want StateDone", got["working-to-done"])
	}
	if _, ok := got["unchanged"]; ok {
		t.Errorf("unchanged session must be coalesced out, but it appeared: %v", got["unchanged"])
	}
}

// TestSessionAgentStateChanges_EmptyUpdatedYieldsNoChanges verifies the
// zero-updates case produces no changes and does not panic on a nil map.
func TestSessionAgentStateChanges_EmptyUpdatedYieldsNoChanges(t *testing.T) {
	prev := map[string]*activity.SessionState{"a": {Score: activity.ScoreThreshold}}
	changes := sessionAgentStateChanges(prev, nil, time.Now())
	if len(changes) != 0 {
		t.Fatalf("expected no changes for empty updatedStates, got %#v", changes)
	}
}

// ---------------------------------------------------------------------------
// agentStateTagWriteCmd — the best-effort dispatch, via the setAgentStateTag
// seam (mirrors the runTmuxCmd/runTmuxCmdCombined seam pattern in
// internal/tmux, since SetSessionTagValue itself is a direct package call
// with no interface to fake through).
// ---------------------------------------------------------------------------

type recordedAgentStateTagWrite struct {
	sessionName string
	key         string
	value       string
}

func fakeSetAgentStateTag(t *testing.T, err error) *[]recordedAgentStateTagWrite {
	t.Helper()
	orig := setAgentStateTag
	var recorded []recordedAgentStateTagWrite
	setAgentStateTag = func(sessionName, key, value string, _ tmux.Options) error {
		recorded = append(recorded, recordedAgentStateTagWrite{sessionName, key, value})
		return err
	}
	t.Cleanup(func() { setAgentStateTag = orig })
	return &recorded
}

// drainCmd runs cmd and recursively runs every leaf command inside any
// resulting tea.BatchMsg, so best-effort side effects (like the tag write)
// dispatched via common.SafeBatch actually fire during a test, the same way
// the bubbletea runtime would eventually run them.
func drainCmd(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if bm, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range bm {
			drainCmd(c)
		}
	}
}

// TestAgentStateTagWriteCmd_NilForNoChanges verifies the no-churn guard: with
// no coalesced changes, no command (and therefore no tmux call) is produced.
func TestAgentStateTagWriteCmd_NilForNoChanges(t *testing.T) {
	recorded := fakeSetAgentStateTag(t, nil)
	if cmd := agentStateTagWriteCmd(nil, tmux.Options{}); cmd != nil {
		t.Fatal("expected nil cmd for zero changes")
	}
	if len(*recorded) != 0 {
		t.Fatalf("expected no tmux calls, got %#v", *recorded)
	}
}

// TestAgentStateTagWriteCmd_WritesStateStringPerChangedSession is the pin
// plan 057 asks for: a simulated state transition results in a
// SetSessionTagValue call using tmux.TagAgentState and state.String(). The
// write is also proven best-effort — a simulated tmux failure must not panic
// or surface as an error message.
func TestAgentStateTagWriteCmd_WritesStateStringPerChangedSession(t *testing.T) {
	recorded := fakeSetAgentStateTag(t, errors.New("simulated tmux failure"))

	changes := []agentStateTagChange{
		{sessionName: "sess-a", state: activity.StateWorking},
		{sessionName: "sess-b", state: activity.StateDone},
	}
	cmd := agentStateTagWriteCmd(changes, tmux.Options{ServerName: "test-server"})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for non-empty changes")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("best-effort write must yield no message even on failure, got %#v", msg)
	}

	if len(*recorded) != 2 {
		t.Fatalf("expected 2 recorded writes, got %d: %#v", len(*recorded), *recorded)
	}
	want := map[string]string{"sess-a": "working", "sess-b": "done"}
	for _, w := range *recorded {
		if w.key != tmux.TagAgentState {
			t.Errorf("write for %s used key %q, want %q", w.sessionName, w.key, tmux.TagAgentState)
		}
		if w.value != want[w.sessionName] {
			t.Errorf("write for %s: got value %q, want %q", w.sessionName, w.value, want[w.sessionName])
		}
	}
}

// ---------------------------------------------------------------------------
// applyTmuxActivityPayload wiring — proves the tag write is actually reachable
// from the real activity-result handling path, not just a dangling helper.
// ---------------------------------------------------------------------------

// TestApplyTmuxActivityPayload_EmitsAgentStateTagOnTransition simulates a
// session's first observation (idle -> working, since a brand-new session is
// immediately classified active) and asserts the returned command, once
// drained, issues exactly one @amux_agent_state write for that session.
func TestApplyTmuxActivityPayload_EmitsAgentStateTagOnTransition(t *testing.T) {
	recorded := fakeSetAgentStateTag(t, nil)

	app := &App{
		tmuxActivity: tmuxActivityState{
			sessionStates:      map[string]*activity.SessionState{},
			activeWorkspaceIDs: map[string]bool{},
			agentStates:        map[string]activity.AgentState{},
		},
		dashboard: dashboard.New(),
	}
	app.tmuxActivity.settled = true

	msg := tmuxActivityResult{
		ActiveWorkspaceIDs: map[string]bool{},
		AgentStates:        map[string]activity.AgentState{},
		UpdatedStates: map[string]*activity.SessionState{
			"sess-new": {Score: activity.ScoreThreshold},
		},
	}

	drainCmd(app.applyTmuxActivityPayload(msg))

	if len(*recorded) != 1 {
		t.Fatalf("expected 1 recorded tag write, got %d: %#v", len(*recorded), *recorded)
	}
	got := (*recorded)[0]
	if got.sessionName != "sess-new" || got.key != tmux.TagAgentState || got.value != "working" {
		t.Fatalf("unexpected tag write: %#v", got)
	}
}

// TestApplyTmuxActivityPayload_NoAgentStateTagWriteWhenUnchanged proves the
// coalescing end to end: a session reported with the same classification as
// last scan must not produce any @amux_agent_state write.
func TestApplyTmuxActivityPayload_NoAgentStateTagWriteWhenUnchanged(t *testing.T) {
	recorded := fakeSetAgentStateTag(t, nil)

	app := &App{
		tmuxActivity: tmuxActivityState{
			sessionStates: map[string]*activity.SessionState{
				"sess-steady": {Score: activity.ScoreThreshold},
			},
			activeWorkspaceIDs: map[string]bool{},
			agentStates:        map[string]activity.AgentState{},
		},
		dashboard: dashboard.New(),
	}
	app.tmuxActivity.settled = true

	msg := tmuxActivityResult{
		ActiveWorkspaceIDs: map[string]bool{},
		AgentStates:        map[string]activity.AgentState{},
		UpdatedStates: map[string]*activity.SessionState{
			"sess-steady": {Score: activity.ScoreThreshold},
		},
	}

	drainCmd(app.applyTmuxActivityPayload(msg))

	if len(*recorded) != 0 {
		t.Fatalf("expected no tag write for an unchanged state (coalescing), got %#v", *recorded)
	}
}
