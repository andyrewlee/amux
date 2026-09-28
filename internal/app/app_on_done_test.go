package app

import (
	"errors"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/app/activity"
	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/dashboard"

	tea "charm.land/bubbletea/v2"
)

// drainOnDoneCmd executes cmd and flattens one level of tea.BatchMsg so tests
// can inspect the messages a handler's command emits.
func drainOnDoneCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for _, sub := range batch {
		if sub == nil {
			continue
		}
		if m := sub(); m != nil {
			out = append(out, m)
		}
	}
	return out
}

type recordedOnDoneFire struct {
	wsRoot      string
	sessionName string
}

// stubOnDoneHook swaps the runOnDoneHook seam for a recorder, mirroring the
// fakeSetAgentStateTag pattern in app_tmux_activity_agent_state_tag_test.go.
func stubOnDoneHook(t *testing.T, err error) *[]recordedOnDoneFire {
	t.Helper()
	orig := runOnDoneHook
	var recorded []recordedOnDoneFire
	runOnDoneHook = func(ws *data.Workspace, sessionName string, _ *workspacesvc.Service) error {
		recorded = append(recorded, recordedOnDoneFire{ws.Root, sessionName})
		return err
	}
	t.Cleanup(func() { runOnDoneHook = orig })
	return &recorded
}

func onDoneTestApp(ws *data.Workspace) *App {
	return &App{
		projects: []data.Project{{Workspaces: []data.Workspace{*ws}}},
	}
}

func TestOnDoneHookCmd_FiresOnWorkingToDone(t *testing.T) {
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws"}
	fires := stubOnDoneHook(t, nil)
	app := onDoneTestApp(ws)

	session := "amux-" + string(ws.ID()) + "-tab-1"
	cmd := app.onDoneHookCmd([]agentStateTagChange{
		{sessionName: session, state: activity.StateDone, prev: activity.StateWorking},
	})
	if cmd == nil {
		t.Fatal("expected a hook cmd for a working→done edge")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("successful fire should return nil msg, got %T", msg)
	}
	if len(*fires) != 1 {
		t.Fatalf("hook fired %d times, want exactly once", len(*fires))
	}
	got := (*fires)[0]
	if got.sessionName != session || got.wsRoot != ws.Root {
		t.Fatalf("hook fired with %+v, want session %q root %q", got, session, ws.Root)
	}
}

func TestOnDoneHookCmd_NonWorkingEdgesDoNotFire(t *testing.T) {
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws"}
	app := onDoneTestApp(ws)
	session := "amux-" + string(ws.ID()) + "-tab-1"

	for _, tc := range []struct {
		name        string
		state, prev activity.AgentState
	}{
		// The restart case: an agent that finished before amux started scans
		// in as idle→done. Firing a hook there could re-run side-effecting
		// commands for completions nobody watched — deliberately excluded.
		{"idle→done (first scan)", activity.StateDone, activity.StateIdle},
		{"done→idle", activity.StateIdle, activity.StateDone},
		{"working→idle", activity.StateIdle, activity.StateWorking},
		{"idle→working", activity.StateWorking, activity.StateIdle},
		{"working→working", activity.StateWorking, activity.StateWorking},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if cmd := app.onDoneHookCmd([]agentStateTagChange{
				{sessionName: session, state: tc.state, prev: tc.prev},
			}); cmd != nil {
				t.Fatal("expected no hook cmd")
			}
		})
	}
}

func TestOnDoneHookCmd_UnknownWorkspaceSkipped(t *testing.T) {
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws"}
	app := onDoneTestApp(ws)
	// A session name whose wsID segment matches no project workspace.
	cmd := app.onDoneHookCmd([]agentStateTagChange{
		{sessionName: "amux-0000000000000000-tab-1", state: activity.StateDone, prev: activity.StateWorking},
	})
	if cmd != nil {
		t.Fatal("expected nil cmd when the session's workspace is unknown")
	}
}

func TestOnDoneHookCmd_ErrorReportsOnce(t *testing.T) {
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws"}
	stubOnDoneHook(t, errors.New("spawn failed"))
	app := onDoneTestApp(ws)
	session := "amux-" + string(ws.ID()) + "-tab-1"

	cmd := app.onDoneHookCmd([]agentStateTagChange{
		{sessionName: session, state: activity.StateDone, prev: activity.StateWorking},
	})
	msg := cmd()
	res, ok := msg.(messages.WorkspaceOnDoneResult)
	if !ok {
		t.Fatalf("expected WorkspaceOnDoneResult, got %T", msg)
	}
	if res.SessionName != session || res.Err == nil {
		t.Fatalf("result = %+v, want session %q with error", res, session)
	}
}

