package app

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

// ---------------------------------------------------------------------------
// sessionAgentStateChanges — pure coalescing logic (no tmux involved).
// ---------------------------------------------------------------------------

// TestSessionAgentStateChanges_CoalescesToRealTransitions proves that only
// sessions whose classified AgentState differs from the accepted baseline are
// returned — a session still classified as its baseline value must not
// appear, which is the coalescing contract that bounds @amux_agent_state
// writes to real transitions instead of firing on every ~5s scan tick.
func TestSessionAgentStateChanges_CoalescesToRealTransitions(t *testing.T) {
	now := time.Now()
	baseline := map[string]activity.AgentState{
		"working-to-done": activity.StateWorking,
		"unchanged":       activity.StateWorking,
	}
	current := map[string]*activity.SessionState{
		"idle-to-working": {Score: activity.ScoreThreshold},
		"working-to-done": {Score: 0, LastWorkingAt: now},
		"unchanged":       {Score: activity.ScoreThreshold},
	}

	changes := sessionAgentStateChanges(current, baseline, now)

	got := make(map[string]agentStateTagChange, len(changes))
	for _, c := range changes {
		got[c.sessionName] = c
	}
	// idle-to-working is a first observation (no baseline): it publishes the
	// current value with prev pinned to itself — no fabricated edge.
	if len(changes) != 2 {
		t.Fatalf("expected exactly 2 changes, got %d: %#v", len(changes), changes)
	}
	if g := got["idle-to-working"]; g.state != activity.StateWorking || g.prev != activity.StateWorking {
		t.Errorf("idle-to-working first observation: got %#v, want working/working", g)
	}
	if g := got["working-to-done"]; g.state != activity.StateDone || g.prev != activity.StateWorking {
		t.Errorf("working-to-done: got %#v, want done prev working", g)
	}
	if _, ok := got["unchanged"]; ok {
		t.Errorf("unchanged session must be coalesced out, but it appeared: %v", got["unchanged"])
	}
}

// TestSessionAgentStateChanges_EmptyCurrentYieldsNoChanges verifies the
// zero-session case produces no changes, does not panic on a nil current map,
// and prunes baseline entries for sessions no longer retained.
func TestSessionAgentStateChanges_EmptyCurrentYieldsNoChanges(t *testing.T) {
	baseline := map[string]activity.AgentState{"a": activity.StateWorking}
	changes := sessionAgentStateChanges(nil, baseline, time.Now())
	if len(changes) != 0 {
		t.Fatalf("expected no changes for empty current set, got %#v", changes)
	}
	if len(baseline) != 0 {
		t.Fatalf("baseline must drop pruned sessions, got %v", baseline)
	}
}

// TestSessionAgentStateChanges_ClockOnlyDoneToIdle is the regression this plan
// fixes: a session observed Done whose content never changes again must still
// publish Done→Idle once DoneWindow elapses. Diffing reclassified snapshots
// can never see that transition (stale prev and stale next both classify
// Idle); only a stored semantic baseline remembers that Done was accepted.
func TestSessionAgentStateChanges_ClockOnlyDoneToIdle(t *testing.T) {
	t0 := time.Now()
	current := map[string]*activity.SessionState{
		"sess-quiet": {Score: 0, Initialized: true, LastWorkingAt: t0},
	}
	baseline := map[string]activity.AgentState{}

	// t0: first observation publishes Done.
	changes := sessionAgentStateChanges(current, baseline, t0.Add(time.Second))
	if len(changes) != 1 || changes[0].state != activity.StateDone {
		t.Fatalf("first observation must publish done, got %#v", changes)
	}
	if changes[0].prev != activity.StateDone {
		t.Fatalf("first observation must pin prev to itself (no fabricated edge), got %#v", changes[0])
	}

	// Intermediate quiet scans below DoneWindow emit nothing.
	for i := 0; i < 3; i++ {
		if got := sessionAgentStateChanges(current, baseline, t0.Add(10*time.Second)); len(got) != 0 {
			t.Fatalf("quiet scan %d must emit nothing, got %#v", i, got)
		}
	}

	// After DoneWindow expires with zero content updates: exactly one Idle.
	changes = sessionAgentStateChanges(current, baseline, t0.Add(activity.DoneWindow+time.Second))
	if len(changes) != 1 {
		t.Fatalf("clock-only Done→Idle must emit exactly one change, got %#v", changes)
	}
	if changes[0].state != activity.StateIdle || changes[0].prev != activity.StateDone {
		t.Fatalf("want done→idle transition, got %#v", changes[0])
	}

	// Steady-state: further quiet scans emit nothing.
	if got := sessionAgentStateChanges(current, baseline, t0.Add(2*activity.DoneWindow)); len(got) != 0 {
		t.Fatalf("idle steady state must emit nothing, got %#v", got)
	}
}