// onDoneActivityApp builds an App with both the on-done workspace registry and
// owner-capable tmux-activity bookkeeping, so tests can drive full
// handleTmuxActivityResult/applyTmuxActivityPayload sequences.
func onDoneActivityApp(ws *data.Workspace) *App {
	return &App{
		projects:  []data.Project{{Workspaces: []data.Workspace{*ws}}},
		dashboard: dashboard.New(),
		tmuxActivity: tmuxActivityState{
			sessionStates:      map[string]*activity.SessionState{},
			agentStateBaseline: map[string]activity.AgentState{},
			activeWorkspaceIDs: map[string]bool{},
			agentStates:        map[string]activity.AgentState{},
			settled:            true,
		},
	}
}

// TestOnDoneHook_FirstObservationDoneDoesNotFire proves the restart case end
// to end: an owner scan that first observes an already-finished agent
// publishes the done tag but cannot fire the hook — there is no locally
// observed Working edge to complete.
func TestOnDoneHook_FirstObservationDoneDoesNotFire(t *testing.T) {
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws"}
	fires := stubOnDoneHook(t, nil)
	recorded := fakeSetAgentStateTag(t, nil)
	app := onDoneActivityApp(ws)
	session := "amux-" + string(ws.ID()) + "-tab-1"
	now := time.Now()

	drainCmd(app.applyTmuxActivityPayload(tmuxActivityResult{
		ScannerOwner:       true,
		Now:                now,
		ActiveWorkspaceIDs: map[string]bool{},
		AgentStates:        map[string]activity.AgentState{},
		SessionStates: map[string]*activity.SessionState{
			session: {Initialized: true, LastWorkingAt: now},
		},
	}))

	if len(*fires) != 0 {
		t.Fatalf("first-observation done must not fire the hook, got %#v", *fires)
	}
	if len(*recorded) != 1 || (*recorded)[0].value != "done" {
		t.Fatalf("first observation must still publish the done tag, got %#v", *recorded)
	}
}

// TestOnDoneHook_ObservedWorkingToDoneFiresOnce drives the genuine edge:
// Working is accepted first (published, no hook), the next scan sees Done, and
// the hook fires exactly once — a repeat quiet scan does not refire.
func TestOnDoneHook_ObservedWorkingToDoneFiresOnce(t *testing.T) {
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws"}
	fires := stubOnDoneHook(t, nil)
	app := onDoneActivityApp(ws)
	session := "amux-" + string(ws.ID()) + "-tab-1"
	t0 := time.Now()

	ownerScan := func(now time.Time, st *activity.SessionState) tmuxActivityResult {
		return tmuxActivityResult{
			ScannerOwner:       true,
			Now:                now,
			ActiveWorkspaceIDs: map[string]bool{},
			AgentStates:        map[string]activity.AgentState{},
			SessionStates:      map[string]*activity.SessionState{session: st},
		}
	}
	drainCmd(app.applyTmuxActivityPayload(ownerScan(t0, &activity.SessionState{
		Initialized: true, Score: activity.ScoreThreshold, LastWorkingAt: t0,
	})))
	if len(*fires) != 0 {
		t.Fatalf("working first-observation must not fire, got %#v", *fires)
	}

	drainCmd(app.applyTmuxActivityPayload(ownerScan(t0.Add(time.Second), &activity.SessionState{
		Initialized: true, LastWorkingAt: t0.Add(time.Second),
	})))
	if len(*fires) != 1 || (*fires)[0].sessionName != session {
		t.Fatalf("working→done edge must fire the hook exactly once, got %#v", *fires)
	}

	drainCmd(app.applyTmuxActivityPayload(ownerScan(t0.Add(2*time.Second), &activity.SessionState{
		Initialized: true, LastWorkingAt: t0.Add(time.Second),
	})))
	if len(*fires) != 1 {
		t.Fatalf("steady done must not refire, got %#v", *fires)
	}
}