// TestSessionAgentStateChanges_HoldExpiryPublishes covers the below-threshold
// case: a session held Working only by LastActiveAt's hold window publishes
// Working→Done when HoldDuration elapses with no new content — again a pure
// clock transition a snapshot-diff could never express.
func TestSessionAgentStateChanges_HoldExpiryPublishes(t *testing.T) {
	t0 := time.Now()
	current := map[string]*activity.SessionState{
		"sess-held": {Score: activity.ScoreThreshold - 1, Initialized: true, LastActiveAt: t0, LastWorkingAt: t0},
	}
	baseline := map[string]activity.AgentState{"sess-held": activity.StateWorking}

	// Inside the hold window: still Working, nothing emitted.
	if got := sessionAgentStateChanges(current, baseline, t0.Add(activity.HoldDuration-time.Second)); len(got) != 0 {
		t.Fatalf("inside hold window must emit nothing, got %#v", got)
	}
	// Past the hold window: publishes Done (LastWorkingAt still fresh).
	changes := sessionAgentStateChanges(current, baseline, t0.Add(activity.HoldDuration+time.Second))
	if len(changes) != 1 || changes[0].state != activity.StateDone || changes[0].prev != activity.StateWorking {
		t.Fatalf("hold expiry must publish working→done, got %#v", changes)
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
// for the transition-bounded write: a simulated state transition results in a
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
			agentStateBaseline: map[string]activity.AgentState{},
			activeWorkspaceIDs: map[string]bool{},
			agentStates:        map[string]activity.AgentState{},
		},
		dashboard: dashboard.New(),
	}
	app.tmuxActivity.settled = true

	msg := tmuxActivityResult{
		ActiveWorkspaceIDs: map[string]bool{},
		AgentStates:        map[string]activity.AgentState{},
		ScannerOwner:       true,
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
// coalescing end to end: a session whose classification matches the accepted
// baseline must not produce any @amux_agent_state write.
func TestApplyTmuxActivityPayload_NoAgentStateTagWriteWhenUnchanged(t *testing.T) {
	recorded := fakeSetAgentStateTag(t, nil)

	app := &App{
		tmuxActivity: tmuxActivityState{
			sessionStates: map[string]*activity.SessionState{
				"sess-steady": {Score: activity.ScoreThreshold},
			},
			// The baseline already accepted Working — this scan's identical
			// classification is a no-op, not a first observation.
			agentStateBaseline: map[string]activity.AgentState{"sess-steady": activity.StateWorking},
			activeWorkspaceIDs: map[string]bool{},
			agentStates:        map[string]activity.AgentState{},
		},
		dashboard: dashboard.New(),
	}
	app.tmuxActivity.settled = true

	msg := tmuxActivityResult{
		ActiveWorkspaceIDs: map[string]bool{},
		AgentStates:        map[string]activity.AgentState{},
		ScannerOwner:       true,
		UpdatedStates: map[string]*activity.SessionState{
			"sess-steady": {Score: activity.ScoreThreshold},
		},
	}

	drainCmd(app.applyTmuxActivityPayload(msg))

	if len(*recorded) != 0 {
		t.Fatalf("expected no tag write for an unchanged state (coalescing), got %#v", *recorded)
	}
}

// TestApplyTmuxActivityPayload_PublishesClockOnlyIdleEndToEnd drives the full
// apply path through time alone: a session accepted as Done keeps its stale
// snapshot (no UpdatedStates entries — the session emits nothing), and once
// msg.Now passes DoneWindow the retained session still publishes the Idle
// transition. This is the audit's core defect: external consumers saw "done"
// forever because nothing reclassified quiet retained sessions.
func TestApplyTmuxActivityPayload_PublishesClockOnlyIdleEndToEnd(t *testing.T) {
	recorded := fakeSetAgentStateTag(t, nil)
	t0 := time.Now()
	stale := &activity.SessionState{Initialized: true, LastWorkingAt: t0}

	app := &App{
		tmuxActivity: tmuxActivityState{
			sessionStates:      map[string]*activity.SessionState{},
			agentStateBaseline: map[string]activity.AgentState{},
			activeWorkspaceIDs: map[string]bool{},
			agentStates:        map[string]activity.AgentState{},
		},
		dashboard: dashboard.New(),
	}
	app.tmuxActivity.settled = true

	ownerMsg := func(now time.Time) tmuxActivityResult {
		return tmuxActivityResult{
			ActiveWorkspaceIDs: map[string]bool{},
			AgentStates:        map[string]activity.AgentState{},
			ScannerOwner:       true,
			Now:                now,
			// The complete retained set — the quiet session stays retained
			// even though it produced no per-scan update.
			SessionStates: map[string]*activity.SessionState{"sess-quiet": stale},
		}
	}

	drainCmd(app.applyTmuxActivityPayload(ownerMsg(t0)))                       // first obs: done
	drainCmd(app.applyTmuxActivityPayload(ownerMsg(t0.Add(10 * time.Second)))) // quiet
	drainCmd(app.applyTmuxActivityPayload(ownerMsg(t0.Add(activity.DoneWindow + time.Second))))

	var writes []string
	for _, w := range *recorded {
		if w.sessionName == "sess-quiet" && w.key == tmux.TagAgentState {
			writes = append(writes, w.value)
		}
	}
	want := []string{"done", "idle"}
	if len(writes) != len(want) {
		t.Fatalf("tag writes = %v, want %v", writes, want)
	}
	for i := range want {
		if writes[i] != want[i] {
			t.Fatalf("tag writes = %v, want %v", writes, want)
		}
	}
}

// TestApplyTmuxActivityPayload_FollowerPublishesNothing proves followers never
// emit per-session tags or hooks: a follower result carrying shared
// workspace state and even a session map must produce zero changes.
func TestApplyTmuxActivityPayload_FollowerPublishesNothing(t *testing.T) {
	recorded := fakeSetAgentStateTag(t, nil)
	fires := stubOnDoneHook(t, nil)
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws"}

	app := &App{
		projects: []data.Project{{Workspaces: []data.Workspace{*ws}}},
		tmuxActivity: tmuxActivityState{
			sessionStates:      map[string]*activity.SessionState{},
			agentStateBaseline: map[string]activity.AgentState{},
			activeWorkspaceIDs: map[string]bool{},
			agentStates:        map[string]activity.AgentState{},
		},
		dashboard: dashboard.New(),
	}
	app.tmuxActivity.settled = true

	drainCmd(app.applyTmuxActivityPayload(tmuxActivityResult{
		ActiveWorkspaceIDs: map[string]bool{},
		AgentStates:        map[string]activity.AgentState{},
		ScannerOwner:       false, // follower apply
		RoleKnown:          true,
		SessionStates: map[string]*activity.SessionState{
			"amux-" + string(ws.ID()) + "-tab-1": {Score: activity.ScoreThreshold},
		},
	}))

	if len(*recorded) != 0 {
		t.Fatalf("follower must not publish per-session tags, got %#v", *recorded)
	}
	if len(*fires) != 0 {
		t.Fatalf("follower must not fire on-done hooks, got %#v", *fires)
	}
}