// TestOnDoneHook_OwnerEpochResetDoesNotReplay proves a follower→owner epoch
// transition clears the semantic baseline: the post-transition first
// observation republishes the tag but cannot replay a prior epoch's on-done.
func TestOnDoneHook_OwnerEpochResetDoesNotReplay(t *testing.T) {
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws"}
	fires := stubOnDoneHook(t, nil)
	app := onDoneActivityApp(ws)
	app.tmuxActivity.ownershipSet = true
	app.tmuxActivity.scannerOwner = true
	app.tmuxActivity.ownerEpoch = 1
	session := "amux-" + string(ws.ID()) + "-tab-1"
	t0 := time.Now()
	app.tmuxActivity.agentStateBaseline[session] = activity.StateDone
	app.tmuxActivity.sessionStates[session] = &activity.SessionState{Initialized: true, LastWorkingAt: t0}
	app.tmuxActivity.token = 3

	// New ownership epoch: baseline + hysteresis reset inside the same result
	// handler, then this scan's payload applies — the session classifies Done
	// as a first observation, which must not fire.
	app.tmuxActivity.scanInFlight = true
	app.handleTmuxActivityResult(tmuxActivityResult{
		Token:              3,
		RoleKnown:          true,
		ScannerOwner:       true,
		ScannerEpoch:       2,
		Now:                t0.Add(time.Second),
		ActiveWorkspaceIDs: map[string]bool{},
		AgentStates:        map[string]activity.AgentState{},
		SessionStates:      map[string]*activity.SessionState{session: {Initialized: true, LastWorkingAt: t0}},
	})

	if len(*fires) != 0 {
		t.Fatalf("post-transition first observation must not replay on-done, got %#v", *fires)
	}
	if got := app.tmuxActivity.agentStateBaseline[session]; got != activity.StateDone {
		t.Fatalf("baseline must reseed to the observed state, got %v", got)
	}
}

// TestOnDoneHook_PruneThenReappearIsFreshObservation proves a pruned session's
// baseline entry is dropped, so a session that disappears and reappears (new
// tab, recycled name) cannot inherit a fabricated Working edge — but a real
// new working cycle on it still fires once.
func TestOnDoneHook_PruneThenReappearIsFreshObservation(t *testing.T) {
	ws := &data.Workspace{Name: "ws", Repo: "/repo", Root: "/repo/ws"}
	fires := stubOnDoneHook(t, nil)
	app := onDoneActivityApp(ws)
	session := "amux-" + string(ws.ID()) + "-tab-1"
	t0 := time.Now()
	app.tmuxActivity.agentStateBaseline[session] = activity.StateWorking
	app.tmuxActivity.sessionStates[session] = &activity.SessionState{Score: activity.ScoreThreshold}

	// The session leaves the retained set entirely (pruned): baseline drops.
	drainCmd(app.applyTmuxActivityPayload(tmuxActivityResult{
		ScannerOwner:       true,
		Now:                t0,
		ActiveWorkspaceIDs: map[string]bool{},
		AgentStates:        map[string]activity.AgentState{},
		SessionStates:      map[string]*activity.SessionState{},
	}))
	if _, ok := app.tmuxActivity.agentStateBaseline[session]; ok {
		t.Fatal("baseline must drop pruned sessions")
	}

	// Reappearing already-done: first observation, no hook.
	drainCmd(app.applyTmuxActivityPayload(tmuxActivityResult{
		ScannerOwner:       true,
		Now:                t0.Add(time.Second),
		ActiveWorkspaceIDs: map[string]bool{},
		AgentStates:        map[string]activity.AgentState{},
		SessionStates: map[string]*activity.SessionState{
			session: {Initialized: true, LastWorkingAt: t0},
		},
	}))
	if len(*fires) != 0 {
		t.Fatalf("reappeared session's first done observation must not fire, got %#v", *fires)
	}
}

func TestHandleWorkspaceOnDoneResult_UntrustedOffersTrustDialog(t *testing.T) {
	ws := &data.Workspace{Name: "feature", Repo: "/repo", Root: "/repo/ws"}
	h, err := NewHarness(HarnessOptions{Mode: HarnessCenter, Width: 120, Height: 40})
	if err != nil {
		t.Fatalf("NewHarness returned error: %v", err)
	}
	errUntrusted := &process.ScriptsNotTrustedError{Repo: ws.Repo, Command: "notify-send done", ConfigHash: "abc123"}

	cmd := h.app.handleWorkspaceOnDoneResult(messages.WorkspaceOnDoneResult{
		Workspace: ws, SessionName: "s1", Err: errUntrusted,
	})
	if cmd == nil {
		t.Fatal("expected cmds for the trust skip")
	}
	// First cmd is the warning toast; the batch also carries the trust-dialog
	// message so the user can approve the repo for the next edge.
	found := false
	for _, m := range drainOnDoneCmd(cmd) {
		if show, ok := m.(messages.ShowTrustScriptsDialog); ok {
			found = true
			if show.Workspace != ws || show.ConfigHash != "abc123" {
				t.Fatalf("trust dialog msg = %+v", show)
			}
		}
	}
	if !found {
		t.Fatal("expected a ShowTrustScriptsDialog message")
	}
}
